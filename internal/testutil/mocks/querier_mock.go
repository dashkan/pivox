package mocks

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// MockQuerier implements db.Querier using testify/mock.
// Only methods actually called by existing services are implemented.
// Add more as tests require them.
type MockQuerier struct {
	mock.Mock
}

// Compile-time check that MockQuerier implements db.Querier.
var _ db.Querier = (*MockQuerier)(nil)

// --- Operations (LRO) ---

func (m *MockQuerier) CancelOperation(ctx context.Context, id uuid.UUID) (db.Operation, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Operation), args.Error(1)
}

func (m *MockQuerier) CompleteOperation(ctx context.Context, arg db.CompleteOperationParams) (db.Operation, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Operation), args.Error(1)
}

func (m *MockQuerier) CreateOperation(ctx context.Context, arg db.CreateOperationParams) (db.Operation, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Operation), args.Error(1)
}

func (m *MockQuerier) DeleteExpiredOperations(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockQuerier) DeleteOperation(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) FailOperation(ctx context.Context, arg db.FailOperationParams) (db.Operation, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Operation), args.Error(1)
}

func (m *MockQuerier) GetOperation(ctx context.Context, id uuid.UUID) (db.Operation, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Operation), args.Error(1)
}

func (m *MockQuerier) ListOperations(ctx context.Context, arg db.ListOperationsParams) ([]db.Operation, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Operation), args.Error(1)
}

func (m *MockQuerier) ListPendingOperations(ctx context.Context) ([]db.Operation, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.Operation), args.Error(1)
}

// --- Auth ---

func (m *MockQuerier) ConsumeAuthTokenCode(ctx context.Context, code uuid.UUID) (db.AuthTokenCode, error) {
	args := m.Called(ctx, code)
	return args.Get(0).(db.AuthTokenCode), args.Error(1)
}

func (m *MockQuerier) CreateAuthTokenCode(ctx context.Context, idToken string) (db.AuthTokenCode, error) {
	args := m.Called(ctx, idToken)
	return args.Get(0).(db.AuthTokenCode), args.Error(1)
}

func (m *MockQuerier) DeleteExpiredAuthTokenCodes(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockQuerier) CreateDelegatedAuthSession(ctx context.Context, arg db.CreateDelegatedAuthSessionParams) (db.DelegatedAuthSession, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.DelegatedAuthSession), args.Error(1)
}

func (m *MockQuerier) CompleteDelegatedAuthSession(ctx context.Context, arg db.CompleteDelegatedAuthSessionParams) (db.DelegatedAuthSession, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.DelegatedAuthSession), args.Error(1)
}

func (m *MockQuerier) ConsumeDelegatedAuthSession(ctx context.Context, code uuid.UUID) (pgtype.Text, error) {
	args := m.Called(ctx, code)
	return args.Get(0).(pgtype.Text), args.Error(1)
}

func (m *MockQuerier) GetDelegatedAuthSessionStatus(ctx context.Context, code uuid.UUID) (string, error) {
	args := m.Called(ctx, code)
	return args.String(0), args.Error(1)
}

func (m *MockQuerier) DeleteExpiredDelegatedAuthSessions(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockQuerier) UpsertAccount(ctx context.Context, arg db.UpsertAccountParams) (db.Account, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Account), args.Error(1)
}

func (m *MockQuerier) GetAccountByFirebaseUID(ctx context.Context, firebaseUid string) (db.Account, error) {
	args := m.Called(ctx, firebaseUid)
	return args.Get(0).(db.Account), args.Error(1)
}

// --- Organizations ---

func (m *MockQuerier) CreateOrganization(ctx context.Context, arg db.CreateOrganizationParams) (db.Organization, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Organization), args.Error(1)
}

func (m *MockQuerier) GetOrganization(ctx context.Context, id uuid.UUID) (db.Organization, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Organization), args.Error(1)
}

func (m *MockQuerier) GetOrganizationByName(ctx context.Context, name string) (db.Organization, error) {
	args := m.Called(ctx, name)
	return args.Get(0).(db.Organization), args.Error(1)
}

func (m *MockQuerier) SetOrganizationTenantID(ctx context.Context, arg db.SetOrganizationTenantIDParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

// --- Projects ---

func (m *MockQuerier) CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Project, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Project), args.Error(1)
}

func (m *MockQuerier) GetProject(ctx context.Context, id uuid.UUID) (db.Project, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Project), args.Error(1)
}

func (m *MockQuerier) GetProjectByName(ctx context.Context, arg db.GetProjectByNameParams) (db.Project, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Project), args.Error(1)
}

func (m *MockQuerier) GetProjectIncludingDeleted(ctx context.Context, id uuid.UUID) (db.Project, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Project), args.Error(1)
}

func (m *MockQuerier) UpdateProject(ctx context.Context, arg db.UpdateProjectParams) (db.Project, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Project), args.Error(1)
}

func (m *MockQuerier) SoftDeleteProject(ctx context.Context, arg db.SoftDeleteProjectParams) (db.Project, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Project), args.Error(1)
}

func (m *MockQuerier) UndeleteProject(ctx context.Context, arg db.UndeleteProjectParams) (db.Project, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Project), args.Error(1)
}

// --- API Keys ---

func (m *MockQuerier) CreateApiKey(ctx context.Context, arg db.CreateApiKeyParams) (db.ApiKey, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ApiKey), args.Error(1)
}

func (m *MockQuerier) GetApiKey(ctx context.Context, id uuid.UUID) (db.ApiKey, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.ApiKey), args.Error(1)
}

func (m *MockQuerier) GetApiKeyByOrgAndKeyID(ctx context.Context, arg db.GetApiKeyByOrgAndKeyIDParams) (db.ApiKey, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ApiKey), args.Error(1)
}

func (m *MockQuerier) GetApiKeyIncludingDeleted(ctx context.Context, id uuid.UUID) (db.ApiKey, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.ApiKey), args.Error(1)
}

func (m *MockQuerier) GetApiKeyString(ctx context.Context, id uuid.UUID) (string, error) {
	args := m.Called(ctx, id)
	return args.String(0), args.Error(1)
}

func (m *MockQuerier) LookupApiKeyByKeyString(ctx context.Context, keyString string) (db.ApiKey, error) {
	args := m.Called(ctx, keyString)
	return args.Get(0).(db.ApiKey), args.Error(1)
}

func (m *MockQuerier) UpdateApiKey(ctx context.Context, arg db.UpdateApiKeyParams) (db.ApiKey, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ApiKey), args.Error(1)
}

func (m *MockQuerier) SoftDeleteApiKey(ctx context.Context, arg db.SoftDeleteApiKeyParams) (db.ApiKey, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ApiKey), args.Error(1)
}

func (m *MockQuerier) UndeleteApiKey(ctx context.Context, arg db.UndeleteApiKeyParams) (db.ApiKey, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ApiKey), args.Error(1)
}

// --- Tags ---

func (m *MockQuerier) CreateTagKey(ctx context.Context, arg db.CreateTagKeyParams) (db.TagKey, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.TagKey), args.Error(1)
}

func (m *MockQuerier) GetTagKey(ctx context.Context, id uuid.UUID) (db.TagKey, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.TagKey), args.Error(1)
}

func (m *MockQuerier) GetTagKeyByNamespacedName(ctx context.Context, namespacedName string) (db.TagKey, error) {
	args := m.Called(ctx, namespacedName)
	return args.Get(0).(db.TagKey), args.Error(1)
}

func (m *MockQuerier) UpdateTagKey(ctx context.Context, arg db.UpdateTagKeyParams) (db.TagKey, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.TagKey), args.Error(1)
}

func (m *MockQuerier) DeleteTagKey(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) CountTagValuesByTagKey(ctx context.Context, tagKeyID uuid.UUID) (int64, error) {
	args := m.Called(ctx, tagKeyID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CreateTagValue(ctx context.Context, arg db.CreateTagValueParams) (db.TagValue, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.TagValue), args.Error(1)
}

func (m *MockQuerier) GetTagValue(ctx context.Context, id uuid.UUID) (db.TagValue, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.TagValue), args.Error(1)
}

func (m *MockQuerier) GetTagValueByNamespacedName(ctx context.Context, namespacedName string) (db.TagValue, error) {
	args := m.Called(ctx, namespacedName)
	return args.Get(0).(db.TagValue), args.Error(1)
}

func (m *MockQuerier) UpdateTagValue(ctx context.Context, arg db.UpdateTagValueParams) (db.TagValue, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.TagValue), args.Error(1)
}

func (m *MockQuerier) DeleteTagValue(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) CountTagBindingsByTagValue(ctx context.Context, tagValueID uuid.UUID) (int64, error) {
	args := m.Called(ctx, tagValueID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CreateTagBinding(ctx context.Context, arg db.CreateTagBindingParams) (db.TagBinding, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.TagBinding), args.Error(1)
}

func (m *MockQuerier) GetTagBinding(ctx context.Context, id uuid.UUID) (db.TagBinding, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.TagBinding), args.Error(1)
}

func (m *MockQuerier) DeleteTagBinding(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) ListEffectiveTags(ctx context.Context, parentResource string) ([]db.ListEffectiveTagsRow, error) {
	args := m.Called(ctx, parentResource)
	return args.Get(0).([]db.ListEffectiveTagsRow), args.Error(1)
}

// --- Assets ---

func (m *MockQuerier) CreateAsset(ctx context.Context, arg db.CreateAssetParams) (db.Asset, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Asset), args.Error(1)
}

func (m *MockQuerier) GetAsset(ctx context.Context, id uuid.UUID) (db.Asset, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Asset), args.Error(1)
}

func (m *MockQuerier) GetAssetByChecksum(ctx context.Context, arg db.GetAssetByChecksumParams) (db.Asset, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Asset), args.Error(1)
}

func (m *MockQuerier) GetAssetByName(ctx context.Context, arg db.GetAssetByNameParams) (db.Asset, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Asset), args.Error(1)
}

func (m *MockQuerier) UpdateAsset(ctx context.Context, arg db.UpdateAssetParams) (db.Asset, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Asset), args.Error(1)
}

func (m *MockQuerier) UpdateAssetState(ctx context.Context, arg db.UpdateAssetStateParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) UpdateAssetIngestion(ctx context.Context, arg db.UpdateAssetIngestionParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) SoftDeleteAsset(ctx context.Context, arg db.SoftDeleteAssetParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) UndeleteAsset(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) ListAssetsByProject(ctx context.Context, arg db.ListAssetsByProjectParams) ([]db.Asset, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Asset), args.Error(1)
}

func (m *MockQuerier) ListAssetsByProjectWithDeleted(ctx context.Context, arg db.ListAssetsByProjectWithDeletedParams) ([]db.Asset, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Asset), args.Error(1)
}

func (m *MockQuerier) ListExpiredAssets(ctx context.Context, limit int32) ([]db.Asset, error) {
	args := m.Called(ctx, limit)
	return args.Get(0).([]db.Asset), args.Error(1)
}

func (m *MockQuerier) SearchAssets(ctx context.Context, arg db.SearchAssetsParams) ([]db.Asset, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Asset), args.Error(1)
}

func (m *MockQuerier) CountAssetsByProject(ctx context.Context, projectID uuid.UUID) (int64, error) {
	args := m.Called(ctx, projectID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CountAssetVersions(ctx context.Context, assetID uuid.UUID) (int64, error) {
	args := m.Called(ctx, assetID)
	return args.Get(0).(int64), args.Error(1)
}

// --- Asset Versions ---

func (m *MockQuerier) CreateAssetVersion(ctx context.Context, arg db.CreateAssetVersionParams) (db.AssetVersion, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.AssetVersion), args.Error(1)
}

func (m *MockQuerier) GetAssetVersion(ctx context.Context, id uuid.UUID) (db.AssetVersion, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.AssetVersion), args.Error(1)
}

func (m *MockQuerier) GetAssetVersionByNumber(ctx context.Context, arg db.GetAssetVersionByNumberParams) (db.AssetVersion, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.AssetVersion), args.Error(1)
}

func (m *MockQuerier) GetLatestAssetVersion(ctx context.Context, assetID uuid.UUID) (db.AssetVersion, error) {
	args := m.Called(ctx, assetID)
	return args.Get(0).(db.AssetVersion), args.Error(1)
}

func (m *MockQuerier) ListAssetVersions(ctx context.Context, arg db.ListAssetVersionsParams) ([]db.AssetVersion, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.AssetVersion), args.Error(1)
}

func (m *MockQuerier) NextVersionNumber(ctx context.Context, assetID uuid.UUID) (int32, error) {
	args := m.Called(ctx, assetID)
	return args.Get(0).(int32), args.Error(1)
}

func (m *MockQuerier) UpdateAssetVersionError(ctx context.Context, arg db.UpdateAssetVersionErrorParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

// --- Asset Renditions ---

func (m *MockQuerier) CreateAssetRendition(ctx context.Context, arg db.CreateAssetRenditionParams) (db.AssetRendition, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.AssetRendition), args.Error(1)
}

func (m *MockQuerier) ListAssetRenditions(ctx context.Context, versionID uuid.UUID) ([]db.AssetRendition, error) {
	args := m.Called(ctx, versionID)
	return args.Get(0).([]db.AssetRendition), args.Error(1)
}

func (m *MockQuerier) DeleteAssetRenditionsByVersion(ctx context.Context, versionID uuid.UUID) error {
	args := m.Called(ctx, versionID)
	return args.Error(0)
}

// --- Requests ---

func (m *MockQuerier) CreateRequest(ctx context.Context, arg db.CreateRequestParams) (db.Request, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Request), args.Error(1)
}

func (m *MockQuerier) GetRequest(ctx context.Context, id uuid.UUID) (db.Request, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Request), args.Error(1)
}

func (m *MockQuerier) GetRequestByName(ctx context.Context, arg db.GetRequestByNameParams) (db.Request, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Request), args.Error(1)
}

func (m *MockQuerier) UpdateRequest(ctx context.Context, arg db.UpdateRequestParams) (db.Request, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Request), args.Error(1)
}

func (m *MockQuerier) UpdateRequestApproved(ctx context.Context, arg db.UpdateRequestApprovedParams) (db.Request, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Request), args.Error(1)
}

func (m *MockQuerier) UpdateRequestAssignee(ctx context.Context, arg db.UpdateRequestAssigneeParams) (db.Request, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Request), args.Error(1)
}

func (m *MockQuerier) UpdateRequestDelivered(ctx context.Context, arg db.UpdateRequestDeliveredParams) (db.Request, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Request), args.Error(1)
}

func (m *MockQuerier) UpdateRequestState(ctx context.Context, arg db.UpdateRequestStateParams) (db.Request, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Request), args.Error(1)
}

func (m *MockQuerier) SoftDeleteRequest(ctx context.Context, arg db.SoftDeleteRequestParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) ListRequestsByProject(ctx context.Context, arg db.ListRequestsByProjectParams) ([]db.Request, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Request), args.Error(1)
}

func (m *MockQuerier) ListRequestsByProjectWithDeleted(ctx context.Context, arg db.ListRequestsByProjectWithDeletedParams) ([]db.Request, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Request), args.Error(1)
}

func (m *MockQuerier) CountRequestsByProject(ctx context.Context, projectID uuid.UUID) (int64, error) {
	args := m.Called(ctx, projectID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CountFulfilledLineItems(ctx context.Context, requestID uuid.UUID) (int64, error) {
	args := m.Called(ctx, requestID)
	return args.Get(0).(int64), args.Error(1)
}

// --- Line Items ---

func (m *MockQuerier) CreateLineItem(ctx context.Context, arg db.CreateLineItemParams) (db.LineItem, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.LineItem), args.Error(1)
}

func (m *MockQuerier) GetLineItem(ctx context.Context, id uuid.UUID) (db.LineItem, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.LineItem), args.Error(1)
}

func (m *MockQuerier) GetLineItemByName(ctx context.Context, arg db.GetLineItemByNameParams) (db.LineItem, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.LineItem), args.Error(1)
}

func (m *MockQuerier) UpdateLineItem(ctx context.Context, arg db.UpdateLineItemParams) (db.LineItem, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.LineItem), args.Error(1)
}

func (m *MockQuerier) UpdateLineItemState(ctx context.Context, arg db.UpdateLineItemStateParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) DeleteLineItem(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) ListLineItemsByRequest(ctx context.Context, arg db.ListLineItemsByRequestParams) ([]db.LineItem, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.LineItem), args.Error(1)
}

func (m *MockQuerier) CountLineItemsByRequest(ctx context.Context, requestID uuid.UUID) (int64, error) {
	args := m.Called(ctx, requestID)
	return args.Get(0).(int64), args.Error(1)
}

// --- Storage Gateways ---

func (m *MockQuerier) CreateStorageGateway(ctx context.Context, arg db.CreateStorageGatewayParams) (db.StorageGateway, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageGateway), args.Error(1)
}

func (m *MockQuerier) GetStorageGateway(ctx context.Context, id uuid.UUID) (db.StorageGateway, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.StorageGateway), args.Error(1)
}

func (m *MockQuerier) GetStorageGatewayByName(ctx context.Context, arg db.GetStorageGatewayByNameParams) (db.StorageGateway, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageGateway), args.Error(1)
}

func (m *MockQuerier) GetStorageGatewayByToken(ctx context.Context, registrationToken string) (db.StorageGateway, error) {
	args := m.Called(ctx, registrationToken)
	return args.Get(0).(db.StorageGateway), args.Error(1)
}

func (m *MockQuerier) UpdateStorageGateway(ctx context.Context, arg db.UpdateStorageGatewayParams) (db.StorageGateway, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageGateway), args.Error(1)
}

func (m *MockQuerier) UpdateStorageGatewayCert(ctx context.Context, arg db.UpdateStorageGatewayCertParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) UpdateStorageGatewayState(ctx context.Context, arg db.UpdateStorageGatewayStateParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) UpdateStorageGatewayVersion(ctx context.Context, arg db.UpdateStorageGatewayVersionParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) DeleteStorageGateway(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) RotateRegistrationToken(ctx context.Context, arg db.RotateRegistrationTokenParams) (db.StorageGateway, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageGateway), args.Error(1)
}

// --- Storage Agents ---

func (m *MockQuerier) CreateStorageAgent(ctx context.Context, arg db.CreateStorageAgentParams) (db.StorageAgent, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageAgent), args.Error(1)
}

func (m *MockQuerier) GetStorageAgent(ctx context.Context, id uuid.UUID) (db.StorageAgent, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.StorageAgent), args.Error(1)
}

func (m *MockQuerier) GetStorageAgentByGatewayAndIP(ctx context.Context, arg db.GetStorageAgentByGatewayAndIPParams) (db.StorageAgent, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageAgent), args.Error(1)
}

func (m *MockQuerier) UpdateStorageAgentState(ctx context.Context, arg db.UpdateStorageAgentStateParams) (db.StorageAgent, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageAgent), args.Error(1)
}

func (m *MockQuerier) UpdateStorageAgentHeartbeat(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) UpdateStorageAgentCacheUsed(ctx context.Context, arg db.UpdateStorageAgentCacheUsedParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) UpdateStorageAgentCert(ctx context.Context, arg db.UpdateStorageAgentCertParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) UpdateStorageAgentVersion(ctx context.Context, arg db.UpdateStorageAgentVersionParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) DeleteStorageAgent(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) ListStorageAgentsByGateway(ctx context.Context, gatewayID uuid.UUID) ([]db.StorageAgent, error) {
	args := m.Called(ctx, gatewayID)
	return args.Get(0).([]db.StorageAgent), args.Error(1)
}

func (m *MockQuerier) CountStorageAgentsByGateway(ctx context.Context, gatewayID uuid.UUID) (int64, error) {
	args := m.Called(ctx, gatewayID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CountConnectedStorageAgentsByGateway(ctx context.Context, gatewayID uuid.UUID) (int64, error) {
	args := m.Called(ctx, gatewayID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CreateStorageAgentAudit(ctx context.Context, arg db.CreateStorageAgentAuditParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) DeleteExpiredStorageAgentAudit(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) ListStorageAgentAuditByAgent(ctx context.Context, arg db.ListStorageAgentAuditByAgentParams) ([]db.StorageAgentAudit, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.StorageAgentAudit), args.Error(1)
}

func (m *MockQuerier) ListStorageAgentAuditByGateway(ctx context.Context, arg db.ListStorageAgentAuditByGatewayParams) ([]db.StorageAgentAudit, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.StorageAgentAudit), args.Error(1)
}

// --- Storage Endpoints ---

func (m *MockQuerier) CreateStorageEndpoint(ctx context.Context, arg db.CreateStorageEndpointParams) (db.StorageEndpoint, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageEndpoint), args.Error(1)
}

func (m *MockQuerier) GetStorageEndpoint(ctx context.Context, id uuid.UUID) (db.StorageEndpoint, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.StorageEndpoint), args.Error(1)
}

func (m *MockQuerier) GetStorageEndpointByName(ctx context.Context, arg db.GetStorageEndpointByNameParams) (db.StorageEndpoint, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageEndpoint), args.Error(1)
}

func (m *MockQuerier) UpdateStorageEndpoint(ctx context.Context, arg db.UpdateStorageEndpointParams) (db.StorageEndpoint, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.StorageEndpoint), args.Error(1)
}

func (m *MockQuerier) UpdateStorageEndpointState(ctx context.Context, arg db.UpdateStorageEndpointStateParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) DeleteStorageEndpoint(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) ListStorageEndpointsByGateway(ctx context.Context, gatewayID uuid.UUID) ([]db.StorageEndpoint, error) {
	args := m.Called(ctx, gatewayID)
	return args.Get(0).([]db.StorageEndpoint), args.Error(1)
}

// --- IAM ---

func (m *MockQuerier) GetIamPolicy(ctx context.Context, resourceID uuid.UUID) (db.IamPolicy, error) {
	args := m.Called(ctx, resourceID)
	return args.Get(0).(db.IamPolicy), args.Error(1)
}

func (m *MockQuerier) UpsertIamPolicy(ctx context.Context, arg db.UpsertIamPolicyParams) (db.IamPolicy, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.IamPolicy), args.Error(1)
}

func (m *MockQuerier) DeleteIamPolicy(ctx context.Context, resourceID uuid.UUID) error {
	args := m.Called(ctx, resourceID)
	return args.Error(0)
}

// --- Operation Metadata ---

func (m *MockQuerier) UpdateOperationMetadata(ctx context.Context, arg db.UpdateOperationMetadataParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

// --- AI Chat ---

// Conversations

func (m *MockQuerier) CountConversationsByCreator(ctx context.Context, arg db.CountConversationsByCreatorParams) (int64, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CreateConversation(ctx context.Context, arg db.CreateConversationParams) (db.Conversation, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Conversation), args.Error(1)
}

func (m *MockQuerier) DeleteConversation(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) GetConversationByID(ctx context.Context, id uuid.UUID) (db.Conversation, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Conversation), args.Error(1)
}

func (m *MockQuerier) GetConversationByName(ctx context.Context, arg db.GetConversationByNameParams) (db.Conversation, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Conversation), args.Error(1)
}

func (m *MockQuerier) IncrementConversationMessageCount(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) ListConversationsByCreator(ctx context.Context, arg db.ListConversationsByCreatorParams) ([]db.Conversation, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Conversation), args.Error(1)
}

func (m *MockQuerier) ListConversationsByCreatorActive(ctx context.Context, arg db.ListConversationsByCreatorActiveParams) ([]db.Conversation, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Conversation), args.Error(1)
}

func (m *MockQuerier) UpdateConversation(ctx context.Context, arg db.UpdateConversationParams) (db.Conversation, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Conversation), args.Error(1)
}

// Messages

func (m *MockQuerier) CountMessagesByConversation(ctx context.Context, conversationID uuid.UUID) (int64, error) {
	args := m.Called(ctx, conversationID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CreateMessage(ctx context.Context, arg db.CreateMessageParams) (db.Message, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Message), args.Error(1)
}

func (m *MockQuerier) GetMessageByName(ctx context.Context, arg db.GetMessageByNameParams) (db.Message, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Message), args.Error(1)
}

func (m *MockQuerier) GetNextSequenceForConversation(ctx context.Context, conversationID uuid.UUID) (int32, error) {
	args := m.Called(ctx, conversationID)
	return args.Get(0).(int32), args.Error(1)
}

func (m *MockQuerier) ListMessagesByConversation(ctx context.Context, arg db.ListMessagesByConversationParams) ([]db.Message, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Message), args.Error(1)
}

func (m *MockQuerier) ListMessagesNewestFirst(ctx context.Context, arg db.ListMessagesNewestFirstParams) ([]db.Message, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Message), args.Error(1)
}

func (m *MockQuerier) SumTokensByConversation(ctx context.Context, conversationID uuid.UUID) (int64, error) {
	args := m.Called(ctx, conversationID)
	return args.Get(0).(int64), args.Error(1)
}

// Artifacts

func (m *MockQuerier) CountArtifactsByConversation(ctx context.Context, conversationID uuid.UUID) (int64, error) {
	args := m.Called(ctx, conversationID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CreateArtifact(ctx context.Context, arg db.CreateArtifactParams) (db.Artifact, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Artifact), args.Error(1)
}

func (m *MockQuerier) DeleteArtifact(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) GetArtifactByID(ctx context.Context, id uuid.UUID) (db.Artifact, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Artifact), args.Error(1)
}

func (m *MockQuerier) GetArtifactByName(ctx context.Context, arg db.GetArtifactByNameParams) (db.Artifact, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Artifact), args.Error(1)
}

func (m *MockQuerier) ListArtifactsByConversation(ctx context.Context, arg db.ListArtifactsByConversationParams) ([]db.Artifact, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Artifact), args.Error(1)
}

func (m *MockQuerier) UpdateArtifactLatestVersion(ctx context.Context, arg db.UpdateArtifactLatestVersionParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

// Artifact Versions

func (m *MockQuerier) CountArtifactVersionsByArtifact(ctx context.Context, artifactID uuid.UUID) (int64, error) {
	args := m.Called(ctx, artifactID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CreateAssetArtifactVersion(ctx context.Context, arg db.CreateAssetArtifactVersionParams) (db.ArtifactVersion, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ArtifactVersion), args.Error(1)
}

func (m *MockQuerier) CreateInlineArtifactVersion(ctx context.Context, arg db.CreateInlineArtifactVersionParams) (db.ArtifactVersion, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ArtifactVersion), args.Error(1)
}

func (m *MockQuerier) DeleteArtifactVersion(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) GetArtifactVersionByName(ctx context.Context, arg db.GetArtifactVersionByNameParams) (db.ArtifactVersion, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ArtifactVersion), args.Error(1)
}

func (m *MockQuerier) GetArtifactVersionForContent(ctx context.Context, arg db.GetArtifactVersionForContentParams) (db.GetArtifactVersionForContentRow, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.GetArtifactVersionForContentRow), args.Error(1)
}

func (m *MockQuerier) IsOnlyArtifactVersion(ctx context.Context, artifactID uuid.UUID) (bool, error) {
	args := m.Called(ctx, artifactID)
	return args.Bool(0), args.Error(1)
}

func (m *MockQuerier) ListArtifactVersionsByArtifact(ctx context.Context, arg db.ListArtifactVersionsByArtifactParams) ([]db.ListArtifactVersionsByArtifactRow, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.ListArtifactVersionsByArtifactRow), args.Error(1)
}
