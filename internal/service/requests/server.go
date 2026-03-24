package requests

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
)

type RequestsServer struct {
	assetsv1.UnimplementedRequestsServer
	db      db.DBTX
	queries *db.Queries
}

func NewRequestsServer(pool db.DBTX, queries *db.Queries) *RequestsServer {
	return &RequestsServer{
		db:      pool,
		queries: queries,
	}
}

// parseRequestName parses "organizations/{org}/projects/{project}/requests/{request}".
func parseRequestName(name string) (orgName, projectName, requestName string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "projects" || parts[4] != "requests" {
		return "", "", "", fmt.Errorf("invalid request name %q", name)
	}
	return parts[1], parts[3], parts[5], nil
}

// parseRequestParent parses "organizations/{org}/projects/{project}".
func parseRequestParent(parent string) (orgName, projectName string, err error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "projects" {
		return "", "", fmt.Errorf("invalid parent %q", parent)
	}
	return parts[1], parts[3], nil
}

// resolveProject resolves org name + project name to project UUID.
func (s *RequestsServer) resolveProject(ctx context.Context, orgName, projectName string) (uuid.UUID, error) {
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	project, err := s.queries.GetProjectByName(ctx, db.GetProjectByNameParams{OrgID: org.ID, Name: projectName})
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Project", fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName))
	}
	return project.ID, nil
}

func (s *RequestsServer) GetRequest(ctx context.Context, req *assetsv1.GetRequestRequest) (*assetsv1.Request, error) {
	orgName, projectName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	request, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{ProjectID: projectID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	proto := convert.RequestToProto(request, parentName)

	// Populate line items, counts.
	lineItems, err := s.queries.ListLineItemsByRequest(ctx, db.ListLineItemsByRequestParams{
		RequestID: request.ID,
		Limit:     100,
		Offset:    0,
	})
	if err == nil {
		requestFullName := fmt.Sprintf("%s/requests/%s", parentName, requestName)
		for _, li := range lineItems {
			proto.LineItems = append(proto.LineItems, convert.LineItemToProto(li, requestFullName, parentName))
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
	orgName, projectName, err := parseRequestParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetParent())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
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

	var rows []db.Request
	if req.GetShowDeleted() {
		rows, err = s.queries.ListRequestsByProjectWithDeleted(ctx, db.ListRequestsByProjectWithDeletedParams{
			ProjectID: projectID,
			Limit:     pageSize + 1,
			Offset:    0,
		})
	} else {
		rows, err = s.queries.ListRequestsByProject(ctx, db.ListRequestsByProjectParams{
			ProjectID: projectID,
			Limit:     pageSize + 1,
			Offset:    0,
		})
	}
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken = rows[pageSize].ID.String()
		rows = rows[:pageSize]
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	requests := make([]*assetsv1.Request, 0, len(rows))
	for _, r := range rows {
		requests = append(requests, convert.RequestToProto(r, parentName))
	}

	return &assetsv1.ListRequestsResponse{
		Requests:      requests,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *RequestsServer) CreateRequest(ctx context.Context, req *assetsv1.CreateRequestRequest) (*longrunningpb.Operation, error) {
	request := req.GetRequest()
	orgName, projectName, err := parseRequestParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetParent())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
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

	result, err := s.queries.CreateRequest(ctx, db.CreateRequestParams{
		ID:          uuid.New(),
		ProjectID:   projectID,
		Name:        requestName,
		DisplayName: request.GetDisplayName(),
		Description: request.GetDescription(),
		State:       db.RequestStateDRAFT,
		Priority:    priority,
		Assignee:    request.GetAssignee(),
		DueTime:     dueTime,
		CreatedBy:   "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", "")
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)

	// Create line items and placeholder assets for each.
	for _, li := range request.GetLineItems() {
		lineItemName := uuid.New().String()[:12]
		assetName := uuid.New().String()[:12]

		// Create placeholder asset.
		asset, err := s.queries.CreateAsset(ctx, db.CreateAssetParams{
			ID:          uuid.New(),
			ProjectID:   projectID,
			Name:        assetName,
			DisplayName: li.GetDisplayName(),
			State:       db.AssetStatePLACEHOLDER,
			Annotations: json.RawMessage("{}"),
			CreatedBy:   "",
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
			CreatedBy:   "",
		})
		if err != nil {
			return nil, apierr.HandleResourceError(err, "LineItem", "")
		}
	}

	return lro.DoneOperation(convert.RequestToProto(result, parentName))
}

func (s *RequestsServer) UpdateRequest(ctx context.Context, req *assetsv1.UpdateRequestRequest) (*longrunningpb.Operation, error) {
	request := req.GetRequest()
	orgName, projectName, requestName, err := parseRequestName(request.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", request.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{ProjectID: projectID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", request.GetName())
	}

	updateParams := db.UpdateRequestParams{
		ID:        existing.ID,
		UpdatedBy: "",
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

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	return lro.DoneOperation(convert.RequestToProto(result, parentName))
}

func (s *RequestsServer) DeleteRequest(ctx context.Context, req *assetsv1.DeleteRequestRequest) (*longrunningpb.Operation, error) {
	orgName, projectName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{ProjectID: projectID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	err = s.queries.SoftDeleteRequest(ctx, db.SoftDeleteRequestParams{
		ID:        existing.ID,
		DeletedBy: "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	existing.State = db.RequestStateCANCELLED
	return lro.DoneOperation(convert.RequestToProto(existing, parentName))
}

// SubmitRequest transitions DRAFT → OPEN.
func (s *RequestsServer) SubmitRequest(ctx context.Context, req *assetsv1.SubmitRequestRequest) (*assetsv1.Request, error) {
	return s.transitionRequest(ctx, req.GetName(), db.RequestStateDRAFT, db.RequestStateOPEN)
}

// AssignRequest sets the assignee and transitions OPEN → IN_PROGRESS.
func (s *RequestsServer) AssignRequest(ctx context.Context, req *assetsv1.AssignRequestRequest) (*assetsv1.Request, error) {
	orgName, projectName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{ProjectID: projectID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	if existing.State != db.RequestStateOPEN && existing.State != db.RequestStateINPROGRESS {
		return nil, status.Errorf(codes.FailedPrecondition, "request must be OPEN or IN_PROGRESS to assign, got %s", existing.State)
	}

	result, err := s.queries.UpdateRequestAssignee(ctx, db.UpdateRequestAssigneeParams{
		ID:        existing.ID,
		Assignee:  req.GetAssignee(),
		State:     db.RequestStateINPROGRESS,
		UpdatedBy: "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	return convert.RequestToProto(result, parentName), nil
}

// ClaimRequest self-assigns the caller.
func (s *RequestsServer) ClaimRequest(ctx context.Context, req *assetsv1.ClaimRequestRequest) (*assetsv1.Request, error) {
	orgName, projectName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{ProjectID: projectID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	if existing.State != db.RequestStateOPEN {
		return nil, status.Errorf(codes.FailedPrecondition, "can only claim OPEN requests, got %s", existing.State)
	}

	// TODO: get caller identity from context
	caller := ""

	result, err := s.queries.UpdateRequestAssignee(ctx, db.UpdateRequestAssigneeParams{
		ID:        existing.ID,
		Assignee:  caller,
		State:     db.RequestStateINPROGRESS,
		UpdatedBy: caller,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	return convert.RequestToProto(result, parentName), nil
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
	orgName, projectName, requestName, err := parseRequestName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{ProjectID: projectID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	if existing.State == db.RequestStateAPPROVED || existing.State == db.RequestStateCANCELLED {
		return nil, status.Errorf(codes.FailedPrecondition, "cannot cancel a request in state %s", existing.State)
	}

	result, err := s.queries.UpdateRequestState(ctx, db.UpdateRequestStateParams{
		ID:        existing.ID,
		State:     db.RequestStateCANCELLED,
		UpdatedBy: "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	return convert.RequestToProto(result, parentName), nil
}

// transitionRequest is a helper for simple state transitions.
func (s *RequestsServer) transitionRequest(ctx context.Context, name string, fromState, toState db.RequestState) (*assetsv1.Request, error) {
	orgName, projectName, requestName, err := parseRequestName(name)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", name)
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetRequestByName(ctx, db.GetRequestByNameParams{ProjectID: projectID, Name: requestName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", name)
	}

	if existing.State != fromState {
		return nil, status.Errorf(codes.FailedPrecondition, "request must be in state %s, got %s", fromState, existing.State)
	}

	result, err := s.queries.UpdateRequestState(ctx, db.UpdateRequestStateParams{
		ID:        existing.ID,
		State:     toState,
		UpdatedBy: "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Request", name)
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	return convert.RequestToProto(result, parentName), nil
}
