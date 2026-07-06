package lro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"google.golang.org/protobuf/proto"
	"riverqueue.com/riverpro"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// notifyChannel is the Postgres NOTIFY channel the operations
// trigger fires on; the LISTEN goroutine subscribes to it. Exported
// only as a const for symmetry with the trigger SQL — there's no
// other consumer.
const notifyChannel = "pivox_lro_done"

// Manager manages long-running operations.
type Manager struct {
	queries db.Querier
	logger  *slog.Logger

	// pool + river are required by NewLro and only by NewLro. The
	// rest of Manager (RecoverPending, the LISTEN loop, etc.) uses
	// queries directly. They're kept optional so callers that don't
	// touch NewLro (some tests) don't need to wire either field.
	pool  *pgxpool.Pool
	river *riverpro.Client[pgx.Tx]

	mu        sync.Mutex
	listeners map[uuid.UUID][]chan struct{}

	// shuttingDown gates new starts. Flipped by Shutdown under mu so
	// NewLro's "is shutdown in flight?" check observes it atomically
	// with Shutdown's "stop accepting work."
	shuttingDown bool
	// shutdownCtx is cancelled by Shutdown. The LISTEN loop consumes
	// it to know when to stop and release its pool connection.
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
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
	// Optional (some tests construct a pool-less Manager); required for
	// NewLro. Production wires the same pool used by the REST/gRPC
	// surface.
	Pool *pgxpool.Pool
	// River is the river.Client used by NewLro to enqueue jobs in the
	// same tx as the operations row insert. Optional during transition;
	// required for NewLro. Production constructs a query/insert-only
	// client (no Workers, no Start) — work execution lives in
	// pivox-worker, not pivox-cloud.
	River *riverpro.Client[pgx.Tx]
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
	// DELETE_REQUESTED. Leave zero for space-scoped or account-scoped
	// LROs and for DeleteOrganization itself (self-pointing org_id
	// would make the LRO cancel its own work).
	OrgID pgtype.UUID
	// SpaceID is the reverse pointer to the space this LRO operates
	// against. Set for space-scoped operations. The operation's authz
	// scope is SpaceID when set, else OrgID, else the caller's own
	// account (CreatedBy).
	SpaceID pgtype.UUID
	// CreatedBy is the identity-uuid of the caller that originated the
	// LRO. Required for account-scoped operations (the only authz
	// signal there); audit + a "my operations" filter for the rest.
	// Handlers set it via server.MustUserID(ctx).
	CreatedBy pgtype.UUID
	// ValidateOnly, when true, runs the synchronous LRO tx (operation-row
	// INSERT + River job enqueue) against real constraints but rolls it
	// back instead of committing — the AIP validate_only contract for a
	// River-backed LRO. The async job never runs because its enqueue
	// rolled back, so nothing is persisted; the returned Operation is the
	// would-be resource. Handlers pass req.GetValidateOnly() here.
	ValidateOnly bool
}

// NewLro creates a new operation row AND enqueues a River job for it,
// atomically in a single Postgres transaction. Work is enqueued for
// pivox-worker to pick up; pivox-cloud runs no in-process work.
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
// Requires Pool + River set on the Manager; errors at call time if
// either is nil (some tests construct a pool-less Manager).
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

	// Refuse new operations once Shutdown has begun — cheap insurance
	// even though pivox-worker processes jobs independently of
	// pivox-cloud's lifecycle.
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
		SpaceID:   opts.SpaceID,
		CreatedBy: opts.CreatedBy,
	})
	if err != nil {
		return nil, apierr.Internal("failed to create operation")
	}

	if _, err := m.river.InsertTx(ctx, tx, opts.JobArgs, opts.JobOpts); err != nil {
		return nil, apierr.Internal("failed to enqueue river job")
	}

	// validate_only: the operation row and the enqueued job both ran
	// against real constraints; roll them back so nothing persists and no
	// worker ever picks the job up. The deferred Rollback fires on return.
	// The returned Operation is the would-be resource (its row is gone).
	if opts.ValidateOnly {
		return OperationToProto(dbOp)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, apierr.Internal("failed to commit tx")
	}

	return OperationToProto(dbOp)
}

// Shutdown stops accepting new operations (NewLro returns
// Unavailable afterward) and stops the LISTEN loop, blocking until
// the listener goroutine releases its pool connection or ctx
// expires. Idempotent — duplicate signal handlers calling it twice
// is safe.
//
// Work execution lives in pivox-worker, not here, so there are no
// in-process LRO goroutines to drain — Shutdown only gates new
// NewLro starts and tears down the completion listener.
//
// Order in main.go: GracefulStop the gRPC server first (so no new
// NewLro calls land), then Manager.Shutdown(ctx), then close the DB
// pool. Closing the pool before the listener releases its conn can
// produce "use of closed connection" log noise.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.shutdownOnce.Do(func() {
		m.mu.Lock()
		m.shuttingDown = true
		m.mu.Unlock()
		m.shutdownCancel()
	})

	done := make(chan struct{})
	go func() {
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

// ParseOperationName extracts the UUID from an AIP-151 operation
// name. Per the spec the name "ends with `operations/{unique_id}`",
// so we accept any path whose last two segments are
// `operations/<uuid>`. Anything before is the parent resource (an
// AIP path like `organizations/acme/spaces/dev`) and is allowed to
// be empty for root-scoped operations.
func ParseOperationName(name string) (uuid.UUID, error) {
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
	opID, err := ParseOperationName(name)
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
	return OperationToProto(dbOp)
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
		op, err := OperationToProto(dbOp)
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

	opID, _ := ParseOperationName(name)

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
	opID, err := ParseOperationName(name)
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

// CancelOperation cancels a running operation. Cancellation is a
// race-safe DB mark: the CancelOperation query flips the row
// `done=true, error=Cancelled` only when it's still `done=false`, so
// a cancel never clobbers an already-completed result. The matching
// pg_notify wakes any in-process WaitOperation listeners immediately;
// the pivox-worker job processing this LRO observes the cancelled row
// on its next DB write/poll and stops. There is no in-process
// goroutine to cancel — work runs in pivox-worker, not here.
//
// ErrNoRows from the query means the row is either already done (the
// race-safe UPDATE matched nothing) or absent. We disambiguate with a
// GetOperation: present → already-done, treat as a no-op success;
// absent → NotFound.
func (m *Manager) CancelOperation(ctx context.Context, name string) error {
	opID, err := ParseOperationName(name)
	if err != nil {
		return apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}

	// Mark the DB row done. Race-safe: only flips done=false → done=true.
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
