package assets

import (
	"context"
	"encoding/json"
	"fmt"
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
	testOrg       = "acme"
	testSpace     = "proj1"
	testAssetName = "asset-abc123"
	testParent    = "organizations/acme/spaces/proj1"
	testAssetFull = "organizations/acme/spaces/proj1/assets/asset-abc123"
)

type assetFixture struct {
	orgID   uuid.UUID
	spaceID uuid.UUID
	assetID uuid.UUID
	mockQ   *mocks.MockQuerier
	server  *AssetsServer
}

func setupAssetFixture(t *testing.T) assetFixture {
	t.Helper()
	f := assetFixture{
		orgID:   uuid.New(),
		spaceID: uuid.New(),
		assetID: uuid.New(),
		mockQ:   new(mocks.MockQuerier),
	}
	f.server = &AssetsServer{queries: f.mockQ}
	return f
}

func (f *assetFixture) mockResolveSpace() {
	f.mockQ.On("GetOrganizationByName", mock.Anything, testOrg).
		Return(db.Organization{ID: f.orgID, Name: testOrg}, nil)
	f.mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{OrgID: f.orgID, Name: testSpace}).
		Return(db.Space{ID: f.spaceID, Name: testSpace, OrgID: f.orgID}, nil)
}

func makeAsset(id, spaceID uuid.UUID, name string, state db.AssetState) db.Asset {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return db.Asset{
		ID:          id,
		SpaceID:     spaceID,
		Name:        name,
		DisplayName: "Test Asset",
		State:       state,
		Annotations: json.RawMessage("{}"),
		CreateTime:  now,
		UpdateTime:  now,
	}
}

// --- CreateAsset ---

func TestCreateAsset_Placeholder(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	created := makeAsset(f.assetID, f.spaceID, "placeholder", db.AssetStatePLACEHOLDER)

	f.mockQ.On("CreateAsset", mock.Anything, mock.MatchedBy(func(p db.CreateAssetParams) bool {
		return p.SpaceID == f.spaceID && p.State == db.AssetStatePLACEHOLDER && p.DisplayName == "Logo Placeholder"
	})).Return(created, nil)

	op, err := f.server.CreateAsset(context.Background(), &assetsv1.CreateAssetRequest{
		Parent: testParent,
		Asset: &assetsv1.Asset{
			DisplayName: "Logo Placeholder",
		},
		// No endpoint, no filename -> placeholder.
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

func TestCreateAsset_WithFile(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	created := makeAsset(f.assetID, f.spaceID, "file-asset", db.AssetStatePROCESSING)

	f.mockQ.On("CreateAsset", mock.Anything, mock.MatchedBy(func(p db.CreateAssetParams) bool {
		return p.SpaceID == f.spaceID && p.State == db.AssetStatePROCESSING && p.Filename == "logo.png"
	})).Return(created, nil)

	f.mockQ.On("UpdateAssetState", mock.Anything, db.UpdateAssetStateParams{
		ID:    f.assetID,
		State: db.AssetStateACTIVE,
	}).Return(nil)

	op, err := f.server.CreateAsset(context.Background(), &assetsv1.CreateAssetRequest{
		Parent: testParent,
		Asset: &assetsv1.Asset{
			DisplayName: "Logo",
		},
		Filename: "logo.png",
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

func TestCreateAsset_InvalidParent(t *testing.T) {
	f := setupAssetFixture(t)

	_, err := f.server.CreateAsset(context.Background(), &assetsv1.CreateAssetRequest{
		Parent: "bad/parent/format",
		Asset:  &assetsv1.Asset{DisplayName: "X"},
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	// HandleResourceError falls through to Internal for a parse error
	// (not pgx.ErrNoRows, not duplicate key).
	assert.Equal(t, codes.Internal, st.Code())
}

// --- GetAsset ---

func TestGetAsset_Success(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	existing := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateACTIVE)
	f.mockQ.On("GetAssetByName", mock.Anything, db.GetAssetByNameParams{SpaceID: f.spaceID, Name: testAssetName}).
		Return(existing, nil)

	f.mockQ.On("CountAssetVersions", mock.Anything, f.assetID).Return(int64(3), nil)

	versionID := uuid.New()
	latestVersion := db.AssetVersion{
		ID:            versionID,
		AssetID:       f.assetID,
		VersionNumber: 3,
		MimeType:      "image/png",
		StorageKey:    "s3://bucket/key",
		CreateTime:    time.Now(),
	}
	f.mockQ.On("GetLatestAssetVersion", mock.Anything, f.assetID).Return(latestVersion, nil)

	renditions := []db.AssetRendition{
		{
			ID:         uuid.New(),
			VersionID:  versionID,
			Type:       db.RenditionTypeTHUMBNAILSMALL,
			StorageKey: "s3://bucket/thumb-small",
			MimeType:   "image/webp",
			SizeBytes:  1024,
		},
	}
	f.mockQ.On("ListAssetRenditions", mock.Anything, versionID).Return(renditions, nil)

	resp, err := f.server.GetAsset(context.Background(), &assetsv1.GetAssetRequest{
		Name: testAssetFull,
	})

	require.NoError(t, err)
	assert.Equal(t, testAssetFull, resp.GetName())
	assert.Equal(t, assetsv1.Asset_ACTIVE, resp.GetState())
	assert.Equal(t, int32(3), resp.GetVersionCount())
	assert.NotNil(t, resp.GetLatestVersion())
	assert.Len(t, resp.GetLatestVersion().GetRenditions(), 1)
	f.mockQ.AssertExpectations(t)
}

// --- UpdateAsset ---

func TestUpdateAsset_WithFieldMask(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	existing := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateACTIVE)
	f.mockQ.On("GetAssetByName", mock.Anything, db.GetAssetByNameParams{SpaceID: f.spaceID, Name: testAssetName}).
		Return(existing, nil)

	updated := existing
	updated.DisplayName = "Updated Name"
	f.mockQ.On("UpdateAsset", mock.Anything, mock.MatchedBy(func(p db.UpdateAssetParams) bool {
		return p.ID == f.assetID && p.DisplayName.Valid && p.DisplayName.String == "Updated Name"
	})).Return(updated, nil)

	op, err := f.server.UpdateAsset(context.Background(), &assetsv1.UpdateAssetRequest{
		Asset: &assetsv1.Asset{
			Name:        testAssetFull,
			DisplayName: "Updated Name",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name"}},
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

func TestUpdateAsset_NoMask(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	existing := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateACTIVE)
	f.mockQ.On("GetAssetByName", mock.Anything, db.GetAssetByNameParams{SpaceID: f.spaceID, Name: testAssetName}).
		Return(existing, nil)

	updated := existing
	updated.DisplayName = "Full Update Name"
	f.mockQ.On("UpdateAsset", mock.Anything, mock.MatchedBy(func(p db.UpdateAssetParams) bool {
		return p.ID == f.assetID &&
			p.DisplayName.Valid && p.DisplayName.String == "Full Update Name" &&
			p.Annotations == nil && // no annotations in no-mask path (not set unless in mask)
			!p.ExpireTime.Valid // expire_time not set without mask
	})).Return(updated, nil)

	op, err := f.server.UpdateAsset(context.Background(), &assetsv1.UpdateAssetRequest{
		Asset: &assetsv1.Asset{
			Name:        testAssetFull,
			DisplayName: "Full Update Name",
		},
		// No UpdateMask
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

func TestUpdateAsset_ExpireTime(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	existing := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateACTIVE)
	f.mockQ.On("GetAssetByName", mock.Anything, db.GetAssetByNameParams{SpaceID: f.spaceID, Name: testAssetName}).
		Return(existing, nil)

	updated := existing
	f.mockQ.On("UpdateAsset", mock.Anything, mock.MatchedBy(func(p db.UpdateAssetParams) bool {
		return p.ID == f.assetID && p.ExpireTime.Valid
	})).Return(updated, nil)

	expireTime := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	op, err := f.server.UpdateAsset(context.Background(), &assetsv1.UpdateAssetRequest{
		Asset: &assetsv1.Asset{
			Name:       testAssetFull,
			ExpireTime: timestamppb.New(expireTime),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"expire_time"}},
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

func TestUpdateAsset_Annotations(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	existing := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateACTIVE)
	f.mockQ.On("GetAssetByName", mock.Anything, db.GetAssetByNameParams{SpaceID: f.spaceID, Name: testAssetName}).
		Return(existing, nil)

	updated := existing
	f.mockQ.On("UpdateAsset", mock.Anything, mock.MatchedBy(func(p db.UpdateAssetParams) bool {
		return p.ID == f.assetID && p.Annotations != nil && !p.DisplayName.Valid
	})).Return(updated, nil)

	op, err := f.server.UpdateAsset(context.Background(), &assetsv1.UpdateAssetRequest{
		Asset: &assetsv1.Asset{
			Name:        testAssetFull,
			Annotations: map[string]string{"team": "eng"},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"annotations"}},
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

// --- DeleteAsset ---

func TestDeleteAsset_Success(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	existing := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateACTIVE)
	f.mockQ.On("GetAssetByName", mock.Anything, db.GetAssetByNameParams{SpaceID: f.spaceID, Name: testAssetName}).
		Return(existing, nil)

	f.mockQ.On("SoftDeleteAsset", mock.Anything, db.SoftDeleteAssetParams{
		ID:        f.assetID,
		DeletedBy: pgtype.UUID{},
	}).Return(nil)

	op, err := f.server.DeleteAsset(context.Background(), &assetsv1.DeleteAssetRequest{
		Name: testAssetFull,
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

// --- UndeleteAsset ---

func TestUndeleteAsset_Success(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	deletedAt := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	existing := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateDELETEREQUESTED)
	existing.DeleteTime = pgtype.Timestamptz{Time: deletedAt, Valid: true}

	f.mockQ.On("GetAssetByName", mock.Anything, db.GetAssetByNameParams{SpaceID: f.spaceID, Name: testAssetName}).
		Return(existing, nil)

	f.mockQ.On("UndeleteAsset", mock.Anything, f.assetID).Return(nil)

	restored := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateACTIVE)
	f.mockQ.On("GetAsset", mock.Anything, f.assetID).Return(restored, nil)

	op, err := f.server.UndeleteAsset(context.Background(), &assetsv1.UndeleteAssetRequest{
		Name: testAssetFull,
	})

	require.NoError(t, err)
	assert.True(t, op.GetDone())
	f.mockQ.AssertExpectations(t)
}

// --- GetAsset error paths ---

func TestGetAsset_InvalidName(t *testing.T) {
	f := setupAssetFixture(t)

	_, err := f.server.GetAsset(context.Background(), &assetsv1.GetAssetRequest{
		Name: "bad/format",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestGetAsset_NotFound(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	f.mockQ.On("GetAssetByName", mock.Anything, mock.Anything).
		Return(db.Asset{}, pgx.ErrNoRows)

	_, err := f.server.GetAsset(context.Background(), &assetsv1.GetAssetRequest{
		Name: testAssetFull,
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- ListAssets error paths ---

func TestListAssets_InvalidParent(t *testing.T) {
	f := setupAssetFixture(t)

	_, err := f.server.ListAssets(context.Background(), &assetsv1.ListAssetsRequest{
		Parent: "bad/format",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestListAssets_DBError(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	f.mockQ.On("ListAssetsBySpace", mock.Anything, mock.Anything).
		Return([]db.Asset(nil), fmt.Errorf("db down"))

	_, err := f.server.ListAssets(context.Background(), &assetsv1.ListAssetsRequest{
		Parent: testParent,
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- CreateAsset error paths ---

func TestCreateAsset_DBError(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	f.mockQ.On("CreateAsset", mock.Anything, mock.Anything).
		Return(db.Asset{}, fmt.Errorf("db error"))

	_, err := f.server.CreateAsset(context.Background(), &assetsv1.CreateAssetRequest{
		Parent: testParent,
		Asset:  &assetsv1.Asset{DisplayName: "Test"},
	})
	require.Error(t, err)
	f.mockQ.AssertExpectations(t)
}

// --- UpdateAsset error paths ---

func TestUpdateAsset_InvalidName(t *testing.T) {
	f := setupAssetFixture(t)

	_, err := f.server.UpdateAsset(context.Background(), &assetsv1.UpdateAssetRequest{
		Asset: &assetsv1.Asset{Name: "bad"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestUpdateAsset_NotFound(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	f.mockQ.On("GetAssetByName", mock.Anything, mock.Anything).
		Return(db.Asset{}, pgx.ErrNoRows)

	_, err := f.server.UpdateAsset(context.Background(), &assetsv1.UpdateAssetRequest{
		Asset: &assetsv1.Asset{Name: testAssetFull, DisplayName: "X"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

// --- DeleteAsset error paths ---

func TestDeleteAsset_InvalidName(t *testing.T) {
	f := setupAssetFixture(t)

	_, err := f.server.DeleteAsset(context.Background(), &assetsv1.DeleteAssetRequest{
		Name: "bad",
	})
	require.Error(t, err)
}

func TestDeleteAsset_NotFound(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	f.mockQ.On("GetAssetByName", mock.Anything, mock.Anything).
		Return(db.Asset{}, pgx.ErrNoRows)

	_, err := f.server.DeleteAsset(context.Background(), &assetsv1.DeleteAssetRequest{
		Name: testAssetFull,
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestDeleteAsset_SoftDeleteError(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	existing := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateACTIVE)
	f.mockQ.On("GetAssetByName", mock.Anything, mock.Anything).Return(existing, nil)
	f.mockQ.On("SoftDeleteAsset", mock.Anything, mock.Anything).
		Return(fmt.Errorf("constraint error"))

	_, err := f.server.DeleteAsset(context.Background(), &assetsv1.DeleteAssetRequest{
		Name: testAssetFull,
	})
	require.Error(t, err)
	f.mockQ.AssertExpectations(t)
}

// --- UndeleteAsset error paths ---

func TestUndeleteAsset_InvalidName(t *testing.T) {
	f := setupAssetFixture(t)

	_, err := f.server.UndeleteAsset(context.Background(), &assetsv1.UndeleteAssetRequest{
		Name: "bad",
	})
	require.Error(t, err)
}

func TestUndeleteAsset_NotFound(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	f.mockQ.On("GetAssetByName", mock.Anything, mock.Anything).
		Return(db.Asset{}, pgx.ErrNoRows)

	_, err := f.server.UndeleteAsset(context.Background(), &assetsv1.UndeleteAssetRequest{
		Name: testAssetFull,
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	f.mockQ.AssertExpectations(t)
}

func TestUndeleteAsset_NotDeleted(t *testing.T) {
	f := setupAssetFixture(t)
	f.mockResolveSpace()

	existing := makeAsset(f.assetID, f.spaceID, testAssetName, db.AssetStateACTIVE)
	// DeleteTime.Valid is false by default (not deleted).
	f.mockQ.On("GetAssetByName", mock.Anything, db.GetAssetByNameParams{SpaceID: f.spaceID, Name: testAssetName}).
		Return(existing, nil)

	_, err := f.server.UndeleteAsset(context.Background(), &assetsv1.UndeleteAssetRequest{
		Name: testAssetFull,
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}
