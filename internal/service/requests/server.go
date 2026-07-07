package requests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"log/slog"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/server"
)

type RequestsServer struct {
	assetsv1.UnimplementedRequestsServer
	pool    db.TxBeginner
	queries db.Querier
	audit   *audit.Resolver
}

// Config is the constructor input for RequestsServer.
type Config struct {
	// Pool is the database pool used to begin transactions for
	// multi-step write paths (CreateRequest fans out to
	// CreateRequest + CreateAsset + CreateLineItem per line item).
	// Required. Wrapped in a *db.PoolTxer internally; unit tests
	// that need mock-Querier-level control should construct the
	// server struct literal directly with a *db.PassthroughTxer.
	Pool db.TxBeginner
	// Queries is the sqlc query interface for read paths. Required.
	Queries db.Querier
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
}

// NewRequestsServer constructs the server from cfg. Panics on a
// missing required field.
func NewRequestsServer(cfg Config) *RequestsServer {
	if cfg.Pool == nil {
		panic("requests: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("requests: Config.Queries is required")
	}
	return &RequestsServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		audit:   cfg.AuditResolver,
	}
}

// resolveRequestActors gathers created_by/updated_by UUIDs across the
// page and resolves them in a single batched call.
func (s *RequestsServer) resolveRequestActors(ctx context.Context, rows []db.AssetRequest) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve request actors failed", "error", err)
		return nil, apierr.Internal(err, "resolve actors")
	}
	return actors, nil
}

// resolveLineItemActors gathers created_by/updated_by UUIDs across
// the page.
func (s *RequestsServer) resolveLineItemActors(ctx context.Context, rows []db.AssetRequestLineItem) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve line item actors failed", "error", err)
		return nil, apierr.Internal(err, "resolve actors")
	}
	return actors, nil
}

// resolveLineItemAssetNames batch-fetches `assets.name` for every
// non-null `asset_id` across the page so LineItemToProto can render
// `pb.Asset` as a valid resource reference without an N+1 fetch.
// Returns nil when there are no asset_ids to resolve.
func (s *RequestsServer) resolveLineItemAssetNames(ctx context.Context, rows []db.AssetRequestLineItem) (map[uuid.UUID]string, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	seen := make(map[uuid.UUID]struct{}, len(rows))
	for _, r := range rows {
		if !r.AssetID.Valid {
			continue
		}
		id := uuid.UUID(r.AssetID.Bytes)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	pairs, err := s.queries.GetAssetNamesByIDs(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve line item asset names failed", "error", err)
		return nil, apierr.Internal(err, "resolve asset names")
	}
	out := make(map[uuid.UUID]string, len(pairs))
	for _, p := range pairs {
		out[p.ID] = p.Name
	}
	return out, nil
}

// parseRequestName parses "organizations/{org}/spaces/{space}/requests/{request}".
func parseRequestName(name string) (orgName, spaceName, requestName string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "spaces" || parts[4] != "requests" {
		return "", "", "", fmt.Errorf("invalid request name %q", name)
	}
	return parts[1], parts[3], parts[5], nil
}

// parseRequestParent parses "organizations/{org}/spaces/{space}".
func parseRequestParent(parent string) (orgName, spaceName string, err error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "spaces" {
		return "", "", fmt.Errorf("invalid parent %q", parent)
	}
	return parts[1], parts[3], nil
}

// resolveSpace resolves org name + space name to space UUID.
func (s *RequestsServer) resolveSpace(ctx context.Context, orgName, spaceName string) (uuid.UUID, error) {
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	space, err := s.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{OrgID: org.ID, Name: spaceName})
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Space", fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName))
	}
	return space.ID, nil
}

func (s *RequestsServer) GetRequest(ctx context.Context, req *assetsv1.GetRequestRequest) (*assetsv1.Request, error) {
	orgName, spaceName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	request, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{SpaceID: spaceID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, err := s.resolveRequestActors(ctx, []db.AssetRequest{request})
	if err != nil {
		return nil, err
	}
	proto := convert.RequestToProto(request, parentName, actors)

	// Populate line items, counts.
	lineItems, err := s.queries.ListLineItemsByRequest(ctx, db.ListLineItemsByRequestParams{
		RequestID: request.ID,
		Limit:     100,
		Offset:    0,
	})
	if err == nil {
		requestFullName := fmt.Sprintf("%s/requests/%s", parentName, requestName)
		liActors, liErr := s.resolveLineItemActors(ctx, lineItems)
		if liErr != nil {
			return nil, liErr
		}
		assetNames, anErr := s.resolveLineItemAssetNames(ctx, lineItems)
		if anErr != nil {
			return nil, anErr
		}
		for _, li := range lineItems {
			proto.LineItems = append(proto.LineItems, convert.LineItemToProto(li, requestFullName, parentName, liActors, assetNames))
		}
		proto.LineItemCount = int32(len(lineItems))
	}

	fulfilledCount, err := s.queries.CountFulfilledLineItems(ctx, request.ID)
	if err == nil {
		proto.FulfilledCount = int32(fulfilledCount)
	}

	return proto, nil
}

func (s *RequestsServer) ListRequests(ctx context.Context, req *assetsv1.ListRequestsRequest) (*assetsv1.ListRequestsResponse, error) {
	orgName, spaceName, err := parseRequestParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetParent())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var rows []db.AssetRequest
	rows, err = s.queries.ListRequestsBySpace(ctx, db.ListRequestsBySpaceParams{
		SpaceID: spaceID,
		Limit:   pageSize + 1,
		Offset:  0,
	})
	_ = req.GetShowDeleted() // soft-delete removed; flag is a no-op
	if err != nil {
		return nil, apierr.Internal(err, "database error")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken = rows[pageSize].ID.String()
		rows = rows[:pageSize]
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, err := s.resolveRequestActors(ctx, rows)
	if err != nil {
		return nil, err
	}
	requests := make([]*assetsv1.Request, 0, len(rows))
	for _, r := range rows {
		requests = append(requests, convert.RequestToProto(r, parentName, actors))
	}

	return &assetsv1.ListRequestsResponse{
		Requests:      requests,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *RequestsServer) CreateRequest(ctx context.Context, req *assetsv1.CreateRequestRequest) (*longrunningpb.Operation, error) {
	request := req.GetRequest()
	orgName, spaceName, err := parseRequestParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetParent())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	requestName := uuid.New().String()[:12]

	priority := db.RequestPriorityNORMAL
	if request.GetPriority() != assetsv1.Request_PRIORITY_UNSPECIFIED {
		priority = db.RequestPriority(request.GetPriority().String())
	}

	var dueTime pgtype.Timestamptz
	if request.GetDueTime() != nil {
		dueTime = pgtype.Timestamptz{Time: request.GetDueTime().AsTime(), Valid: true}
	}

	caller := convert.PgUUID(server.MustUserID(ctx))

	// Tx-wrapped: a single CreateRequest fans out to (1) the request
	// row, (2) one CreateAsset per line item, and (3) one
	// CreateLineItem per line item. Without the tx, a failure in
	// iteration k would leave k assets + k-1 line items + the request
	// row committed individually — a half-built request the client
	// is told doesn't exist. RunInTx rolls everything back as a
	// single unit on any failure inside the closure. validate_only runs
	// the whole fan-out and rolls it back, so a would-fail request returns
	// the same error a live one would while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.AssetRequest, error) {
		req, err := qtx.CreateRequest(ctx, db.CreateRequestParams{
			ID:          uuid.New(),
			SpaceID:     spaceID,
			Name:        requestName,
			DisplayName: request.GetDisplayName(),
			Description: request.GetDescription(),
			State:       db.RequestStateDRAFT,
			Priority:    priority,
			Assignee:    request.GetAssignee(),
			DueTime:     dueTime,
			CreatedBy:   caller,
		})
		if err != nil {
			return db.AssetRequest{}, apierr.HandleResourceError(err, "Request", "")
		}

		// Create line items and placeholder assets for each.
		for _, li := range request.GetLineItems() {
			lineItemName := uuid.New().String()[:12]
			assetName := uuid.New().String()[:12]

			// Create placeholder asset.
			asset, err := qtx.CreateAsset(ctx, db.CreateAssetParams{
				ID:          uuid.New(),
				SpaceID:     spaceID,
				Name:        assetName,
				DisplayName: li.GetDisplayName(),
				State:       db.AssetStatePLACEHOLDER,
				Annotations: json.RawMessage("{}"),
				CreatedBy:   caller,
			})
			if err != nil {
				return db.AssetRequest{}, apierr.HandleResourceError(err, "Asset", "")
			}

			var mediaType db.NullAssetMediaType
			if li.GetMediaType() != assetsv1.Asset_MEDIA_TYPE_UNSPECIFIED {
				mediaType = db.NullAssetMediaType{
					AssetMediaType: db.AssetMediaType(li.GetMediaType().String()),
					Valid:          true,
				}
			}

			var liAnnotations json.RawMessage
			if ann := li.GetAnnotations(); ann != nil {
				liAnnotations, _ = json.Marshal(ann)
			} else {
				liAnnotations = json.RawMessage("{}")
			}

			if _, err := qtx.CreateLineItem(ctx, db.CreateLineItemParams{
				ID:          uuid.New(),
				RequestID:   req.ID,
				AssetID:     pgtype.UUID{Bytes: asset.ID, Valid: true},
				Name:        lineItemName,
				DisplayName: li.GetDisplayName(),
				Description: li.GetDescription(),
				MediaType:   mediaType,
				Annotations: liAnnotations,
				CreatedBy:   caller,
			}); err != nil {
				return db.AssetRequest{}, apierr.HandleResourceError(err, "LineItem", "")
			}
		}
		return req, nil
	})
	if err != nil {
		return nil, err
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)

	// Post-commit enrichment — Actor resolution lives outside the tx
	// per RunInTx's contract; treat as best-effort.
	actors, resolveErr := s.resolveRequestActors(ctx, []db.AssetRequest{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "request: actor resolution failed; returning proto without audit actors", "request_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.RequestToProto(result, parentName, actors))
}

func (s *RequestsServer) UpdateRequest(ctx context.Context, req *assetsv1.UpdateRequestRequest) (*longrunningpb.Operation, error) {
	request := req.GetRequest()
	orgName, spaceName, requestName, err := parseRequestName(request.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", request.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{SpaceID: spaceID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", request.GetName())
	}

	updateParams := db.UpdateRequestParams{
		ID:        existing.ID,
		UpdatedBy: convert.PgUUID(server.MustUserID(ctx)),
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "display_name":
				updateParams.DisplayName = pgtype.Text{String: request.GetDisplayName(), Valid: true}
			case "description":
				updateParams.Description = pgtype.Text{String: request.GetDescription(), Valid: true}
			case "priority":
				updateParams.Priority = db.NullRequestPriority{
					RequestPriority: db.RequestPriority(request.GetPriority().String()),
					Valid:           true,
				}
			case "due_time":
				if request.GetDueTime() != nil {
					updateParams.DueTime = pgtype.Timestamptz{Time: request.GetDueTime().AsTime(), Valid: true}
				}
			case "annotations":
				ann, _ := json.Marshal(request.GetAnnotations())
				updateParams.Annotations = ann
			}
		}
	} else {
		updateParams.DisplayName = pgtype.Text{String: request.GetDisplayName(), Valid: true}
		updateParams.Description = pgtype.Text{String: request.GetDescription(), Valid: true}
	}

	// validate_only runs the UPDATE against real constraints and rolls it
	// back, so a would-fail request returns the same error a live one would
	// while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.AssetRequest, error) {
		return qtx.UpdateRequest(ctx, updateParams)
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", request.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, resolveErr := s.resolveRequestActors(ctx, []db.AssetRequest{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "request: actor resolution failed; returning proto without audit actors", "request_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.RequestToProto(result, parentName, actors))
}

func (s *RequestsServer) DeleteRequest(ctx context.Context, req *assetsv1.DeleteRequestRequest) (*longrunningpb.Operation, error) {
	orgName, spaceName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{SpaceID: spaceID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	// validate_only runs the DELETE against real state and rolls it back,
	// so a would-fail request returns the same error a live one would while
	// persisting nothing.
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		return qtx.DeleteRequest(ctx, existing.ID)
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	existing.State = db.RequestStateCANCELLED
	actors, resolveErr := s.resolveRequestActors(ctx, []db.AssetRequest{existing})
	if resolveErr != nil {
		slog.WarnContext(ctx, "request: actor resolution failed; returning proto without audit actors", "request_id", existing.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.RequestToProto(existing, parentName, actors))
}

// SubmitRequest transitions DRAFT → OPEN.
func (s *RequestsServer) SubmitRequest(ctx context.Context, req *assetsv1.SubmitRequestRequest) (*assetsv1.Request, error) {
	return s.transitionRequest(ctx, req.GetName(), db.RequestStateDRAFT, db.RequestStateOPEN)
}

// AssignRequest sets the assignee and transitions OPEN → IN_PROGRESS.
//
// Tx-wrapped: the precondition (state ∈ {OPEN, IN_PROGRESS}) is too
// disjunctive for a single conditional WHERE clause, so we lock the
// row inside a tx via GetRequestByNameForUpdate. Concurrent
// AssignRequest / ClaimRequest / Cancel calls on the same row
// queue on the lock and each evaluates the precondition against the
// post-prior-commit state.
func (s *RequestsServer) AssignRequest(ctx context.Context, req *assetsv1.AssignRequestRequest) (*assetsv1.Request, error) {
	orgName, spaceName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	result, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (db.AssetRequest, error) {
		existing, err := qtx.GetRequestByNameForUpdate(ctx, db.GetRequestByNameForUpdateParams{
			SpaceID: spaceID, Name: requestName,
		})
		if err != nil {
			return db.AssetRequest{}, apierr.HandleResourceError(err, "Request", req.GetName())
		}
		if existing.State != db.RequestStateOPEN && existing.State != db.RequestStateINPROGRESS {
			return db.AssetRequest{}, apierr.FailedPrecondition(fmt.Sprintf("request must be OPEN or IN_PROGRESS to assign, got %s", existing.State))
		}
		updated, err := qtx.UpdateRequestAssignee(ctx, db.UpdateRequestAssigneeParams{
			ID:        existing.ID,
			Assignee:  req.GetAssignee(),
			State:     db.RequestStateINPROGRESS,
			UpdatedBy: pgtype.UUID{},
		})
		if err != nil {
			return db.AssetRequest{}, apierr.HandleResourceError(err, "Request", req.GetName())
		}
		return updated, nil
	})
	if err != nil {
		return nil, err
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, resolveErr := s.resolveRequestActors(ctx, []db.AssetRequest{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "request: actor resolution failed; returning proto without audit actors", "request_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return convert.RequestToProto(result, parentName, actors), nil
}

// ClaimRequest self-assigns the caller. OPEN → IN_PROGRESS.
//
// Tx-wrapped: same shape as AssignRequest. Two concurrent claims
// could both observe state=OPEN under the previous read-then-update
// pattern; the row lock makes the second one see IN_PROGRESS and
// surface a clear FailedPrecondition.
func (s *RequestsServer) ClaimRequest(ctx context.Context, req *assetsv1.ClaimRequestRequest) (*assetsv1.Request, error) {
	orgName, spaceName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	result, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (db.AssetRequest, error) {
		existing, err := qtx.GetRequestByNameForUpdate(ctx, db.GetRequestByNameForUpdateParams{
			SpaceID: spaceID, Name: requestName,
		})
		if err != nil {
			return db.AssetRequest{}, apierr.HandleResourceError(err, "Request", req.GetName())
		}
		if existing.State != db.RequestStateOPEN {
			return db.AssetRequest{}, apierr.FailedPrecondition(fmt.Sprintf("can only claim OPEN requests, got %s", existing.State))
		}
		// TODO: pass the caller's identity id through to the
		// `assignee` column once that column also moves to UUID FK
		// (it's currently TEXT and stores the firebase_uid). For now,
		// only the audit `updated_by` is populated with the caller's
		// UUID; `assignee` is left empty until the broader UUID
		// migration covers it.
		//
		// Resolved inside the closure (after the precondition check)
		// so handler-level state-mismatch returns don't trip
		// MustUserID's panic in unit tests that exercise the
		// FailedPrecondition path without a caller claim.
		caller := convert.PgUUID(server.MustUserID(ctx))
		updated, err := qtx.UpdateRequestAssignee(ctx, db.UpdateRequestAssigneeParams{
			ID:        existing.ID,
			Assignee:  "",
			State:     db.RequestStateINPROGRESS,
			UpdatedBy: caller,
		})
		if err != nil {
			return db.AssetRequest{}, apierr.HandleResourceError(err, "Request", req.GetName())
		}
		return updated, nil
	})
	if err != nil {
		return nil, err
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, resolveErr := s.resolveRequestActors(ctx, []db.AssetRequest{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "request: actor resolution failed; returning proto without audit actors", "request_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return convert.RequestToProto(result, parentName, actors), nil
}

// DeliverRequest transitions IN_PROGRESS → DELIVERED.
func (s *RequestsServer) DeliverRequest(ctx context.Context, req *assetsv1.DeliverRequestRequest) (*assetsv1.Request, error) {
	return s.transitionRequest(ctx, req.GetName(), db.RequestStateINPROGRESS, db.RequestStateDELIVERED)
}

// ApproveRequest transitions DELIVERED → APPROVED.
func (s *RequestsServer) ApproveRequest(ctx context.Context, req *assetsv1.ApproveRequestRequest) (*assetsv1.Request, error) {
	return s.transitionRequest(ctx, req.GetName(), db.RequestStateDELIVERED, db.RequestStateAPPROVED)
}

// RequestRevision transitions DELIVERED → REVISION_REQUESTED.
func (s *RequestsServer) RequestRevision(ctx context.Context, req *assetsv1.RequestRevisionRequest) (*assetsv1.Request, error) {
	return s.transitionRequest(ctx, req.GetName(), db.RequestStateDELIVERED, db.RequestStateREVISIONREQUESTED)
}

// RejectRequest transitions DELIVERED → REJECTED.
func (s *RequestsServer) RejectRequest(ctx context.Context, req *assetsv1.RejectRequestRequest) (*assetsv1.Request, error) {
	return s.transitionRequest(ctx, req.GetName(), db.RequestStateDELIVERED, db.RequestStateREJECTED)
}

// CancelRequest transitions any state → CANCELLED unless the
// request is already APPROVED or CANCELLED.
//
// Tx-wrapped: the precondition is "state is anything but
// APPROVED/CANCELLED", which is too disjunctive to fold cleanly
// into a single WHERE clause the way UpdateRequestStateIfFrom does
// for fixed from→to transitions. Instead we lock the row inside a
// tx so concurrent transitions (e.g. an ApproveRequest racing this
// CancelRequest) serialize — the second tx reads the post-first-
// commit state and the precondition fires accurately.
func (s *RequestsServer) CancelRequest(ctx context.Context, req *assetsv1.CancelRequestRequest) (*assetsv1.Request, error) {
	orgName, spaceName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	result, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (db.AssetRequest, error) {
		existing, err := qtx.GetRequestByNameForUpdate(ctx, db.GetRequestByNameForUpdateParams{
			SpaceID: spaceID, Name: requestName,
		})
		if err != nil {
			return db.AssetRequest{}, apierr.HandleResourceError(err, "Request", req.GetName())
		}
		if existing.State == db.RequestStateAPPROVED || existing.State == db.RequestStateCANCELLED {
			return db.AssetRequest{}, apierr.FailedPrecondition(fmt.Sprintf("cannot cancel a request in state %s", existing.State))
		}
		updated, err := qtx.UpdateRequestState(ctx, db.UpdateRequestStateParams{
			ID:        existing.ID,
			State:     db.RequestStateCANCELLED,
			UpdatedBy: pgtype.UUID{},
		})
		if err != nil {
			return db.AssetRequest{}, apierr.HandleResourceError(err, "Request", req.GetName())
		}
		return updated, nil
	})
	if err != nil {
		return nil, err
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, resolveErr := s.resolveRequestActors(ctx, []db.AssetRequest{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "request: actor resolution failed; returning proto without audit actors", "request_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return convert.RequestToProto(result, parentName, actors), nil
}

// transitionRequest is a helper for simple state transitions.
//
// Race protection has two layers:
//
//   - The conditional UpdateRequestStateIfFrom collapses the
//     precondition into the WHERE clause, so two concurrent
//     transition calls can't both win — only one matches `state =
//     $from` at commit time.
//   - The whole sequence (read-id, conditional-update, optional
//     re-read) runs inside a tx so the row's identity is stable
//     across the three statements. Without the tx, a race where the
//     row is deleted and re-created with the same (space_id, name)
//     between our GetRequestByName and our UpdateRequestStateIfFrom
//     would let us address the wrong row's id with the precondition.
//     Per `internal/AGENTS.md`'s load-bearing rule, multi-statement
//     handlers run in a tx; this one is no exception.
//
// On ErrNoRows from the conditional UPDATE we re-read inside the
// same tx to disambiguate "row missing" from "state mismatch" so
// the FailedPrecondition message names the actual current state.
func (s *RequestsServer) transitionRequest(ctx context.Context, name string, fromState, toState db.RequestState) (*assetsv1.Request, error) {
	orgName, spaceName, requestName, err := parseRequestName(name)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", name)
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	result, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (db.AssetRequest, error) {
		existing, err := qtx.GetRequestByName(ctx, db.GetRequestByNameParams{SpaceID: spaceID, Name: requestName})
		if err != nil {
			return db.AssetRequest{}, apierr.HandleResourceError(err, "Request", name)
		}
		updated, err := qtx.UpdateRequestStateIfFrom(ctx, db.UpdateRequestStateIfFromParams{
			ID:        existing.ID,
			State:     toState,
			UpdatedBy: pgtype.UUID{},
			State_2:   fromState,
		})
		if err == nil {
			return updated, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return db.AssetRequest{}, apierr.HandleResourceError(err, "Request", name)
		}
		// Conditional UPDATE matched zero rows: either the row was
		// deleted between our pre-read and our update (unlikely but
		// possible inside the same tx if a concurrent purge raced
		// us), or another transition committed first and flipped
		// the state away from fromState.
		current, lookupErr := qtx.GetRequestByName(ctx, db.GetRequestByNameParams{SpaceID: spaceID, Name: requestName})
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return db.AssetRequest{}, apierr.NotFound("Request", name)
		}
		if lookupErr != nil {
			return db.AssetRequest{}, apierr.HandleResourceError(lookupErr, "Request", name)
		}
		return db.AssetRequest{}, apierr.FailedPrecondition(fmt.Sprintf("request must be in state %s, got %s", fromState, current.State))
	})
	if err != nil {
		return nil, err
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, resolveErr := s.resolveRequestActors(ctx, []db.AssetRequest{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "request: actor resolution failed; returning proto without audit actors", "request_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return convert.RequestToProto(result, parentName, actors), nil
}
