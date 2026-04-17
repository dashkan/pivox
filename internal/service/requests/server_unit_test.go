package requests

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// --- helpers ---

const (
	testOrg     = "acme"
	testProject = "proj1"
	testReqName = "req-abc123"
	testParent  = "organizations/acme/projects/proj1"
	testFull    = "organizations/acme/projects/proj1/requests/req-abc123"
)

type requestFixture struct {
	orgID     uuid.UUID
	projectID uuid.UUID
	requestID uuid.UUID
	mockQ     *mocks.MockQuerier
	server    *RequestsServer
}

func setupRequestFixture(t *testing.T) requestFixture {
	t.Helper()
	f := requestFixture{
		orgID:     uuid.New(),
		projectID: uuid.New(),
		requestID: uuid.New(),
		mockQ:     new(mocks.MockQuerier),
	}
	f.server = NewRequestsServer(f.mockQ)
	return f
}

// mockResolveProject sets up the standard org+project resolution mocks.
func (f *requestFixture) mockResolveProject() {
	f.mockQ.On("GetOrganizationByName", mock.Anything, testOrg).
		Return(db.Organization{ID: f.orgID, Name: testOrg}, nil)
	f.mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{OrgID: f.orgID, Name: testProject}).
		Return(db.Project{ID: f.projectID, Name: testProject, OrgID: f.orgID}, nil)
}

func makeRequest(id, projectID uuid.UUID, name string, state db.RequestState) db.AssetRequest {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return db.AssetRequest{
		ID:          id,
		ProjectID:   projectID,
		Name:        name,
		DisplayName: "Test Request",
		Description: "A test request",
		State:       state,
		Priority:    db.RequestPriorityNORMAL,
		Assignee:    "",
		Annotations: json.RawMessage("{}"),
		CreateTime:  now,
		UpdateTime:  now,
	}
}

// --- CreateRequest ---

func TestCreateRequest_Success(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	created := makeRequest(f.requestID, f.projectID, "placeholder", db.RequestStateDRAFT)

	f.mockQ.On("CreateRequest", mock.Anything, mock.MatchedBy(func(p db.CreateRequestParams) bool {
		return p.ProjectID == f.projectID && p.State == db.RequestStateDRAFT && p.DisplayName == "New Request"
	})).Return(created, nil)

	// Line item creation: asset then line item.
	assetID := uuid.New()
	f.mockQ.On("CreateAsset", mock.Anything, mock.MatchedBy(func(p db.CreateAssetParams) bool {
		return p.ProjectID == f.projectID && p.State == db.AssetStatePLACEHOLDER
	})).Return(db.Asset{ID: assetID, Name: "asset-1", State: db.AssetStatePLACEHOLDER, Annotations: json.RawMessage("{}")}, nil)

	f.mockQ.On("CreateLineItem", mock.Anything, mock.MatchedBy(func(p db.CreateLineItemParams) bool {
		return p.RequestID == f.requestID && p.AssetID.Valid
	})).Return(db.AssetRequestLineItem{ID: uuid.New(), Name: "li-1"}, nil)

	op, err := f.server.CreateRequest(context.Background(), &assetsv1.CreateRequestRequest{
		Parent: testParent,
		Request: &assetsv1.Request{
			DisplayName: "New Request",
			Description: "description",
			LineItems: []*assetsv1.LineItem{
				{DisplayName: "Deliverable 1"},
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

func TestCreateRequest_InvalidParent(t *testing.T) {
	f := setupRequestFixture(t)

	_, err := f.server.CreateRequest(context.Background(), &assetsv1.CreateRequestRequest{
		Parent:  "bad/parent",
		Request: &assetsv1.Request{DisplayName: "X"},
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	// HandleResourceError falls through to Internal for a parse error
	// (not pgx.ErrNoRows, not duplicate key).
	assert.Equal(t, codes.Internal, st.Code())
}

// --- GetRequest ---

func TestGetRequest_Success(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateOPEN)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	li := db.AssetRequestLineItem{
		ID:          uuid.New(),
		RequestID:   f.requestID,
		Name:        "li-1",
		DisplayName: "Deliverable 1",
		State:       db.LineItemStatePENDING,
		Annotations: json.RawMessage("{}"),
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}
	f.mockQ.On("ListLineItemsByRequest", mock.Anything, db.ListLineItemsByRequestParams{
		RequestID: f.requestID,
		Limit:     100,
		Offset:    0,
	}).Return([]db.AssetRequestLineItem{li}, nil)

	f.mockQ.On("CountFulfilledLineItems", mock.Anything, f.requestID).Return(int64(0), nil)

	resp, err := f.server.GetRequest(context.Background(), &assetsv1.GetRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, testFull, resp.GetName())
	assert.Equal(t, assetsv1.Request_OPEN, resp.GetState())
	assert.Equal(t, int32(1), resp.GetLineItemCount())
	assert.Equal(t, int32(0), resp.GetFulfilledCount())
	f.mockQ.AssertExpectations(t)
}

// --- SubmitRequest (DRAFT -> OPEN) ---

func TestSubmitRequest_ValidTransition(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	transitioned := existing
	transitioned.State = db.RequestStateOPEN
	f.mockQ.On("UpdateRequestState", mock.Anything, db.UpdateRequestStateParams{
		ID:        f.requestID,
		State:     db.RequestStateOPEN,
		UpdatedBy: "",
	}).Return(transitioned, nil)

	resp, err := f.server.SubmitRequest(context.Background(), &assetsv1.SubmitRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_OPEN, resp.GetState())
	f.mockQ.AssertExpectations(t)
}

func TestSubmitRequest_InvalidState(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateINPROGRESS)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	_, err := f.server.SubmitRequest(context.Background(), &assetsv1.SubmitRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// --- AssignRequest ---

func TestAssignRequest_FromOpen(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateOPEN)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	assigned := existing
	assigned.State = db.RequestStateINPROGRESS
	assigned.Assignee = "users/jane"
	f.mockQ.On("UpdateRequestAssignee", mock.Anything, db.UpdateRequestAssigneeParams{
		ID:        f.requestID,
		Assignee:  "users/jane",
		State:     db.RequestStateINPROGRESS,
		UpdatedBy: "",
	}).Return(assigned, nil)

	resp, err := f.server.AssignRequest(context.Background(), &assetsv1.AssignRequestRequest{
		Name:     testFull,
		Assignee: "users/jane",
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_IN_PROGRESS, resp.GetState())
	assert.Equal(t, "users/jane", resp.GetAssignee())
	f.mockQ.AssertExpectations(t)
}

func TestAssignRequest_FromInProgress(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateINPROGRESS)
	existing.Assignee = "users/jane"
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	reassigned := existing
	reassigned.Assignee = "users/bob"
	f.mockQ.On("UpdateRequestAssignee", mock.Anything, db.UpdateRequestAssigneeParams{
		ID:        f.requestID,
		Assignee:  "users/bob",
		State:     db.RequestStateINPROGRESS,
		UpdatedBy: "",
	}).Return(reassigned, nil)

	resp, err := f.server.AssignRequest(context.Background(), &assetsv1.AssignRequestRequest{
		Name:     testFull,
		Assignee: "users/bob",
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_IN_PROGRESS, resp.GetState())
	assert.Equal(t, "users/bob", resp.GetAssignee())
	f.mockQ.AssertExpectations(t)
}

func TestAssignRequest_InvalidState(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	_, err := f.server.AssignRequest(context.Background(), &assetsv1.AssignRequestRequest{
		Name:     testFull,
		Assignee: "users/jane",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// --- ClaimRequest ---

func TestClaimRequest_Success(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateOPEN)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	claimed := existing
	claimed.State = db.RequestStateINPROGRESS
	f.mockQ.On("UpdateRequestAssignee", mock.Anything, db.UpdateRequestAssigneeParams{
		ID:        f.requestID,
		Assignee:  "",
		State:     db.RequestStateINPROGRESS,
		UpdatedBy: "",
	}).Return(claimed, nil)

	resp, err := f.server.ClaimRequest(context.Background(), &assetsv1.ClaimRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_IN_PROGRESS, resp.GetState())
	f.mockQ.AssertExpectations(t)
}

func TestClaimRequest_InvalidState(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateINPROGRESS)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	_, err := f.server.ClaimRequest(context.Background(), &assetsv1.ClaimRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// --- DeliverRequest (IN_PROGRESS -> DELIVERED) ---

func TestDeliverRequest_Success(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateINPROGRESS)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	delivered := existing
	delivered.State = db.RequestStateDELIVERED
	f.mockQ.On("UpdateRequestState", mock.Anything, db.UpdateRequestStateParams{
		ID:        f.requestID,
		State:     db.RequestStateDELIVERED,
		UpdatedBy: "",
	}).Return(delivered, nil)

	resp, err := f.server.DeliverRequest(context.Background(), &assetsv1.DeliverRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_DELIVERED, resp.GetState())
	f.mockQ.AssertExpectations(t)
}

func TestDeliverRequest_InvalidState(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateOPEN)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	_, err := f.server.DeliverRequest(context.Background(), &assetsv1.DeliverRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// --- ApproveRequest (DELIVERED -> APPROVED) ---

func TestApproveRequest_Success(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDELIVERED)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	approved := existing
	approved.State = db.RequestStateAPPROVED
	f.mockQ.On("UpdateRequestState", mock.Anything, db.UpdateRequestStateParams{
		ID:        f.requestID,
		State:     db.RequestStateAPPROVED,
		UpdatedBy: "",
	}).Return(approved, nil)

	resp, err := f.server.ApproveRequest(context.Background(), &assetsv1.ApproveRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_APPROVED, resp.GetState())
	f.mockQ.AssertExpectations(t)
}

func TestApproveRequest_InvalidState(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateINPROGRESS)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	_, err := f.server.ApproveRequest(context.Background(), &assetsv1.ApproveRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// --- RequestRevision (DELIVERED -> REVISION_REQUESTED) ---

func TestRequestRevision_Success(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDELIVERED)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	revised := existing
	revised.State = db.RequestStateREVISIONREQUESTED
	f.mockQ.On("UpdateRequestState", mock.Anything, db.UpdateRequestStateParams{
		ID:        f.requestID,
		State:     db.RequestStateREVISIONREQUESTED,
		UpdatedBy: "",
	}).Return(revised, nil)

	resp, err := f.server.RequestRevision(context.Background(), &assetsv1.RequestRevisionRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_REVISION_REQUESTED, resp.GetState())
	f.mockQ.AssertExpectations(t)
}

// --- RejectRequest (DELIVERED -> REJECTED) ---

func TestRejectRequest_Success(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDELIVERED)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	rejected := existing
	rejected.State = db.RequestStateREJECTED
	f.mockQ.On("UpdateRequestState", mock.Anything, db.UpdateRequestStateParams{
		ID:        f.requestID,
		State:     db.RequestStateREJECTED,
		UpdatedBy: "",
	}).Return(rejected, nil)

	resp, err := f.server.RejectRequest(context.Background(), &assetsv1.RejectRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_REJECTED, resp.GetState())
	f.mockQ.AssertExpectations(t)
}

// --- CancelRequest ---

func TestCancelRequest_FromDraft(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	cancelled := existing
	cancelled.State = db.RequestStateCANCELLED
	f.mockQ.On("UpdateRequestState", mock.Anything, db.UpdateRequestStateParams{
		ID:        f.requestID,
		State:     db.RequestStateCANCELLED,
		UpdatedBy: "",
	}).Return(cancelled, nil)

	resp, err := f.server.CancelRequest(context.Background(), &assetsv1.CancelRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_CANCELLED, resp.GetState())
	f.mockQ.AssertExpectations(t)
}

func TestCancelRequest_FromOpen(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateOPEN)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	cancelled := existing
	cancelled.State = db.RequestStateCANCELLED
	f.mockQ.On("UpdateRequestState", mock.Anything, db.UpdateRequestStateParams{
		ID:        f.requestID,
		State:     db.RequestStateCANCELLED,
		UpdatedBy: "",
	}).Return(cancelled, nil)

	resp, err := f.server.CancelRequest(context.Background(), &assetsv1.CancelRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_CANCELLED, resp.GetState())
	f.mockQ.AssertExpectations(t)
}

func TestCancelRequest_FromInProgress(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateINPROGRESS)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	cancelled := existing
	cancelled.State = db.RequestStateCANCELLED
	f.mockQ.On("UpdateRequestState", mock.Anything, db.UpdateRequestStateParams{
		ID:        f.requestID,
		State:     db.RequestStateCANCELLED,
		UpdatedBy: "",
	}).Return(cancelled, nil)

	resp, err := f.server.CancelRequest(context.Background(), &assetsv1.CancelRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.Equal(t, assetsv1.Request_CANCELLED, resp.GetState())
	f.mockQ.AssertExpectations(t)
}

func TestCancelRequest_InvalidState_Approved(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateAPPROVED)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	_, err := f.server.CancelRequest(context.Background(), &assetsv1.CancelRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestCancelRequest_InvalidState_Cancelled(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateCANCELLED)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	_, err := f.server.CancelRequest(context.Background(), &assetsv1.CancelRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// --- UpdateRequest ---

func TestUpdateRequest_WithFieldMask(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	updated := existing
	updated.DisplayName = "Updated"
	updated.Description = "New description"
	f.mockQ.On("UpdateRequest", mock.Anything, mock.MatchedBy(func(p db.UpdateRequestParams) bool {
		return p.ID == f.requestID &&
			p.DisplayName.Valid && p.DisplayName.String == "Updated" &&
			p.Description.Valid && p.Description.String == "New description" &&
			p.Priority.Valid && p.Priority.RequestPriority == db.RequestPriority("HIGH")
	})).Return(updated, nil)

	op, err := f.server.UpdateRequest(context.Background(), &assetsv1.UpdateRequestRequest{
		Request: &assetsv1.Request{
			Name:        testFull,
			DisplayName: "Updated",
			Description: "New description",
			Priority:    assetsv1.Request_HIGH,
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"display_name", "description", "priority"},
		},
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

func TestUpdateRequest_NoMask(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	updated := existing
	updated.DisplayName = "Full Update"
	updated.Description = "Full description"
	f.mockQ.On("UpdateRequest", mock.Anything, mock.MatchedBy(func(p db.UpdateRequestParams) bool {
		return p.ID == f.requestID &&
			p.DisplayName.Valid && p.DisplayName.String == "Full Update" &&
			p.Description.Valid && p.Description.String == "Full description"
	})).Return(updated, nil)

	op, err := f.server.UpdateRequest(context.Background(), &assetsv1.UpdateRequestRequest{
		Request: &assetsv1.Request{
			Name:        testFull,
			DisplayName: "Full Update",
			Description: "Full description",
		},
		// No UpdateMask
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

// --- DeleteRequest ---

func TestDeleteRequest_Success(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	f.mockQ.On("DeleteRequest", mock.Anything, f.requestID).Return(nil)

	op, err := f.server.DeleteRequest(context.Background(), &assetsv1.DeleteRequestRequest{
		Name: testFull,
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())

	// Verify the returned proto has CANCELLED state.
	var req assetsv1.Request
	require.NoError(t, op.GetResponse().UnmarshalTo(&req))
	assert.Equal(t, assetsv1.Request_CANCELLED, req.GetState())
	f.mockQ.AssertExpectations(t)
}

// --- CreateRequest additional error paths ---

func TestCreateRequest_OrgNotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockQ.On("GetOrganizationByName", mock.Anything, testOrg).
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := f.server.CreateRequest(context.Background(), &assetsv1.CreateRequestRequest{
		Parent:  testParent,
		Request: &assetsv1.Request{DisplayName: "X"},
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestCreateRequest_DBError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("CreateRequest", mock.Anything, mock.Anything).
		Return(db.AssetRequest{}, errors.New("connection reset"))

	_, err := f.server.CreateRequest(context.Background(), &assetsv1.CreateRequestRequest{
		Parent:  testParent,
		Request: &assetsv1.Request{DisplayName: "New Request"},
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestCreateRequest_CreateAssetError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	created := makeRequest(f.requestID, f.projectID, "placeholder", db.RequestStateDRAFT)
	f.mockQ.On("CreateRequest", mock.Anything, mock.Anything).Return(created, nil)

	f.mockQ.On("CreateAsset", mock.Anything, mock.Anything).
		Return(db.Asset{}, errors.New("asset insert failed"))

	_, err := f.server.CreateRequest(context.Background(), &assetsv1.CreateRequestRequest{
		Parent: testParent,
		Request: &assetsv1.Request{
			DisplayName: "New Request",
			LineItems:   []*assetsv1.LineItem{{DisplayName: "Deliverable 1"}},
		},
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestCreateRequest_CreateLineItemError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	created := makeRequest(f.requestID, f.projectID, "placeholder", db.RequestStateDRAFT)
	f.mockQ.On("CreateRequest", mock.Anything, mock.Anything).Return(created, nil)

	assetID := uuid.New()
	f.mockQ.On("CreateAsset", mock.Anything, mock.Anything).
		Return(db.Asset{ID: assetID, Name: "asset-1", State: db.AssetStatePLACEHOLDER, Annotations: json.RawMessage("{}")}, nil)

	f.mockQ.On("CreateLineItem", mock.Anything, mock.Anything).
		Return(db.AssetRequestLineItem{}, errors.New("line item insert failed"))

	_, err := f.server.CreateRequest(context.Background(), &assetsv1.CreateRequestRequest{
		Parent: testParent,
		Request: &assetsv1.Request{
			DisplayName: "New Request",
			LineItems:   []*assetsv1.LineItem{{DisplayName: "Deliverable 1"}},
		},
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestCreateRequest_NoLineItems_Success(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	created := makeRequest(f.requestID, f.projectID, "placeholder", db.RequestStateDRAFT)
	f.mockQ.On("CreateRequest", mock.Anything, mock.Anything).Return(created, nil)

	op, err := f.server.CreateRequest(context.Background(), &assetsv1.CreateRequestRequest{
		Parent:  testParent,
		Request: &assetsv1.Request{DisplayName: "New Request"},
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

// --- GetRequest additional error paths ---

func TestGetRequest_InvalidName(t *testing.T) {
	f := setupRequestFixture(t)

	_, err := f.server.GetRequest(context.Background(), &assetsv1.GetRequestRequest{
		Name: "bad/name",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestGetRequest_OrgNotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockQ.On("GetOrganizationByName", mock.Anything, testOrg).
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := f.server.GetRequest(context.Background(), &assetsv1.GetRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestGetRequest_NotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(db.AssetRequest{}, pgx.ErrNoRows)

	_, err := f.server.GetRequest(context.Background(), &assetsv1.GetRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- UpdateRequest additional error paths ---

func TestUpdateRequest_InvalidName(t *testing.T) {
	f := setupRequestFixture(t)

	_, err := f.server.UpdateRequest(context.Background(), &assetsv1.UpdateRequestRequest{
		Request: &assetsv1.Request{Name: "bad/name/format"},
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestUpdateRequest_NotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(db.AssetRequest{}, pgx.ErrNoRows)

	_, err := f.server.UpdateRequest(context.Background(), &assetsv1.UpdateRequestRequest{
		Request: &assetsv1.Request{Name: testFull, DisplayName: "Updated"},
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestUpdateRequest_DBError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	f.mockQ.On("UpdateRequest", mock.Anything, mock.Anything).
		Return(db.AssetRequest{}, errors.New("db failure"))

	_, err := f.server.UpdateRequest(context.Background(), &assetsv1.UpdateRequestRequest{
		Request: &assetsv1.Request{Name: testFull, DisplayName: "Updated"},
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestUpdateRequest_DueTimeFieldMask(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	due := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	updated := existing
	f.mockQ.On("UpdateRequest", mock.Anything, mock.MatchedBy(func(p db.UpdateRequestParams) bool {
		return p.ID == f.requestID && p.DueTime.Valid && p.DueTime.Time.Equal(due)
	})).Return(updated, nil)

	op, err := f.server.UpdateRequest(context.Background(), &assetsv1.UpdateRequestRequest{
		Request: &assetsv1.Request{
			Name:    testFull,
			DueTime: timestamppb.New(due),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"due_time"}},
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

func TestUpdateRequest_AnnotationsFieldMask(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	updated := existing
	f.mockQ.On("UpdateRequest", mock.Anything, mock.MatchedBy(func(p db.UpdateRequestParams) bool {
		return p.ID == f.requestID && p.Annotations != nil
	})).Return(updated, nil)

	op, err := f.server.UpdateRequest(context.Background(), &assetsv1.UpdateRequestRequest{
		Request: &assetsv1.Request{
			Name: testFull,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"annotations"}},
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

// --- DeleteRequest additional error paths ---

func TestDeleteRequest_InvalidName(t *testing.T) {
	f := setupRequestFixture(t)

	_, err := f.server.DeleteRequest(context.Background(), &assetsv1.DeleteRequestRequest{
		Name: "bad/name",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestDeleteRequest_NotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(db.AssetRequest{}, pgx.ErrNoRows)

	_, err := f.server.DeleteRequest(context.Background(), &assetsv1.DeleteRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestDeleteRequest_SoftDeleteError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	f.mockQ.On("DeleteRequest", mock.Anything, f.requestID).Return(errors.New("db failure"))

	_, err := f.server.DeleteRequest(context.Background(), &assetsv1.DeleteRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- AssignRequest additional error paths ---

func TestAssignRequest_InvalidName(t *testing.T) {
	f := setupRequestFixture(t)

	_, err := f.server.AssignRequest(context.Background(), &assetsv1.AssignRequestRequest{
		Name:     "bad/name",
		Assignee: "users/jane",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestAssignRequest_NotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(db.AssetRequest{}, pgx.ErrNoRows)

	_, err := f.server.AssignRequest(context.Background(), &assetsv1.AssignRequestRequest{
		Name:     testFull,
		Assignee: "users/jane",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestAssignRequest_UpdateAssigneeDBError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateOPEN)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	f.mockQ.On("UpdateRequestAssignee", mock.Anything, mock.Anything).
		Return(db.AssetRequest{}, errors.New("db failure"))

	_, err := f.server.AssignRequest(context.Background(), &assetsv1.AssignRequestRequest{
		Name:     testFull,
		Assignee: "users/jane",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- ClaimRequest additional error paths ---

func TestClaimRequest_InvalidName(t *testing.T) {
	f := setupRequestFixture(t)

	_, err := f.server.ClaimRequest(context.Background(), &assetsv1.ClaimRequestRequest{
		Name: "bad/name",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestClaimRequest_NotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(db.AssetRequest{}, pgx.ErrNoRows)

	_, err := f.server.ClaimRequest(context.Background(), &assetsv1.ClaimRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestClaimRequest_UpdateAssigneeDBError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateOPEN)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	f.mockQ.On("UpdateRequestAssignee", mock.Anything, mock.Anything).
		Return(db.AssetRequest{}, errors.New("db failure"))

	_, err := f.server.ClaimRequest(context.Background(), &assetsv1.ClaimRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- CancelRequest additional error paths ---

func TestCancelRequest_InvalidName(t *testing.T) {
	f := setupRequestFixture(t)

	_, err := f.server.CancelRequest(context.Background(), &assetsv1.CancelRequestRequest{
		Name: "bad/name",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestCancelRequest_NotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(db.AssetRequest{}, pgx.ErrNoRows)

	_, err := f.server.CancelRequest(context.Background(), &assetsv1.CancelRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestCancelRequest_UpdateStateDBError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateOPEN)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	f.mockQ.On("UpdateRequestState", mock.Anything, mock.Anything).
		Return(db.AssetRequest{}, errors.New("db failure"))

	_, err := f.server.CancelRequest(context.Background(), &assetsv1.CancelRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- transitionRequest error paths (via SubmitRequest) ---

func TestSubmitRequest_InvalidName(t *testing.T) {
	f := setupRequestFixture(t)

	_, err := f.server.SubmitRequest(context.Background(), &assetsv1.SubmitRequestRequest{
		Name: "bad/name",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestSubmitRequest_NotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(db.AssetRequest{}, pgx.ErrNoRows)

	_, err := f.server.SubmitRequest(context.Background(), &assetsv1.SubmitRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestSubmitRequest_UpdateStateDBError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	existing := makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateDRAFT)
	f.mockQ.On("GetRequestByName", mock.Anything, db.GetRequestByNameParams{ProjectID: f.projectID, Name: testReqName}).
		Return(existing, nil)

	f.mockQ.On("UpdateRequestState", mock.Anything, mock.Anything).
		Return(db.AssetRequest{}, errors.New("db failure"))

	_, err := f.server.SubmitRequest(context.Background(), &assetsv1.SubmitRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- ListRequests error paths ---

func TestListRequests_InvalidParent(t *testing.T) {
	f := setupRequestFixture(t)

	_, err := f.server.ListRequests(context.Background(), &assetsv1.ListRequestsRequest{
		Parent: "bad/parent",
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestListRequests_OrgNotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockQ.On("GetOrganizationByName", mock.Anything, testOrg).
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := f.server.ListRequests(context.Background(), &assetsv1.ListRequestsRequest{
		Parent: testParent,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestListRequests_DBError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("ListRequestsByProject", mock.Anything, mock.Anything).
		Return([]db.AssetRequest{}, errors.New("db failure"))

	_, err := f.server.ListRequests(context.Background(), &assetsv1.ListRequestsRequest{
		Parent: testParent,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestListRequests_ShowDeleted(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	rows := []db.AssetRequest{
		makeRequest(f.requestID, f.projectID, testReqName, db.RequestStateCANCELLED),
	}
	f.mockQ.On("ListRequestsByProject", mock.Anything, mock.Anything).
		Return(rows, nil)

	resp, err := f.server.ListRequests(context.Background(), &assetsv1.ListRequestsRequest{
		Parent:      testParent,
		ShowDeleted: true,
	})

	require.NoError(t, err)
	assert.Len(t, resp.GetRequests(), 1)
	f.mockQ.AssertExpectations(t)
}

func TestListRequests_ShowDeleted_DBError(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("ListRequestsByProject", mock.Anything, mock.Anything).
		Return([]db.AssetRequest{}, errors.New("db failure"))

	_, err := f.server.ListRequests(context.Background(), &assetsv1.ListRequestsRequest{
		Parent:      testParent,
		ShowDeleted: true,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestListRequests_Pagination(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	// Return pageSize+1 rows to trigger next page token.
	rows := make([]db.AssetRequest, 3)
	for i := range rows {
		rows[i] = makeRequest(uuid.New(), f.projectID, "req-"+uuid.New().String()[:8], db.RequestStateOPEN)
	}
	f.mockQ.On("ListRequestsByProject", mock.Anything, mock.MatchedBy(func(p db.ListRequestsByProjectParams) bool {
		return p.ProjectID == f.projectID && p.Limit == 3
	})).Return(rows, nil)

	resp, err := f.server.ListRequests(context.Background(), &assetsv1.ListRequestsRequest{
		Parent:   testParent,
		PageSize: 2,
	})

	require.NoError(t, err)
	assert.Len(t, resp.GetRequests(), 2)
	assert.NotEmpty(t, resp.GetNextPageToken())
	f.mockQ.AssertExpectations(t)
}

func TestListRequests_PageSizeClamped(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("ListRequestsByProject", mock.Anything, mock.MatchedBy(func(p db.ListRequestsByProjectParams) bool {
		return p.Limit == 1001 // 1000 + 1 for next-page detection
	})).Return([]db.AssetRequest{}, nil)

	_, err := f.server.ListRequests(context.Background(), &assetsv1.ListRequestsRequest{
		Parent:   testParent,
		PageSize: 9999,
	})

	require.NoError(t, err)
	f.mockQ.AssertExpectations(t)
}

func TestListRequests_DefaultPageSize(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockResolveProject()

	f.mockQ.On("ListRequestsByProject", mock.Anything, mock.MatchedBy(func(p db.ListRequestsByProjectParams) bool {
		return p.Limit == 101 // default 100 + 1
	})).Return([]db.AssetRequest{}, nil)

	_, err := f.server.ListRequests(context.Background(), &assetsv1.ListRequestsRequest{
		Parent: testParent,
	})

	require.NoError(t, err)
	f.mockQ.AssertExpectations(t)
}

// --- resolveProject error paths ---

func TestResolveProject_ProjectNotFound(t *testing.T) {
	f := setupRequestFixture(t)
	f.mockQ.On("GetOrganizationByName", mock.Anything, testOrg).
		Return(db.Organization{ID: f.orgID, Name: testOrg}, nil)
	f.mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{OrgID: f.orgID, Name: testProject}).
		Return(db.Project{}, pgx.ErrNoRows)

	_, err := f.server.GetRequest(context.Background(), &assetsv1.GetRequestRequest{
		Name: testFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- unused import guard ---

var _ = pgtype.UUID{}
var _ = errors.New
