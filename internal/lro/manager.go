package lro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// notifyChannel is the Postgres NOTIFY channel the operations
// trigger fires on; the LISTEN goroutine subscribes to it. Exported
// only as a const for symmetry with the trigger SQL — there's no
// other consumer.
const notifyChannel = "pivox_lro_done"

// WorkFunc performs the actual work for an operation. The supplied
// `progress` reports phase transitions back to the operation
// metadata so polling clients can observe state. Progress reporting
// is best-effort; the Update method swallows DB errors and never
// returns one (a metadata write failure must not abort the work).
type WorkFunc func(ctx context.Context, progress Progress) (proto.Message, error)

// Progress is the phase-reporting handle passed to a WorkFunc. It
// captures the operation ID so callers don't have to thread it
// manually. Implementations are safe to call from any goroutine.
type Progress interface {
	Update(ctx context.Context, metadata proto.Message)
}

type managerProgress struct {
	m    *Manager
	opID uuid.UUID
}

func (p *managerProgress) Update(ctx context.Context, metadata proto.Message) {
	p.m.UpdateMetadata(ctx, p.opID, metadata)
}

// Manager manages long-running operations.
type Manager struct {
	queries db.Querier
	logger  *slog.Logger

	// pool + river are required by NewLro and only by NewLro. The
	// rest of Manager (CreateAndRun, runWork, RecoverPending, etc.)
	// uses queries directly. They're kept optional for now so the
	// transition off the legacy path is incremental — handlers
	// migrate one at a time, tests that don't touch NewLro don't
	// need to wire either field. Once every LRO is on the new path
	// these become required and CreateAndRun/runWork get deleted.
	pool  *pgxpool.Pool
	river *river.Client[pgx.Tx]

	mu        sync.Mutex
	listeners map[uuid.UUID][]chan struct{}
	// running maps op id → cancel fn for the goroutine running its
	// WorkFunc. CancelOperation calls the registered fn so the work
	// goroutine sees ctx.Done() and aborts; without this, marking the
	// DB row done is just a label and the goroutine runs to
	// completion, overwriting the cancel state on success. The map
	// is populated when runWork starts and cleared when it returns.
	running map[uuid.UUID]context.CancelFunc

	// shuttingDown gates new starts. Flipped by Shutdown under mu so
	// CreateAndRun's "is shutdown in flight?" check and its wg.Add are
	// atomic with Shutdown's "stop accepting work, then Wait." Without
	// the lock, an Add could land after Wait returns — undefined
	// behavior on sync.WaitGroup.
	shuttingDown bool
	// shutdownCtx is cancelled by Shutdown. Each runWork forwards its
	// cancellation into that op's workCtx via context.AfterFunc, so a
	// shutdown signal aborts every in-flight WorkFunc. This is the
	// graceful-stop counterpart to CancelOperation's per-op cancel.
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	// wg tracks in-flight runWork goroutines so Shutdown can wait for
	// them to finish their bookkeeping (FailOperation/CompleteOperation)
	// before the caller closes the DB pool.
	wg sync.WaitGroup
	// shutdownOnce makes Shutdown idempotent — repeat callers from
	// duplicated signal handlers don't double-cancel or re-flip flags.
	shutdownOnce sync.Once
	// listenWG tracks the LISTEN goroutine so Shutdown can wait for
	// it to release its pool connection before the caller closes the
	// pool. One goroutine when pool is set; zero otherwise.
	listenWG sync.WaitGroup
}

// ManagerConfig is the constructor input for Manager.
type ManagerConfig struct {
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Logger is the slog logger used for failure / progress lines.
	// Required.
	Logger *slog.Logger
	// Pool is the pgxpool.Pool used by NewLro to begin a transaction
	// that wraps the operations row insert + the River job insert.
	// Optional during the transition off legacy CreateAndRun; required
	// for NewLro. Production wires the same pool used by the REST/gRPC
	// surface.
	Pool *pgxpool.Pool
	// River is the river.Client used by NewLro to enqueue jobs in the
	// same tx as the operations row insert. Optional during transition;
	// required for NewLro. Production constructs a query/insert-only
	// client (no Workers, no Start) — work execution lives in
	// pivox-worker, not pivox-cloud.
	River *river.Client[pgx.Tx]
}

// NewManager constructs a Manager from cfg. Panics on a missing
// required field — startup-time programmer error, fail loud on boot.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.Queries == nil {
		panic("lro: ManagerConfig.Queries is required")
	}
	if cfg.Logger == nil {
		panic("lro: ManagerConfig.Logger is required")
	}
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	m := &Manager{
		queries:        cfg.Queries,
		logger:         cfg.Logger,
		pool:           cfg.Pool,
		river:          cfg.River,
		listeners:      make(map[uuid.UUID][]chan struct{}),
		running:        make(map[uuid.UUID]context.CancelFunc),
		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,
	}
	// LISTEN bridges DB-side completion (workers commit done=true and
	// the operations trigger fires pg_notify) to in-process
	// WaitOperation listeners. Without this loop, WaitOperation has
	// no way to learn that a River-backed LRO has finished except
	// ctx-timeout-then-DB-read. Skipped when pool is nil so legacy
	// pool-less callers (none in production today; some tests) don't
	// fail to construct.
	if cfg.Pool != nil {
		m.listenWG.Add(1)
		go m.listenForCompletions()
	}
	return m
}

// listenForCompletions runs the pg LISTEN loop. Reconnects on error
// with capped exponential backoff. Exits when shutdownCtx is
// cancelled (i.e. Shutdown is called).
func (m *Manager) listenForCompletions() {
	defer m.listenWG.Done()
	const (
		minBackoff = 100 * time.Millisecond
		maxBackoff = 5 * time.Second
	)
	backoff := minBackoff
	for {
		if m.shutdownCtx.Err() != nil {
			return
		}
		err := m.listenOnce(m.shutdownCtx)
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		m.logger.Warn("lro: notification listener error; reconnecting",
			"error", err, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-m.shutdownCtx.Done():
			return
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// listenOnce holds a single pgx connection, issues LISTEN, and
// dispatches each notification to in-process listeners until the
// context is cancelled or a connection error surfaces. The caller
// (listenForCompletions) decides whether to retry.
//
// One pool slot is occupied for the lifetime of the connection.
// Default pool sizes (4–10) leave plenty for query traffic; if
// pool sizing ever becomes tight this can switch to a dedicated
// pgx.Connect using pool.Config().ConnConfig.
func (m *Manager) listenOnce(ctx context.Context) error {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn for LISTEN: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return fmt.Errorf("LISTEN %s: %w", notifyChannel, err)
	}

	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		opID, parseErr := uuid.Parse(n.Payload)
		if parseErr != nil {
			// A malformed payload is a bug in the trigger or a
			// rogue NOTIFY from outside the schema; log and keep
			// going so one bad row doesn't kill the listener.
			m.logger.Warn("lro: invalid notification payload",
				"payload", n.Payload, "error", parseErr)
			continue
		}
		m.notifyListeners(opID)
	}
}

// UpdateMetadata replaces the metadata blob on an in-flight
// operation. Used by multi-phase work functions to surface progress
// (e.g. DeleteOrganization transitioning VALIDATING →
// CANCELLING_OPERATIONS → MARKING_DELETED) so polling clients see
// the current phase. Marshal errors are logged but not returned —
// progress reporting is best-effort and must not abort the work.
func (m *Manager) UpdateMetadata(ctx context.Context, opID uuid.UUID, metadata proto.Message) {
	metaJSON, err := marshalAny(metadata)
	if err != nil {
		m.logger.Error("lro: marshal metadata for update", "op", opID, "error", err)
		return
	}
	if err := m.queries.UpdateOperationMetadata(ctx, db.UpdateOperationMetadataParams{
		ID:       opID,
		Metadata: metaJSON,
	}); err != nil {
		m.logger.Error("lro: update operation metadata", "op", opID, "error", err)
	}
}

// NewLroOpts is the input bundle for Manager.NewLro. Required fields
// are JobArgs; everything else is optional. Mirrors the codebase's
// Config-struct convention for multi-arg constructors.
type NewLroOpts struct {
	// OperationID is the UUID for the operations row (and the
	// caller-side reference the worker uses to call CompleteOperation
	// / FailOperation). Optional — if zero, NewLro generates one.
	// Most callers pre-generate this so they can also embed it in
	// JobArgs (the worker needs it to mark the operation done).
	OperationID uuid.UUID
	// JobArgs is the River job to enqueue. Required.
	JobArgs river.JobArgs
	// JobOpts is forwarded to river.InsertTx (priority, scheduledAt,
	// queue, tags, metadata, uniqueness). Optional.
	JobOpts *river.InsertOpts
	// Metadata is the initial AIP-151 Operation.metadata payload —
	// typically a typed protobuf describing the operation's phase /
	// target resource. Optional.
	Metadata proto.Message
	// OrgID is the reverse pointer to the org this LRO operates
	// against. Set for org-scoped operations so
	// CancelRunningOpsForOrg can interrupt them when the org enters
	// DELETE_REQUESTED. Leave zero for unscoped LROs and for
	// DeleteOrganization itself (self-pointing org_id would make
	// the LRO cancel its own work).
	OrgID pgtype.UUID
	// CreatedBy is the identity-uuid of the caller that originated
	// the LRO. Optional; populated by handlers via
	// server.MustPivoxUserID(ctx) when known.
	CreatedBy pgtype.UUID
}

// NewLro creates a new operation row AND enqueues a River job for it,
// atomically in a single Postgres transaction. This is the post-River
// replacement for CreateAndRun: instead of spawning a goroutine in
// pivox-cloud, work is enqueued and pivox-worker picks it up.
//
// `parent` is the AIP-151 parent resource (e.g.,
// "organizations/acme/spaces/dev"); the public Operation.name is
// constructed as `{parent}/operations/{uuid}`. Empty parent yields the
// unparented `operations/{uuid}` form.
//
// Atomicity: the operations row INSERT and River's job INSERT both
// run inside the same pgx.Tx. River's tables live in the `river`
// schema, our operations table in `public` — both writes commit (or
// roll back) together. No "row exists but no job" or vice versa.
//
// Requires Pool + River set on the Manager. Errors at call time if
// either is nil; this allows the legacy CreateAndRun path to keep
// working in tests/wiring that hasn't migrated.
func (m *Manager) NewLro(ctx context.Context, parent string, opts NewLroOpts) (*longrunningpb.Operation, error) {
	if m.pool == nil {
		return nil, apierr.Internal("lro: NewLro requires Manager.Pool")
	}
	if m.river == nil {
		return nil, apierr.Internal("lro: NewLro requires Manager.River")
	}
	if opts.JobArgs == nil {
		return nil, apierr.Internal("lro: NewLro requires JobArgs")
	}

	var metaJSON json.RawMessage
	if opts.Metadata != nil {
		var err error
		metaJSON, err = marshalAny(opts.Metadata)
		if err != nil {
			return nil, apierr.Internal("failed to marshal operation metadata")
		}
	}

	// Refuse new operations once Shutdown has begun. Matches the
	// CreateAndRun gate; cheap insurance even though pivox-worker
	// processes jobs independently of pivox-cloud's lifecycle.
	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		return nil, apierr.Unavailable("server is shutting down; no new operations accepted")
	}
	m.mu.Unlock()

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return nil, apierr.Internal("failed to begin tx")
	}
	// Rollback after Commit is a no-op (ErrTxClosed); intentional.
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := db.New(tx)

	opID := opts.OperationID
	if opID == uuid.Nil {
		opID = uuid.New()
	}
	dbOp, err := qtx.CreateOperation(ctx, db.CreateOperationParams{
		ID:        opID,
		Parent:    parent,
		Metadata:  metaJSON,
		OrgID:     opts.OrgID,
		CreatedBy: opts.CreatedBy,
	})
	if err != nil {
		return nil, apierr.Internal("failed to create operation")
	}

	if _, err := m.river.InsertTx(ctx, tx, opts.JobArgs, opts.JobOpts); err != nil {
		return nil, apierr.Internal("failed to enqueue river job")
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, apierr.Internal("failed to commit tx")
	}

	return dbToProto(dbOp)
}

// CreateAndRun creates a new operation and runs the work function
// asynchronously. `parent` is the AIP-151 parent resource the LRO
// operates against (e.g., "organizations/acme/spaces/dev"); the
// public Operation.name is constructed as
// `{parent}/operations/{uuid}`. Pass an empty string for unscoped
// LROs (root-level operations), in which case the name falls back
// to the unparented `operations/{uuid}` form.
//
// The operation is unscoped — operations.org_id is NULL — so it
// isn't cancellable via DeleteOrganization's CANCELLING_OPERATIONS
// phase. Use CreateAndRunForOrg when the LRO targets an
// organization and should be cancelled if the org is soft-deleted.
func (m *Manager) CreateAndRun(ctx context.Context, parent string, metadata proto.Message, work WorkFunc) (*longrunningpb.Operation, error) {
	return m.createAndRun(ctx, parent, pgtype.UUID{}, metadata, work)
}

// CreateAndRunForOrg is the org-scoped variant: the operation row's
// org_id is set to `orgID`, so DeleteOrganization's
// CancelRunningOpsForOrg will mark this LRO done with codes.Cancelled
// when the org enters DELETE_REQUESTED. Use this for any LRO whose
// progress would mutate org-scoped state (asset imports, domain
// verifications, gateway upgrades). DO NOT use it for the
// DeleteOrganization LRO itself — a self-pointing org_id would
// cause the cancellation phase to cancel its own work.
func (m *Manager) CreateAndRunForOrg(ctx context.Context, parent string, orgID uuid.UUID, metadata proto.Message, work WorkFunc) (*longrunningpb.Operation, error) {
	return m.createAndRun(ctx, parent, pgtype.UUID{Bytes: orgID, Valid: true}, metadata, work)
}

func (m *Manager) createAndRun(ctx context.Context, parent string, orgID pgtype.UUID, metadata proto.Message, work WorkFunc) (*longrunningpb.Operation, error) {
	opID := uuid.New()

	var metaJSON json.RawMessage
	if metadata != nil {
		var err error
		metaJSON, err = marshalAny(metadata)
		if err != nil {
			return nil, apierr.Internal("failed to marshal operation metadata")
		}
	}

	// Reserve a wg slot under mu so Shutdown's "flip the flag, then
	// Wait" sequence can't observe Add() after Wait() has returned.
	// If shutdown is already in flight, refuse the new operation.
	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		return nil, apierr.Unavailable("server is shutting down; no new operations accepted")
	}
	m.wg.Add(1)
	m.mu.Unlock()

	dbOp, err := m.queries.CreateOperation(ctx, db.CreateOperationParams{
		ID:       opID,
		Parent:   parent,
		Metadata: metaJSON,
		OrgID:    orgID,
	})
	if err != nil {
		m.wg.Done()
		return nil, apierr.Internal("failed to create operation")
	}

	go m.runWork(ctx, opID, work)

	return dbToProto(dbOp)
}

func (m *Manager) runWork(parent context.Context, opID uuid.UUID, work WorkFunc) {
	defer m.wg.Done()

	// Detach from the originating RPC's cancellation while keeping its
	// values (trace IDs, slog attrs, span context). LROs must outlive
	// the request that started them — `WithoutCancel` is precisely the
	// "values without lifetime" primitive.
	//
	// Two derived contexts:
	//  - workCtx: cancellable, passed to the WorkFunc. Three triggers
	//    cancel it: CancelOperation (per-op, via the registered cancel
	//    fn), CancelLocal (bulk, same map), and Shutdown (manager-wide,
	//    via the AfterFunc below). The WorkFunc observes ctx.Done()
	//    regardless of which fired.
	//  - cleanupCtx: derived from `detached` but not from workCtx, so
	//    the final FailOperation/CompleteOperation write inherits
	//    request-scoped values for log correlation but is not aborted
	//    by the same cancellation that ended the work. Shutdown's
	//    wg.Wait gives this bookkeeping its drain budget.
	detached := context.WithoutCancel(parent)
	workCtx, cancel := context.WithCancel(detached)
	defer cancel()
	cleanupCtx := detached

	// Forward shutdown into workCtx without spawning a watcher
	// goroutine. AfterFunc fires `cancel` if shutdownCtx is or becomes
	// done; `stop()` deregisters when work completes. If shutdownCtx
	// is already cancelled at registration time, AfterFunc invokes
	// the callback immediately in its own goroutine — covering the
	// Shutdown-fires-between-CreateAndRun-and-here race.
	stop := context.AfterFunc(m.shutdownCtx, cancel)
	defer stop()

	m.mu.Lock()
	m.running[opID] = cancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.running, opID)
		m.mu.Unlock()
	}()

	var (
		result proto.Message
		err    error
	)
	// Recover panics from user-supplied WorkFunc so a single bad LRO
	// can't take down pivox-cloud. The synchronous RPC path is covered
	// by gRPC's recovery interceptor; this goroutine is detached and
	// outside that chain. The closure scope keeps the bookkeeping
	// defers above untouched — they always run.
	func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.ErrorContext(cleanupCtx, "lro: WorkFunc panicked",
					"op", opID, "panic", r, "stack", string(debug.Stack()))
				err = grpcstatus.Errorf(codes.Internal, "operation panicked: %v", r)
			}
		}()
		progress := &managerProgress{m: m, opID: opID}
		result, err = work(workCtx, progress)
	}()

	// Distinguish shutdown-induced cancellation in the recorded
	// failure so RecoverPending isn't the only way to tell what
	// happened. Plain CancelOperation already sets the DB row done via
	// its own SQL path — this branch only reshapes the message that
	// FailOperation will record below.
	if errors.Is(err, context.Canceled) && m.shutdownCtx.Err() != nil {
		err = grpcstatus.Error(codes.Aborted, "operation aborted by server shutdown")
	}

	ctx := cleanupCtx
	// dbDone tracks whether the bookkeeping row has been written
	// (done=true). Listeners are only notified when this is true —
	// otherwise WaitOperation would wake up, observe done=false, and
	// the caller would see a stuck operation. A DB-write failure
	// here is recovered on the next server restart via
	// RecoverPending; we log loudly so the failure is investigable.
	dbDone := false
	if err != nil {
		errCode := int32(codes.Internal)
		errMsg := err.Error()
		if st, ok := grpcstatus.FromError(err); ok {
			errCode = int32(st.Code())
			errMsg = st.Message()
		}
		if _, dbErr := m.queries.FailOperation(ctx, db.FailOperationParams{
			ID:           opID,
			ErrorCode:    pgtype.Int4{Int32: errCode, Valid: true},
			ErrorMessage: pgtype.Text{String: errMsg, Valid: true},
		}); dbErr != nil {
			m.logger.ErrorContext(ctx, "lro: FailOperation DB write failed; row stuck done=false until RecoverPending",
				"op", opID, "error", dbErr)
		} else {
			dbDone = true
		}
	} else {
		var resultJSON json.RawMessage
		if result != nil {
			var marshalErr error
			resultJSON, marshalErr = marshalAny(result)
			if marshalErr != nil {
				m.logger.ErrorContext(ctx, "lro: marshal operation result failed", "op", opID, "error", marshalErr)
				if _, dbErr := m.queries.FailOperation(ctx, db.FailOperationParams{
					ID:           opID,
					ErrorCode:    pgtype.Int4{Int32: int32(codes.Internal), Valid: true},
					ErrorMessage: pgtype.Text{String: "marshal result: " + marshalErr.Error(), Valid: true},
				}); dbErr != nil {
					m.logger.ErrorContext(ctx, "lro: FailOperation (marshal-error path) DB write failed; row stuck done=false until RecoverPending",
						"op", opID, "error", dbErr)
				} else {
					m.notifyListeners(opID)
				}
				return
			}
		}
		if _, dbErr := m.queries.CompleteOperation(ctx, db.CompleteOperationParams{
			ID:     opID,
			Result: resultJSON,
		}); dbErr != nil {
			m.logger.ErrorContext(ctx, "lro: CompleteOperation DB write failed; row stuck done=false until RecoverPending",
				"op", opID, "error", dbErr)
		} else {
			dbDone = true
		}
	}

	if dbDone {
		m.notifyListeners(opID)
	}
}

// Shutdown stops accepting new operations, signals every in-flight
// WorkFunc via shutdownCtx, and blocks until all runWork goroutines
// have finished their bookkeeping (FailOperation/CompleteOperation)
// or until ctx expires. After Shutdown returns, CreateAndRun /
// CreateAndRunForOrg return Unavailable. Idempotent — duplicate
// signal handlers calling it twice is safe.
//
// Caller (main.go) supplies the drain budget. Anything not finished
// when ctx expires is left for RecoverPending on the next start —
// rows stuck done=false get marked Aborted("operation abandoned
// during server restart").
//
// Order in main.go: GracefulStop the gRPC server first (so no new
// CreateAndRun calls land), then Manager.Shutdown(ctx), then close
// the DB pool. Closing the pool before Shutdown returns can produce
// "use of closed connection" log noise from the bookkeeping writes.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.shutdownOnce.Do(func() {
		m.mu.Lock()
		m.shuttingDown = true
		m.mu.Unlock()
		m.shutdownCancel()
	})

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		// Wait for the LISTEN goroutine to release its pool conn
		// before returning, so the caller can close the pool
		// without a "use of closed connection" race. listenWG is
		// zero when pool was unset (no listener was started); Wait
		// is a no-op in that case.
		m.listenWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		m.logger.Warn("lro: shutdown drain deadline exceeded; abandoned ops will be marked failed by RecoverPending on next start")
		return ctx.Err()
	}
}

// parseOperationName extracts the UUID from an AIP-151 operation
// name. Per the spec the name "ends with `operations/{unique_id}`",
// so we accept any path whose last two segments are
// `operations/<uuid>`. Anything before is the parent resource (an
// AIP path like `organizations/acme/spaces/dev`) and is allowed to
// be empty for root-scoped operations.
func parseOperationName(name string) (uuid.UUID, error) {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return uuid.Nil, fmt.Errorf("invalid operation name %q: must end with operations/{id}", name)
	}
	if parts[len(parts)-2] != "operations" {
		return uuid.Nil, fmt.Errorf("invalid operation name %q: must end with operations/{id}", name)
	}
	id, err := uuid.Parse(parts[len(parts)-1])
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid operation ID in %q: %w", name, err)
	}
	return id, nil
}

// GetOperation retrieves an operation by name.
func (m *Manager) GetOperation(ctx context.Context, name string) (*longrunningpb.Operation, error) {
	opID, err := parseOperationName(name)
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	dbOp, err := m.queries.GetOperation(ctx, opID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("Operation", name)
		}
		return nil, apierr.Internal("failed to get operation")
	}
	return dbToProto(dbOp)
}

// ListOperations lists operations with optional filtering by parent.
// `parent` is the AIP-151 parent resource (e.g.,
// "organizations/acme/spaces/dev"); empty string lists all
// operations regardless of parent.
func (m *Manager) ListOperations(ctx context.Context, parent string, pageSize int32) ([]*longrunningpb.Operation, error) {
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}

	var parentFilter pgtype.Text
	if parent != "" {
		parentFilter = pgtype.Text{String: parent, Valid: true}
	}

	dbOps, err := m.queries.ListOperations(ctx, db.ListOperationsParams{
		Limit:        pageSize,
		ParentFilter: parentFilter,
	})
	if err != nil {
		return nil, apierr.Internal("failed to list operations")
	}

	ops := make([]*longrunningpb.Operation, 0, len(dbOps))
	for _, dbOp := range dbOps {
		op, err := dbToProto(dbOp)
		if err != nil {
			continue
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// WaitOperation waits for an operation to complete or the context to be cancelled.
func (m *Manager) WaitOperation(ctx context.Context, name string) (*longrunningpb.Operation, error) {
	op, err := m.GetOperation(ctx, name)
	if err != nil {
		return nil, err
	}
	if op.Done {
		return op, nil
	}

	opID, _ := parseOperationName(name)

	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.listeners[opID] = append(m.listeners[opID], ch)
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		listeners := m.listeners[opID]
		for i, l := range listeners {
			if l == ch {
				m.listeners[opID] = append(listeners[:i], listeners[i+1:]...)
				break
			}
		}
		if len(m.listeners[opID]) == 0 {
			delete(m.listeners, opID)
		}
		m.mu.Unlock()
	}()

	select {
	case <-ch:
		return m.GetOperation(ctx, name)
	case <-ctx.Done():
		return m.GetOperation(context.Background(), name)
	}
}

// DeleteOperation deletes a completed operation.
func (m *Manager) DeleteOperation(ctx context.Context, name string) error {
	opID, err := parseOperationName(name)
	if err != nil {
		return apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	dbOp, err := m.queries.GetOperation(ctx, opID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("Operation", name)
		}
		return apierr.Internal("failed to get operation")
	}
	if !dbOp.Done {
		return apierr.FailedPrecondition("cannot delete a running operation")
	}
	if err := m.queries.DeleteOperation(ctx, opID); err != nil {
		return apierr.Internal("failed to delete operation")
	}
	return nil
}

// CancelOperation cancels a running operation. Two-step:
//  1. Cancel the in-flight goroutine (if any) by invoking the
//     context.CancelFunc registered in runWork. The goroutine sees
//     ctx.Done() and aborts; runWork then writes the failure to the
//     DB via FailOperation as part of its normal exit path.
//  2. Best-effort write `done=true, error=Cancelled` to the DB. This
//     covers the case where the goroutine isn't on this replica
//     (cross-replica cancellation) — without it, a cancel issued from
//     a different server would only mark intent locally.
//
// When the goroutine IS local, both writes happen and the second one
// is absorbed (race-safe SQL refuses to flip an already-done row).
// When the goroutine is on another replica, only step 2 runs; the
// remote goroutine continues until it next checks ctx.Done() / makes
// a DB call that observes the cancelled row.
func (m *Manager) CancelOperation(ctx context.Context, name string) error {
	opID, err := parseOperationName(name)
	if err != nil {
		return apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}

	// Step 1: cancel the in-flight goroutine on this replica, if any.
	m.mu.Lock()
	cancel, ok := m.running[opID]
	m.mu.Unlock()
	if ok {
		cancel()
	}

	// Step 2: mark the DB row done. Race-safe: only flips
	// done=false → done=true.
	_, err = m.queries.CancelOperation(ctx, opID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, getErr := m.queries.GetOperation(ctx, opID)
			if getErr != nil {
				return apierr.NotFound("Operation", name)
			}
			return nil
		}
		return apierr.Internal("failed to cancel operation")
	}
	m.notifyListeners(opID)
	return nil
}

// CancelLocal fires the cancel func for any of the supplied opIDs
// that are running on THIS replica. No DB write — callers use this
// after a bulk SQL UPDATE has already marked the rows done. The bulk
// SQL handles cross-replica completion (other replicas' goroutines
// see ErrNoRows on their next DB poll); CancelLocal handles the
// in-replica goroutines that would otherwise run to completion
// before noticing the row was cancelled.
func (m *Manager) CancelLocal(opIDs ...uuid.UUID) {
	if len(opIDs) == 0 {
		return
	}
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(opIDs))
	for _, id := range opIDs {
		if c, ok := m.running[id]; ok {
			cancels = append(cancels, c)
		}
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// RecoverPending marks any pending (non-done) operations as failed on startup.
func (m *Manager) RecoverPending(ctx context.Context) error {
	ops, err := m.queries.ListPendingOperations(ctx)
	if err != nil {
		return fmt.Errorf("list pending operations: %w", err)
	}
	for _, op := range ops {
		if _, err := m.queries.FailOperation(ctx, db.FailOperationParams{
			ID:           op.ID,
			ErrorCode:    pgtype.Int4{Int32: int32(codes.Aborted), Valid: true},
			ErrorMessage: pgtype.Text{String: "operation abandoned during server restart", Valid: true},
		}); err != nil {
			m.logger.Error("failed to recover pending operation", "op", op.ID, "error", err)
		}
	}
	if len(ops) > 0 {
		m.logger.Info("recovered pending operations", "count", len(ops))
	}
	return nil
}

func (m *Manager) notifyListeners(opID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.listeners[opID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
