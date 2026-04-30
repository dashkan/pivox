package requests

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"log/slog"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
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
	queries db.Querier
	audit   *audit.Resolver
}

// Config is the constructor input for RequestsServer.
type Config struct {
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
}

// NewRequestsServer constructs the server from cfg. Panics on a
// missing required field.
func NewRequestsServer(cfg Config) *RequestsServer {
	if cfg.Queries == nil {
		panic("requests: Config.Queries is required")
	}
	return &RequestsServer{
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
		return nil, apierr.Internal("resolve actors")
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
		return nil, apierr.Internal("resolve actors")
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
		return nil, apierr.Internal("resolve asset names")
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
		return nil, apierr.Internal("database error")
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

	caller := convert.PgUUID(server.MustPivoxUserID(ctx))
	result, err := s.queries.CreateRequest(ctx, db.CreateRequestParams{
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
		return nil, apierr.HandleResourceError(err, "Request", "")
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)

	// Create line items and placeholder assets for each.
	for _, li := range request.GetLineItems() {
		lineItemName := uuid.New().String()[:12]
		assetName := uuid.New().String()[:12]

		// Create placeholder asset.
		asset, err := s.queries.CreateAsset(ctx, db.CreateAssetParams{
			ID:          uuid.New(),
			SpaceID:     spaceID,
			Name:        assetName,
			DisplayName: li.GetDisplayName(),
			State:       db.AssetStatePLACEHOLDER,
			Annotations: json.RawMessage("{}"),
			CreatedBy:   caller,
		})
		if err != nil {
			return nil, apierr.HandleResourceError(err, "Asset", "")
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

		_, err = s.queries.CreateLineItem(ctx, db.CreateLineItemParams{
			ID:          uuid.New(),
			RequestID:   result.ID,
			AssetID:     pgtype.UUID{Bytes: asset.ID, Valid: true},
			Name:        lineItemName,
			DisplayName: li.GetDisplayName(),
			Description: li.GetDescription(),
			MediaType:   mediaType,
			Annotations: liAnnotations,
			CreatedBy:   caller,
		})
		if err != nil {
			return nil, apierr.HandleResourceError(err, "LineItem", "")
		}
	}

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
		UpdatedBy: convert.PgUUID(server.MustPivoxUserID(ctx)),
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

	result, err := s.queries.UpdateRequest(ctx, updateParams)
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

	err = s.queries.DeleteRequest(ctx, existing.ID)
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
func (s *RequestsServer) AssignRequest(ctx context.Context, req *assetsv1.AssignRequestRequest) (*assetsv1.Request, error) {
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

	if existing.State != db.RequestStateOPEN && existing.State != db.RequestStateINPROGRESS {
		return nil, apierr.FailedPrecondition(fmt.Sprintf("request must be OPEN or IN_PROGRESS to assign, got %s", existing.State))
	}

	result, err := s.queries.UpdateRequestAssignee(ctx, db.UpdateRequestAssigneeParams{
		ID:        existing.ID,
		Assignee:  req.GetAssignee(),
		State:     db.RequestStateINPROGRESS,
		UpdatedBy: pgtype.UUID{},
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, resolveErr := s.resolveRequestActors(ctx, []db.AssetRequest{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "request: actor resolution failed; returning proto without audit actors", "request_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return convert.RequestToProto(result, parentName, actors), nil
}

// ClaimRequest self-assigns the caller.
func (s *RequestsServer) ClaimRequest(ctx context.Context, req *assetsv1.ClaimRequestRequest) (*assetsv1.Request, error) {
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

	if existing.State != db.RequestStateOPEN {
		return nil, apierr.FailedPrecondition(fmt.Sprintf("can only claim OPEN requests, got %s", existing.State))
	}

	// TODO: pass the caller's pivox_user_id through to the
	// `assignee` column once that column also moves to UUID FK
	// (it's currently TEXT and stores the firebase_uid). For now,
	// only the audit `updated_by` is populated with the caller's
	// UUID; `assignee` is left empty until the broader UUID
	// migration covers it.
	caller := convert.PgUUID(server.MustPivoxUserID(ctx))
	result, err := s.queries.UpdateRequestAssignee(ctx, db.UpdateRequestAssigneeParams{
		ID:        existing.ID,
		Assignee:  "",
		State:     db.RequestStateINPROGRESS,
		UpdatedBy: caller,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
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

// CancelRequest transitions any state → CANCELLED.
func (s *RequestsServer) CancelRequest(ctx context.Context, req *assetsv1.CancelRequestRequest) (*assetsv1.Request, error) {
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

	if existing.State == db.RequestStateAPPROVED || existing.State == db.RequestStateCANCELLED {
		return nil, apierr.FailedPrecondition(fmt.Sprintf("cannot cancel a request in state %s", existing.State))
	}

	result, err := s.queries.UpdateRequestState(ctx, db.UpdateRequestStateParams{
		ID:        existing.ID,
		State:     db.RequestStateCANCELLED,
		UpdatedBy: pgtype.UUID{},
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
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
func (s *RequestsServer) transitionRequest(ctx context.Context, name string, fromState, toState db.RequestState) (*assetsv1.Request, error) {
	orgName, spaceName, requestName, err := parseRequestName(name)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", name)
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{SpaceID: spaceID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", name)
	}

	if existing.State != fromState {
		return nil, apierr.FailedPrecondition(fmt.Sprintf("request must be in state %s, got %s", fromState, existing.State))
	}

	result, err := s.queries.UpdateRequestState(ctx, db.UpdateRequestStateParams{
		ID:        existing.ID,
		State:     toState,
		UpdatedBy: pgtype.UUID{},
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", name)
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, resolveErr := s.resolveRequestActors(ctx, []db.AssetRequest{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "request: actor resolution failed; returning proto without audit actors", "request_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return convert.RequestToProto(result, parentName, actors), nil
}
