package convert

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
)

// ---------------------------------------------------------------------------
// Assets
// ---------------------------------------------------------------------------

func TestAssetToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)
	annotations := map[string]string{"env": "prod"}
	annotationsJSON, err := json.Marshal(annotations)
	require.NoError(t, err)
	deleteTime := now.Add(24 * time.Hour)
	purgeTime := now.Add(48 * time.Hour)
	expireTime := now.Add(72 * time.Hour)

	tests := []struct {
		name        string
		row         db.Asset
		projectName string
		wantName    string
		wantState   assetsv1.Asset_State
		checkFunc   func(t *testing.T, pb *assetsv1.Asset)
	}{
		{
			name: "active asset with all optional fields",
			row: db.Asset{
				ID:              uuid.New(),
				Name:            "abc123",
				DisplayName:     "My Asset",
				State:           db.AssetStateACTIVE,
				ContentType:     "image/png",
				Filename:        "photo.png",
				ImportPath:      "/uploads/photo.png",
				ChecksumSha256:  "sha256-hash",
				SizeBytes:       1024,
				Etag:            "etag-1",
				CreatedBy:       "user@test.com",
				UpdatedBy:       "user@test.com",
				CreateTime:      now,
				UpdateTime:      updated,
				MediaType:       db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				Width:           pgtype.Int4{Int32: 1920, Valid: true},
				Height:          pgtype.Int4{Int32: 1080, Valid: true},
				DurationSeconds: pgtype.Float8{Float64: 120.5, Valid: true},
				Annotations:     annotationsJSON,
				DeleteTime:      pgtype.Timestamptz{Time: deleteTime, Valid: true},
				PurgeTime:       pgtype.Timestamptz{Time: purgeTime, Valid: true},
				ExpireTime:      pgtype.Timestamptz{Time: expireTime, Valid: true},
			},
			projectName: "organizations/acme/projects/my-project",
			wantName:    "organizations/acme/projects/my-project/assets/abc123",
			wantState:   assetsv1.Asset_ACTIVE,
			checkFunc: func(t *testing.T, pb *assetsv1.Asset) {
				assert.Equal(t, assetsv1.Asset_IMAGE, pb.MediaType)
				assert.Equal(t, int32(1920), pb.Width)
				assert.Equal(t, int32(1080), pb.Height)
				assert.NotNil(t, pb.Duration)
				assert.Equal(t, int64(120), pb.Duration.Seconds)
				assert.Equal(t, map[string]string{"env": "prod"}, pb.Annotations)
				assert.NotNil(t, pb.DeleteTime)
				assert.NotNil(t, pb.PurgeTime)
				assert.NotNil(t, pb.ExpireTime)
			},
		},
		{
			name: "placeholder asset with no optional fields",
			row: db.Asset{
				ID:              uuid.New(),
				Name:            "def456",
				DisplayName:     "Placeholder",
				State:           db.AssetStatePLACEHOLDER,
				ContentType:     "",
				Etag:            "etag-2",
				CreateTime:      now,
				UpdateTime:      updated,
				MediaType:       db.NullAssetMediaType{Valid: false},
				Width:           pgtype.Int4{Valid: false},
				Height:          pgtype.Int4{Valid: false},
				DurationSeconds: pgtype.Float8{Valid: false},
				Annotations:     nil,
				DeleteTime:      pgtype.Timestamptz{Valid: false},
				PurgeTime:       pgtype.Timestamptz{Valid: false},
				ExpireTime:      pgtype.Timestamptz{Valid: false},
			},
			projectName: "organizations/acme/projects/my-project",
			wantName:    "organizations/acme/projects/my-project/assets/def456",
			wantState:   assetsv1.Asset_PLACEHOLDER,
			checkFunc: func(t *testing.T, pb *assetsv1.Asset) {
				assert.Equal(t, assetsv1.Asset_MEDIA_TYPE_UNSPECIFIED, pb.MediaType)
				assert.Equal(t, int32(0), pb.Width)
				assert.Equal(t, int32(0), pb.Height)
				assert.Nil(t, pb.Duration)
				assert.Nil(t, pb.Annotations)
				assert.Nil(t, pb.DeleteTime)
				assert.Nil(t, pb.PurgeTime)
				assert.Nil(t, pb.ExpireTime)
			},
		},
		{
			name: "processing state",
			row: db.Asset{
				ID:         uuid.New(),
				Name:       "proc1",
				State:      db.AssetStatePROCESSING,
				CreateTime: now,
				UpdateTime: updated,
			},
			projectName: "organizations/acme/projects/p1",
			wantName:    "organizations/acme/projects/p1/assets/proc1",
			wantState:   assetsv1.Asset_PROCESSING,
		},
		{
			name: "failed state",
			row: db.Asset{
				ID:         uuid.New(),
				Name:       "fail1",
				State:      db.AssetStateFAILED,
				CreateTime: now,
				UpdateTime: updated,
			},
			projectName: "organizations/acme/projects/p1",
			wantName:    "organizations/acme/projects/p1/assets/fail1",
			wantState:   assetsv1.Asset_FAILED,
		},
		{
			name: "delete_requested state",
			row: db.Asset{
				ID:         uuid.New(),
				Name:       "del1",
				State:      db.AssetStateDELETEREQUESTED,
				CreateTime: now,
				UpdateTime: updated,
			},
			projectName: "organizations/acme/projects/p1",
			wantName:    "organizations/acme/projects/p1/assets/del1",
			wantState:   assetsv1.Asset_DELETE_REQUESTED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := AssetToProto(tt.row, tt.projectName)
			require.NotNil(t, pb)
			assert.Equal(t, tt.wantName, pb.Name)
			assert.Equal(t, tt.wantState, pb.State)
			assert.Equal(t, tt.row.DisplayName, pb.DisplayName)
			assert.Equal(t, tt.row.ContentType, pb.ContentType)
			assert.Equal(t, tt.row.Filename, pb.Filename)
			assert.Equal(t, tt.row.ImportPath, pb.ImportPath)
			assert.Equal(t, tt.row.ChecksumSha256, pb.ChecksumSha256)
			assert.Equal(t, tt.row.SizeBytes, pb.SizeBytes)
			assert.Equal(t, tt.row.Etag, pb.Etag)
			assert.Equal(t, tt.row.CreatedBy, pb.Creator)
			assert.Equal(t, tt.row.UpdatedBy, pb.Updater)
			if tt.checkFunc != nil {
				tt.checkFunc(t, pb)
			}
		})
	}
}

func TestAssetVersionToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	versionID := uuid.New()

	tests := []struct {
		name      string
		row       db.AssetVersion
		assetName string
	}{
		{
			name: "full version",
			row: db.AssetVersion{
				ID:             versionID,
				AssetID:        uuid.New(),
				VersionNumber:  3,
				ChecksumSha256: "sha-abc",
				SizeBytes:      2048,
				MimeType:       "video/mp4",
				StorageKey:     "s3://bucket/key",
				ChangeNote:     "Updated quality",
				IngestionError: "",
				CreatedBy:      "editor@test.com",
				CreateTime:     now,
			},
			assetName: "organizations/acme/projects/p1/assets/abc",
		},
		{
			name: "version with ingestion error",
			row: db.AssetVersion{
				ID:             uuid.New(),
				AssetID:        uuid.New(),
				VersionNumber:  1,
				MimeType:       "image/jpeg",
				StorageKey:     "s3://bucket/fail",
				IngestionError: "transcoding failed",
				CreatedBy:      "user@test.com",
				CreateTime:     now,
			},
			assetName: "organizations/acme/projects/p1/assets/def",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := AssetVersionToProto(tt.row, tt.assetName)
			require.NotNil(t, pb)
			assert.Contains(t, pb.Name, tt.assetName+"/versions/")
			assert.Equal(t, tt.row.VersionNumber, pb.VersionNumber)
			assert.Equal(t, tt.row.ChecksumSha256, pb.ChecksumSha256)
			assert.Equal(t, tt.row.SizeBytes, pb.SizeBytes)
			assert.Equal(t, tt.row.MimeType, pb.MimeType)
			assert.Equal(t, tt.row.StorageKey, pb.StorageKey)
			assert.Equal(t, tt.row.ChangeNote, pb.ChangeNote)
			assert.Equal(t, tt.row.IngestionError, pb.IngestionError)
			assert.Equal(t, tt.row.CreatedBy, pb.Creator)
		})
	}
}

func TestRenditionToProto(t *testing.T) {
	tests := []struct {
		name     string
		row      db.AssetRendition
		wantType assetsv1.Rendition_Type
	}{
		{
			name: "thumbnail small with dimensions",
			row: db.AssetRendition{
				ID:         uuid.New(),
				Type:       db.RenditionTypeTHUMBNAILSMALL,
				StorageKey: "s3://bucket/thumb-s",
				MimeType:   "image/jpeg",
				SizeBytes:  1024,
				Width:      pgtype.Int4{Int32: 100, Valid: true},
				Height:     pgtype.Int4{Int32: 100, Valid: true},
			},
			wantType: assetsv1.Rendition_THUMBNAIL_SMALL,
		},
		{
			name: "thumbnail medium",
			row: db.AssetRendition{
				ID:         uuid.New(),
				Type:       db.RenditionTypeTHUMBNAILMEDIUM,
				StorageKey: "s3://bucket/thumb-m",
				MimeType:   "image/jpeg",
				SizeBytes:  2048,
				Width:      pgtype.Int4{Valid: false},
				Height:     pgtype.Int4{Valid: false},
			},
			wantType: assetsv1.Rendition_THUMBNAIL_MEDIUM,
		},
		{
			name: "thumbnail large",
			row: db.AssetRendition{
				ID:         uuid.New(),
				Type:       db.RenditionTypeTHUMBNAILLARGE,
				StorageKey: "s3://bucket/thumb-l",
				MimeType:   "image/png",
				SizeBytes:  4096,
			},
			wantType: assetsv1.Rendition_THUMBNAIL_LARGE,
		},
		{
			name: "animated preview",
			row: db.AssetRendition{
				ID:         uuid.New(),
				Type:       db.RenditionTypeANIMATEDPREVIEW,
				StorageKey: "s3://bucket/anim",
				MimeType:   "image/gif",
				SizeBytes:  8192,
			},
			wantType: assetsv1.Rendition_ANIMATED_PREVIEW,
		},
		{
			name: "video proxy",
			row: db.AssetRendition{
				ID:         uuid.New(),
				Type:       db.RenditionTypeVIDEOPROXY,
				StorageKey: "s3://bucket/proxy.mp4",
				MimeType:   "video/mp4",
				SizeBytes:  16384,
			},
			wantType: assetsv1.Rendition_VIDEO_PROXY,
		},
		{
			name: "audio preview",
			row: db.AssetRendition{
				ID:         uuid.New(),
				Type:       db.RenditionTypeAUDIOPREVIEW,
				StorageKey: "s3://bucket/audio.mp3",
				MimeType:   "audio/mpeg",
				SizeBytes:  32768,
			},
			wantType: assetsv1.Rendition_AUDIO_PREVIEW,
		},
		{
			name: "poster frame",
			row: db.AssetRendition{
				ID:         uuid.New(),
				Type:       db.RenditionTypePOSTERFRAME,
				StorageKey: "s3://bucket/poster.jpg",
				MimeType:   "image/jpeg",
				SizeBytes:  65536,
			},
			wantType: assetsv1.Rendition_POSTER_FRAME,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := RenditionToProto(tt.row)
			require.NotNil(t, pb)
			assert.Equal(t, tt.wantType, pb.Type)
			assert.Equal(t, tt.row.StorageKey, pb.StorageKey)
			assert.Equal(t, tt.row.MimeType, pb.MimeType)
			assert.Equal(t, tt.row.SizeBytes, pb.SizeBytes)
			if tt.row.Width.Valid {
				assert.Equal(t, tt.row.Width.Int32, pb.Width)
			}
			if tt.row.Height.Valid {
				assert.Equal(t, tt.row.Height.Int32, pb.Height)
			}
		})
	}
}

func TestRenditionsToProto(t *testing.T) {
	tests := []struct {
		name string
		rows []db.AssetRendition
		want int
	}{
		{
			name: "multiple renditions",
			rows: []db.AssetRendition{
				{ID: uuid.New(), Type: db.RenditionTypeTHUMBNAILSMALL, StorageKey: "k1", MimeType: "image/jpeg"},
				{ID: uuid.New(), Type: db.RenditionTypeVIDEOPROXY, StorageKey: "k2", MimeType: "video/mp4"},
			},
			want: 2,
		},
		{
			name: "empty renditions",
			rows: []db.AssetRendition{},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenditionsToProto(tt.rows)
			assert.Len(t, result, tt.want)
		})
	}
}

func TestAssetState(t *testing.T) {
	tests := []struct {
		name string
		db   db.AssetState
		want assetsv1.Asset_State
	}{
		{"PLACEHOLDER", db.AssetStatePLACEHOLDER, assetsv1.Asset_PLACEHOLDER},
		{"PROCESSING", db.AssetStatePROCESSING, assetsv1.Asset_PROCESSING},
		{"ACTIVE", db.AssetStateACTIVE, assetsv1.Asset_ACTIVE},
		{"FAILED", db.AssetStateFAILED, assetsv1.Asset_FAILED},
		{"DELETE_REQUESTED", db.AssetStateDELETEREQUESTED, assetsv1.Asset_DELETE_REQUESTED},
		{"unknown defaults to STATE_UNSPECIFIED", db.AssetState("BOGUS"), assetsv1.Asset_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := assetState(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAssetMediaType(t *testing.T) {
	tests := []struct {
		name string
		db   db.AssetMediaType
		want assetsv1.Asset_MediaType
	}{
		{"IMAGE", db.AssetMediaTypeIMAGE, assetsv1.Asset_IMAGE},
		{"VIDEO", db.AssetMediaTypeVIDEO, assetsv1.Asset_VIDEO},
		{"AUDIO", db.AssetMediaTypeAUDIO, assetsv1.Asset_AUDIO},
		{"DOCUMENT", db.AssetMediaTypeDOCUMENT, assetsv1.Asset_DOCUMENT},
		{"GRAPHIC defaults to UNSPECIFIED", db.AssetMediaTypeGRAPHIC, assetsv1.Asset_MEDIA_TYPE_UNSPECIFIED},
		{"unknown defaults to UNSPECIFIED", db.AssetMediaType("UNKNOWN"), assetsv1.Asset_MEDIA_TYPE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := assetMediaType(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRenditionType(t *testing.T) {
	tests := []struct {
		name string
		db   db.RenditionType
		want assetsv1.Rendition_Type
	}{
		{"THUMBNAIL_SMALL", db.RenditionTypeTHUMBNAILSMALL, assetsv1.Rendition_THUMBNAIL_SMALL},
		{"THUMBNAIL_MEDIUM", db.RenditionTypeTHUMBNAILMEDIUM, assetsv1.Rendition_THUMBNAIL_MEDIUM},
		{"THUMBNAIL_LARGE", db.RenditionTypeTHUMBNAILLARGE, assetsv1.Rendition_THUMBNAIL_LARGE},
		{"ANIMATED_PREVIEW", db.RenditionTypeANIMATEDPREVIEW, assetsv1.Rendition_ANIMATED_PREVIEW},
		{"VIDEO_PROXY", db.RenditionTypeVIDEOPROXY, assetsv1.Rendition_VIDEO_PROXY},
		{"AUDIO_PREVIEW", db.RenditionTypeAUDIOPREVIEW, assetsv1.Rendition_AUDIO_PREVIEW},
		{"POSTER_FRAME", db.RenditionTypePOSTERFRAME, assetsv1.Rendition_POSTER_FRAME},
		{"unknown defaults to TYPE_UNSPECIFIED", db.RenditionType("UNKNOWN"), assetsv1.Rendition_TYPE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renditionType(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSecondsToDuration(t *testing.T) {
	tests := []struct {
		name        string
		secs        float64
		wantSeconds int64
		wantNanos   int32
	}{
		{"whole seconds", 60.0, 60, 0},
		{"zero", 0.0, 0, 0},
		{"fractional seconds", 3.5, 3, 500000000},
		{"small fraction", 0.001, 0, 1000000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := secondsToDuration(tt.secs)
			require.NotNil(t, d)
			assert.Equal(t, tt.wantSeconds, d.Seconds)
			assert.Equal(t, tt.wantNanos, d.Nanos)
		})
	}
}

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

func TestRequestToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)
	dueTime := now.Add(7 * 24 * time.Hour)
	deliveredTime := now.Add(5 * 24 * time.Hour)
	approvedTime := now.Add(6 * 24 * time.Hour)
	annotations := map[string]string{"priority-source": "sla"}
	annotationsJSON, err := json.Marshal(annotations)
	require.NoError(t, err)

	tests := []struct {
		name        string
		row         db.Request
		projectName string
		wantState   assetsv1.Request_State
		checkFunc   func(t *testing.T, pb *assetsv1.Request)
	}{
		{
			name: "full request with all timestamps",
			row: db.Request{
				ID:            uuid.New(),
				Name:          "req-1",
				DisplayName:   "My Request",
				Description:   "Please provide assets",
				State:         db.RequestStateOPEN,
				Priority:      db.RequestPriorityHIGH,
				Assignee:      "editor@test.com",
				Etag:          "etag-req",
				CreatedBy:     "user@test.com",
				UpdatedBy:     "manager@test.com",
				CreateTime:    now,
				UpdateTime:    updated,
				DeleteTime:    pgtype.Timestamptz{Time: now.Add(48 * time.Hour), Valid: true},
				PurgeTime:     pgtype.Timestamptz{Time: now.Add(96 * time.Hour), Valid: true},
				DueTime:       pgtype.Timestamptz{Time: dueTime, Valid: true},
				DeliveredTime: pgtype.Timestamptz{Time: deliveredTime, Valid: true},
				ApprovedTime:  pgtype.Timestamptz{Time: approvedTime, Valid: true},
				Annotations:   annotationsJSON,
			},
			projectName: "organizations/acme/projects/p1",
			wantState:   assetsv1.Request_OPEN,
			checkFunc: func(t *testing.T, pb *assetsv1.Request) {
				assert.NotNil(t, pb.DeleteTime)
				assert.NotNil(t, pb.PurgeTime)
				assert.NotNil(t, pb.DueTime)
				assert.NotNil(t, pb.DeliveredTime)
				assert.NotNil(t, pb.ApprovedTime)
				assert.Equal(t, map[string]string{"priority-source": "sla"}, pb.Annotations)
				assert.Equal(t, assetsv1.Request_HIGH, pb.Priority)
			},
		},
		{
			name: "minimal request without optional timestamps",
			row: db.Request{
				ID:          uuid.New(),
				Name:        "req-2",
				DisplayName: "Simple Request",
				State:       db.RequestStateDRAFT,
				Priority:    db.RequestPriorityNORMAL,
				CreatedBy:   "user@test.com",
				CreateTime:  now,
				UpdateTime:  updated,
			},
			projectName: "organizations/acme/projects/p1",
			wantState:   assetsv1.Request_DRAFT,
			checkFunc: func(t *testing.T, pb *assetsv1.Request) {
				assert.Nil(t, pb.DeleteTime)
				assert.Nil(t, pb.PurgeTime)
				assert.Nil(t, pb.DueTime)
				assert.Nil(t, pb.DeliveredTime)
				assert.Nil(t, pb.ApprovedTime)
				assert.Nil(t, pb.Annotations)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := RequestToProto(tt.row, tt.projectName)
			require.NotNil(t, pb)
			assert.Equal(t, tt.projectName+"/requests/"+tt.row.Name, pb.Name)
			assert.Equal(t, tt.row.DisplayName, pb.DisplayName)
			assert.Equal(t, tt.row.Description, pb.Description)
			assert.Equal(t, tt.wantState, pb.State)
			assert.Equal(t, tt.row.Assignee, pb.Assignee)
			assert.Equal(t, tt.row.Etag, pb.Etag)
			assert.Equal(t, tt.row.CreatedBy, pb.Creator)
			assert.Equal(t, tt.row.UpdatedBy, pb.Updater)
			if tt.checkFunc != nil {
				tt.checkFunc(t, pb)
			}
		})
	}
}

func TestLineItemToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)
	annotations := map[string]string{"source": "import"}
	annotationsJSON, err := json.Marshal(annotations)
	require.NoError(t, err)

	tests := []struct {
		name        string
		row         db.LineItem
		requestName string
		projectName string
		checkFunc   func(t *testing.T, pb *assetsv1.LineItem)
	}{
		{
			name: "line item with asset and media type",
			row: db.LineItem{
				ID:          uuid.New(),
				Name:        "li-1",
				DisplayName: "Photo Request",
				Description: "Need photo",
				State:       db.LineItemStatePENDING,
				MediaType:   db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				AssetID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
				Annotations: annotationsJSON,
				CreatedBy:   "user@test.com",
				CreateTime:  now,
				UpdateTime:  updated,
			},
			requestName: "organizations/acme/projects/p1/requests/req-1",
			projectName: "organizations/acme/projects/p1",
			checkFunc: func(t *testing.T, pb *assetsv1.LineItem) {
				assert.Equal(t, assetsv1.Asset_IMAGE, pb.MediaType)
				assert.NotEmpty(t, pb.Asset)
				assert.Equal(t, map[string]string{"source": "import"}, pb.Annotations)
			},
		},
		{
			name: "line item without optional fields",
			row: db.LineItem{
				ID:          uuid.New(),
				Name:        "li-2",
				DisplayName: "Basic",
				State:       db.LineItemStateINPROGRESS,
				MediaType:   db.NullAssetMediaType{Valid: false},
				AssetID:     pgtype.UUID{Valid: false},
				CreatedBy:   "user@test.com",
				CreateTime:  now,
				UpdateTime:  updated,
			},
			requestName: "organizations/acme/projects/p1/requests/req-1",
			projectName: "organizations/acme/projects/p1",
			checkFunc: func(t *testing.T, pb *assetsv1.LineItem) {
				assert.Equal(t, assetsv1.Asset_MEDIA_TYPE_UNSPECIFIED, pb.MediaType)
				assert.Empty(t, pb.Asset)
				assert.Nil(t, pb.Annotations)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := LineItemToProto(tt.row, tt.requestName, tt.projectName)
			require.NotNil(t, pb)
			assert.Equal(t, tt.requestName+"/lineItems/"+tt.row.Name, pb.Name)
			assert.Equal(t, tt.row.DisplayName, pb.DisplayName)
			assert.Equal(t, tt.row.Description, pb.Description)
			assert.Equal(t, tt.row.CreatedBy, pb.Creator)
			if tt.checkFunc != nil {
				tt.checkFunc(t, pb)
			}
		})
	}
}

func TestRequestState(t *testing.T) {
	tests := []struct {
		name string
		db   db.RequestState
		want assetsv1.Request_State
	}{
		{"DRAFT", db.RequestStateDRAFT, assetsv1.Request_DRAFT},
		{"OPEN", db.RequestStateOPEN, assetsv1.Request_OPEN},
		{"IN_PROGRESS", db.RequestStateINPROGRESS, assetsv1.Request_IN_PROGRESS},
		{"DELIVERED", db.RequestStateDELIVERED, assetsv1.Request_DELIVERED},
		{"APPROVED", db.RequestStateAPPROVED, assetsv1.Request_APPROVED},
		{"REVISION_REQUESTED", db.RequestStateREVISIONREQUESTED, assetsv1.Request_REVISION_REQUESTED},
		{"REJECTED", db.RequestStateREJECTED, assetsv1.Request_REJECTED},
		{"CANCELLED", db.RequestStateCANCELLED, assetsv1.Request_CANCELLED},
		{"unknown defaults to STATE_UNSPECIFIED", db.RequestState("BOGUS"), assetsv1.Request_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestState(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRequestPriority(t *testing.T) {
	tests := []struct {
		name string
		db   db.RequestPriority
		want assetsv1.Request_Priority
	}{
		{"LOW", db.RequestPriorityLOW, assetsv1.Request_LOW},
		{"NORMAL", db.RequestPriorityNORMAL, assetsv1.Request_NORMAL},
		{"HIGH", db.RequestPriorityHIGH, assetsv1.Request_HIGH},
		{"URGENT", db.RequestPriorityURGENT, assetsv1.Request_URGENT},
		{"unknown defaults to PRIORITY_UNSPECIFIED", db.RequestPriority("BOGUS"), assetsv1.Request_PRIORITY_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestPriority(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLineItemState(t *testing.T) {
	tests := []struct {
		name string
		db   db.LineItemState
		want assetsv1.LineItem_State
	}{
		{"PENDING", db.LineItemStatePENDING, assetsv1.LineItem_PENDING},
		{"IN_PROGRESS", db.LineItemStateINPROGRESS, assetsv1.LineItem_IN_PROGRESS},
		{"DELIVERED", db.LineItemStateDELIVERED, assetsv1.LineItem_DELIVERED},
		{"APPROVED", db.LineItemStateAPPROVED, assetsv1.LineItem_APPROVED},
		{"REVISION_REQUESTED", db.LineItemStateREVISIONREQUESTED, assetsv1.LineItem_REVISION_REQUESTED},
		{"unknown defaults to STATE_UNSPECIFIED", db.LineItemState("BOGUS"), assetsv1.LineItem_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lineItemState(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

func TestStorageGatewayToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)
	certExpiry := now.Add(365 * 24 * time.Hour)
	annotations := map[string]string{"region": "us-east-1"}
	annotationsJSON, err := json.Marshal(annotations)
	require.NoError(t, err)

	tests := []struct {
		name      string
		gw        db.StorageGateway
		orgName   string
		checkFunc func(t *testing.T, pb *storagev1.StorageGateway)
	}{
		{
			name: "fully populated gateway",
			gw: db.StorageGateway{
				ID:                uuid.New(),
				Name:              "gw-1",
				DisplayName:       "Gateway One",
				State:             db.StorageGatewayStateACTIVE,
				Hostname:          "gw1.example.com",
				IpAddresses:       []string{"10.0.0.1", "10.0.0.2"},
				RegistrationToken: "token-abc",
				TargetVersion:     "1.2.0",
				CurrentVersion:    "1.1.0",
				CertState:         db.CertStateACTIVE,
				CertExpiryTime:    pgtype.Timestamptz{Time: certExpiry, Valid: true},
				Etag:              "etag-gw",
				CreatedBy:         "admin@test.com",
				UpdatedBy:         "admin@test.com",
				CreateTime:        now,
				UpdateTime:        updated,
				Annotations:       annotationsJSON,
			},
			orgName: "acme",
			checkFunc: func(t *testing.T, pb *storagev1.StorageGateway) {
				assert.Equal(t, "organizations/acme/storageGateways/gw-1", pb.Name)
				assert.Equal(t, storagev1.StorageGateway_ACTIVE, pb.State)
				assert.Equal(t, storagev1.StorageGateway_CERT_ACTIVE, pb.CertState)
				assert.NotNil(t, pb.CertExpiryTime)
				assert.Equal(t, map[string]string{"region": "us-east-1"}, pb.Annotations)
				assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, pb.IpAddresses)
			},
		},
		{
			name: "minimal gateway without optional fields",
			gw: db.StorageGateway{
				ID:             uuid.New(),
				Name:           "gw-2",
				DisplayName:    "Gateway Two",
				State:          db.StorageGatewayStatePROVISIONING,
				CertState:      db.CertStatePENDING,
				CertExpiryTime: pgtype.Timestamptz{Valid: false},
				CreateTime:     now,
				UpdateTime:     updated,
			},
			orgName: "acme",
			checkFunc: func(t *testing.T, pb *storagev1.StorageGateway) {
				assert.Equal(t, storagev1.StorageGateway_PROVISIONING, pb.State)
				assert.Equal(t, storagev1.StorageGateway_PENDING, pb.CertState)
				assert.Nil(t, pb.CertExpiryTime)
				assert.Nil(t, pb.Annotations)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := StorageGatewayToProto(tt.gw, tt.orgName)
			require.NotNil(t, pb)
			assert.Equal(t, tt.gw.DisplayName, pb.DisplayName)
			assert.Equal(t, tt.gw.Hostname, pb.Hostname)
			assert.Equal(t, tt.gw.RegistrationToken, pb.RegistrationToken)
			assert.Equal(t, tt.gw.TargetVersion, pb.TargetVersion)
			assert.Equal(t, tt.gw.CurrentVersion, pb.CurrentVersion)
			assert.Equal(t, tt.gw.Etag, pb.Etag)
			assert.Equal(t, tt.gw.CreatedBy, pb.Creator)
			assert.Equal(t, tt.gw.UpdatedBy, pb.Updater)
			if tt.checkFunc != nil {
				tt.checkFunc(t, pb)
			}
		})
	}
}

func TestAgentToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	certExpiry := now.Add(365 * 24 * time.Hour)

	tests := []struct {
		name        string
		agent       db.StorageAgent
		gatewayName string
		checkFunc   func(t *testing.T, pb *storagev1.Agent)
	}{
		{
			name: "agent with cert expiry",
			agent: db.StorageAgent{
				ID:             uuid.MustParse("0192a000-0001-7000-8000-000000010001"),
				IpAddress:      "10.0.0.10",
				Hostname:       "agent-1.local",
				State:          db.AgentStateCONNECTED,
				Version:        "1.2.0",
				CacheUsedGb:    42,
				JoinTime:       now,
				LastSeenTime:   now.Add(5 * time.Minute),
				CertExpiryTime: pgtype.Timestamptz{Time: certExpiry, Valid: true},
			},
			gatewayName: "organizations/acme/storageGateways/gw-1",
			checkFunc: func(t *testing.T, pb *storagev1.Agent) {
				assert.Equal(t, storagev1.Agent_CONNECTED, pb.State)
				assert.NotNil(t, pb.CertExpiryTime)
				assert.Equal(t, int32(42), pb.CacheUsedGb)
			},
		},
		{
			name: "agent without cert expiry",
			agent: db.StorageAgent{
				ID:             uuid.New(),
				IpAddress:      "10.0.0.11",
				Hostname:       "agent-2.local",
				State:          db.AgentStateCONNECTING,
				Version:        "1.0.0",
				JoinTime:       now,
				LastSeenTime:   now,
				CertExpiryTime: pgtype.Timestamptz{Valid: false},
			},
			gatewayName: "organizations/acme/storageGateways/gw-1",
			checkFunc: func(t *testing.T, pb *storagev1.Agent) {
				assert.Equal(t, storagev1.Agent_CONNECTING, pb.State)
				assert.Nil(t, pb.CertExpiryTime)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := AgentToProto(tt.agent, tt.gatewayName)
			require.NotNil(t, pb)
			assert.Contains(t, pb.Name, tt.gatewayName+"/agents/")
			assert.Equal(t, tt.agent.IpAddress, pb.IpAddress)
			assert.Equal(t, tt.agent.Hostname, pb.Hostname)
			assert.Equal(t, tt.agent.Version, pb.Version)
			if tt.checkFunc != nil {
				tt.checkFunc(t, pb)
			}
		})
	}
}

func TestEndpointToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)
	annotations := map[string]string{"tier": "premium"}
	annotationsJSON, err := json.Marshal(annotations)
	require.NoError(t, err)

	s3Config, err := json.Marshal(map[string]string{
		"type":         "s3",
		"endpoint_uri": "https://s3.amazonaws.com",
		"bucket":       "my-bucket",
		"region":       "us-east-1",
	})
	require.NoError(t, err)

	fsConfig, err := json.Marshal(map[string]string{
		"type": "filesystem",
		"path": "/mnt/storage",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		ep          db.StorageEndpoint
		gatewayName string
		checkFunc   func(t *testing.T, pb *storagev1.Endpoint)
	}{
		{
			name: "S3 endpoint with cache",
			ep: db.StorageEndpoint{
				ID:             uuid.New(),
				Name:           "ep-1",
				DisplayName:    "S3 Endpoint",
				State:          db.EndpointStateACTIVE,
				Configuration:  s3Config,
				CacheEnabled:   true,
				CacheMaxSizeGb: 100,
				CacheEviction:  db.EvictionPolicyLRU,
				CacheTtlHours:  24,
				Etag:           "etag-ep",
				CreatedBy:      "admin@test.com",
				UpdatedBy:      "admin@test.com",
				CreateTime:     now,
				UpdateTime:     updated,
				Annotations:    annotationsJSON,
			},
			gatewayName: "organizations/acme/storageGateways/gw-1",
			checkFunc: func(t *testing.T, pb *storagev1.Endpoint) {
				assert.Equal(t, storagev1.Endpoint_ACTIVE, pb.State)
				require.NotNil(t, pb.CacheConfig)
				assert.True(t, pb.CacheConfig.Enabled)
				assert.Equal(t, int32(100), pb.CacheConfig.MaxSizeGb)
				assert.Equal(t, storagev1.CacheConfig_LRU, pb.CacheConfig.EvictionPolicy)
				assert.Equal(t, int32(24), pb.CacheConfig.TtlHours)
				s3 := pb.GetS3()
				require.NotNil(t, s3)
				assert.Equal(t, "my-bucket", s3.Bucket)
				assert.Equal(t, "us-east-1", s3.Region)
				assert.Equal(t, map[string]string{"tier": "premium"}, pb.Annotations)
			},
		},
		{
			name: "filesystem endpoint",
			ep: db.StorageEndpoint{
				ID:            uuid.New(),
				Name:          "ep-2",
				DisplayName:   "Local Endpoint",
				State:         db.EndpointStateINACTIVE,
				Configuration: fsConfig,
				CacheEnabled:  false,
				CacheEviction: db.EvictionPolicyLFU,
				Etag:          "etag-ep2",
				CreatedBy:     "admin@test.com",
				UpdatedBy:     "admin@test.com",
				CreateTime:    now,
				UpdateTime:    updated,
			},
			gatewayName: "organizations/acme/storageGateways/gw-1",
			checkFunc: func(t *testing.T, pb *storagev1.Endpoint) {
				assert.Equal(t, storagev1.Endpoint_INACTIVE, pb.State)
				fs := pb.GetFilesystem()
				require.NotNil(t, fs)
				assert.Equal(t, "/mnt/storage", fs.Path)
				assert.Equal(t, storagev1.CacheConfig_LFU, pb.CacheConfig.EvictionPolicy)
			},
		},
		{
			name: "endpoint with no configuration",
			ep: db.StorageEndpoint{
				ID:            uuid.New(),
				Name:          "ep-3",
				DisplayName:   "Bare Endpoint",
				State:         db.EndpointStateUNREACHABLE,
				Configuration: nil,
				CacheEviction: db.EvictionPolicyLRU,
				Etag:          "etag-ep3",
				CreateTime:    now,
				UpdateTime:    updated,
			},
			gatewayName: "organizations/acme/storageGateways/gw-1",
			checkFunc: func(t *testing.T, pb *storagev1.Endpoint) {
				assert.Equal(t, storagev1.Endpoint_UNREACHABLE, pb.State)
				assert.Nil(t, pb.Configuration)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := EndpointToProto(tt.ep, tt.gatewayName)
			require.NotNil(t, pb)
			assert.Equal(t, tt.gatewayName+"/endpoints/"+tt.ep.Name, pb.Name)
			assert.Equal(t, tt.ep.DisplayName, pb.DisplayName)
			assert.Equal(t, tt.ep.Etag, pb.Etag)
			assert.Equal(t, tt.ep.CreatedBy, pb.Creator)
			assert.Equal(t, tt.ep.UpdatedBy, pb.Updater)
			if tt.checkFunc != nil {
				tt.checkFunc(t, pb)
			}
		})
	}
}

func TestStorageGatewayState(t *testing.T) {
	tests := []struct {
		name string
		db   db.StorageGatewayState
		want storagev1.StorageGateway_State
	}{
		{"PROVISIONING", db.StorageGatewayStatePROVISIONING, storagev1.StorageGateway_PROVISIONING},
		{"ACTIVE", db.StorageGatewayStateACTIVE, storagev1.StorageGateway_ACTIVE},
		{"DEGRADED", db.StorageGatewayStateDEGRADED, storagev1.StorageGateway_DEGRADED},
		{"OFFLINE", db.StorageGatewayStateOFFLINE, storagev1.StorageGateway_OFFLINE},
		{"unknown defaults to STATE_UNSPECIFIED", db.StorageGatewayState("BOGUS"), storagev1.StorageGateway_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storageGatewayState(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCertState(t *testing.T) {
	tests := []struct {
		name string
		db   db.CertState
		want storagev1.StorageGateway_CertState
	}{
		{"PENDING", db.CertStatePENDING, storagev1.StorageGateway_PENDING},
		{"ACTIVE", db.CertStateACTIVE, storagev1.StorageGateway_CERT_ACTIVE},
		{"EXPIRING", db.CertStateEXPIRING, storagev1.StorageGateway_EXPIRING},
		{"EXPIRED", db.CertStateEXPIRED, storagev1.StorageGateway_EXPIRED},
		{"unknown defaults to CERT_STATE_UNSPECIFIED", db.CertState("BOGUS"), storagev1.StorageGateway_CERT_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := certState(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCacheEvictionPolicy(t *testing.T) {
	tests := []struct {
		name string
		db   db.EvictionPolicy
		want storagev1.CacheConfig_EvictionPolicy
	}{
		{"LRU", db.EvictionPolicyLRU, storagev1.CacheConfig_LRU},
		{"LFU", db.EvictionPolicyLFU, storagev1.CacheConfig_LFU},
		{"unknown defaults to EVICTION_POLICY_UNSPECIFIED", db.EvictionPolicy("BOGUS"), storagev1.CacheConfig_EVICTION_POLICY_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cacheEvictionPolicy(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAgentState(t *testing.T) {
	tests := []struct {
		name string
		db   db.AgentState
		want storagev1.Agent_State
	}{
		{"CONNECTING", db.AgentStateCONNECTING, storagev1.Agent_CONNECTING},
		{"CONNECTED", db.AgentStateCONNECTED, storagev1.Agent_CONNECTED},
		{"DRAINING", db.AgentStateDRAINING, storagev1.Agent_DRAINING},
		{"UPGRADING", db.AgentStateUPGRADING, storagev1.Agent_UPGRADING},
		{"DISCONNECTED", db.AgentStateDISCONNECTED, storagev1.Agent_DISCONNECTED},
		{"unknown defaults to STATE_UNSPECIFIED", db.AgentState("BOGUS"), storagev1.Agent_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agentState(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEndpointState(t *testing.T) {
	tests := []struct {
		name string
		db   db.EndpointState
		want storagev1.Endpoint_State
	}{
		{"ACTIVE", db.EndpointStateACTIVE, storagev1.Endpoint_ACTIVE},
		{"INACTIVE", db.EndpointStateINACTIVE, storagev1.Endpoint_INACTIVE},
		{"UNREACHABLE", db.EndpointStateUNREACHABLE, storagev1.Endpoint_UNREACHABLE},
		{"unknown defaults to STATE_UNSPECIFIED", db.EndpointState("BOGUS"), storagev1.Endpoint_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointState(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Organizations
// ---------------------------------------------------------------------------

func TestOrganizationToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	tests := []struct {
		name      string
		org       db.Organization
		checkFunc func(t *testing.T, pb *apiv1.Organization)
	}{
		{
			name: "active org",
			org: db.Organization{
				ID:          uuid.New(),
				Name:        "my-org",
				DisplayName: "My Org",
				State:       db.ResourceStateACTIVE,
				Etag:        "etag-org",
				Revision:    1,
				CreateTime:  now,
				UpdateTime:  updated,
				DeleteTime:  pgtype.Timestamptz{Valid: false},
				PurgeTime:   pgtype.Timestamptz{Valid: false},
			},
			checkFunc: func(t *testing.T, pb *apiv1.Organization) {
				assert.Equal(t, "organizations/my-org", pb.Name)
				assert.Equal(t, apiv1.Organization_ACTIVE, pb.State)
				assert.Nil(t, pb.DeleteTime)
				assert.Nil(t, pb.PurgeTime)
			},
		},
		{
			name: "delete-requested org with delete/purge times",
			org: db.Organization{
				ID:          uuid.New(),
				Name:        "deleted-org",
				DisplayName: "Deleted Org",
				State:       db.ResourceStateDELETEREQUESTED,
				Etag:        "etag-del",
				Revision:    2,
				CreateTime:  now,
				UpdateTime:  updated,
				DeleteTime:  pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
				PurgeTime:   pgtype.Timestamptz{Time: now.Add(48 * time.Hour), Valid: true},
			},
			checkFunc: func(t *testing.T, pb *apiv1.Organization) {
				assert.Equal(t, apiv1.Organization_DELETE_REQUESTED, pb.State)
				assert.NotNil(t, pb.DeleteTime)
				assert.NotNil(t, pb.PurgeTime)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := OrganizationToProto(tt.org)
			require.NotNil(t, pb)
			assert.Equal(t, tt.org.DisplayName, pb.DisplayName)
			assert.Equal(t, tt.org.Etag, pb.Etag)
			if tt.checkFunc != nil {
				tt.checkFunc(t, pb)
			}
		})
	}
}

func TestOrgState(t *testing.T) {
	tests := []struct {
		name string
		db   db.ResourceState
		want apiv1.Organization_State
	}{
		{"ACTIVE", db.ResourceStateACTIVE, apiv1.Organization_ACTIVE},
		{"DELETE_REQUESTED", db.ResourceStateDELETEREQUESTED, apiv1.Organization_DELETE_REQUESTED},
		{"unknown defaults to STATE_UNSPECIFIED", db.ResourceState("BOGUS"), apiv1.Organization_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orgState(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------

func TestProjectToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	labels := map[string]string{"env": "prod"}
	labelsJSON, err := json.Marshal(labels)
	require.NoError(t, err)

	tests := []struct {
		name      string
		project   db.Project
		orgName   string
		checkFunc func(t *testing.T, pb *apiv1.Project)
	}{
		{
			name: "active project with labels",
			project: db.Project{
				ID:          uuid.New(),
				OrgID:       uuid.New(),
				Name:        "my-project",
				DisplayName: "My Project",
				State:       db.ResourceStateACTIVE,
				Etag:        "etag-proj",
				Labels:      labelsJSON,
				Revision:    1,
				CreateTime:  now,
				UpdateTime:  updated,
				DeleteTime:  pgtype.Timestamptz{Valid: false},
				PurgeTime:   pgtype.Timestamptz{Valid: false},
			},
			orgName: "my-org",
			checkFunc: func(t *testing.T, pb *apiv1.Project) {
				assert.Equal(t, "organizations/my-org/projects/my-project", pb.Name)
				assert.Equal(t, apiv1.Project_ACTIVE, pb.State)
				assert.Equal(t, map[string]string{"env": "prod"}, pb.Labels)
				assert.Nil(t, pb.DeleteTime)
				assert.Nil(t, pb.PurgeTime)
			},
		},
		{
			name: "delete-requested project with timestamps",
			project: db.Project{
				ID:          uuid.New(),
				OrgID:       uuid.New(),
				Name:        "deleted-proj",
				DisplayName: "Deleted Project",
				State:       db.ResourceStateDELETEREQUESTED,
				Etag:        "etag-del",
				Labels:      nil,
				Revision:    2,
				CreateTime:  now,
				UpdateTime:  updated,
				DeleteTime:  pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
				PurgeTime:   pgtype.Timestamptz{Time: now.Add(48 * time.Hour), Valid: true},
			},
			orgName: "my-org",
			checkFunc: func(t *testing.T, pb *apiv1.Project) {
				assert.Equal(t, apiv1.Project_DELETE_REQUESTED, pb.State)
				assert.Nil(t, pb.Labels)
				assert.NotNil(t, pb.DeleteTime)
				assert.NotNil(t, pb.PurgeTime)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := ProjectToProto(tt.project, tt.orgName)
			require.NotNil(t, pb)
			assert.Equal(t, tt.project.DisplayName, pb.DisplayName)
			assert.Equal(t, tt.project.Etag, pb.Etag)
			if tt.checkFunc != nil {
				tt.checkFunc(t, pb)
			}
		})
	}
}

func TestProjectState(t *testing.T) {
	tests := []struct {
		name string
		db   db.ResourceState
		want apiv1.Project_State
	}{
		{"ACTIVE", db.ResourceStateACTIVE, apiv1.Project_ACTIVE},
		{"DELETE_REQUESTED", db.ResourceStateDELETEREQUESTED, apiv1.Project_DELETE_REQUESTED},
		{"unknown defaults to STATE_UNSPECIFIED", db.ResourceState("BOGUS"), apiv1.Project_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectState(tt.db)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

func TestTagKeyToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	id := uuid.MustParse("0192a000-0004-7000-8000-000000410001")
	tk := db.TagKey{
		ID:             id,
		OrgID:          uuid.New(),
		ShortName:      "env",
		NamespacedName: "org-uuid/env",
		Description:    "Environment tag",
		Etag:           "etag-tk",
		Revision:       1,
		CreateTime:     now,
		UpdateTime:     updated,
	}

	t.Run("all fields mapped", func(t *testing.T) {
		proto := TagKeyToProto(tk)
		assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001", proto.Name)
		assert.Equal(t, "Environment tag", proto.Description)
		assert.Equal(t, "etag-tk", proto.Etag)
	})
}

func TestTagValueToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	id := uuid.MustParse("0192a000-0005-7000-8000-000000510001")
	tagKeyID := uuid.MustParse("0192a000-0004-7000-8000-000000410001")
	tv := db.TagValue{
		ID:             id,
		TagKeyID:       tagKeyID,
		ShortName:      "production",
		NamespacedName: "org/env/production",
		Description:    "Production environment",
		Etag:           "etag-tv",
		Revision:       1,
		CreateTime:     now,
		UpdateTime:     updated,
	}

	t.Run("all fields mapped", func(t *testing.T) {
		proto := TagValueToProto(tv)
		assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001/tagValues/0192a000-0005-7000-8000-000000510001", proto.Name)
		assert.Equal(t, "Production environment", proto.Description)
	})
}

func TestTagBindingToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	tbID := uuid.MustParse("0192a000-0006-7000-8000-000000610001")
	tvID := uuid.MustParse("0192a000-0005-7000-8000-000000510001")
	tkID := uuid.MustParse("0192a000-0004-7000-8000-000000410001")

	tb := db.TagBinding{
		ID:             tbID,
		ParentResource: "//pivox.api/organizations/meridian/projects/corp-site",
		TagValueID:     tvID,
		Etag:           "etag-tb",
		CreateTime:     now,
		UpdateTime:     now,
	}
	tv := db.TagValue{
		ID:       tvID,
		TagKeyID: tkID,
	}

	t.Run("resource names formed correctly", func(t *testing.T) {
		proto := TagBindingToProto(tb, tv)
		assert.Equal(t, "tagBindings/0192a000-0006-7000-8000-000000610001", proto.Name)
		assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001/tagValues/0192a000-0005-7000-8000-000000510001", proto.TagValue)
	})
}

func TestEffectiveTagToProto(t *testing.T) {
	tvID := uuid.MustParse("0192a000-0005-7000-8000-000000510001")
	tkID := uuid.MustParse("0192a000-0004-7000-8000-000000410001")

	row := db.ListEffectiveTagsRow{
		TagValueID:             tvID,
		TagValueNamespacedName: "org/env/production",
		TagKeyID:               tkID,
		TagKeyNamespacedName:   "org/env",
	}

	t.Run("resource names formed correctly", func(t *testing.T) {
		proto := EffectiveTagToProto(row)
		assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001/tagValues/0192a000-0005-7000-8000-000000510001", proto.TagValue)
		assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001", proto.TagKey)
		assert.False(t, proto.Inherited)
	})
}

// ---------------------------------------------------------------------------
// API Keys
// ---------------------------------------------------------------------------

func TestApiKeyToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	annotations := map[string]string{"created-by": "terraform"}
	annotationsJSON, err := json.Marshal(annotations)
	require.NoError(t, err)

	browserRestrictions := &apiv1.Restrictions{
		ClientRestrictions: &apiv1.Restrictions_BrowserKeyRestrictions{
			BrowserKeyRestrictions: &apiv1.BrowserKeyRestrictions{
				AllowedReferrers: []string{"https://example.com/*"},
			},
		},
	}
	restrictionsJSON, err := protojson.Marshal(browserRestrictions)
	require.NoError(t, err)

	tests := []struct {
		name      string
		key       db.ApiKey
		orgName   string
		checkFunc func(t *testing.T, pb *apiv1.Key)
	}{
		{
			name: "key with annotations and restrictions",
			key: db.ApiKey{
				ID:           uuid.New(),
				OrgID:        uuid.New(),
				KeyID:        "my-key",
				DisplayName:  "My API Key",
				KeyString:    "AIzaSySecretKeyValue",
				Etag:         "etag-key",
				Annotations:  annotationsJSON,
				Restrictions: restrictionsJSON,
				Revision:     1,
				CreateTime:   now,
				UpdateTime:   updated,
				DeleteTime:   pgtype.Timestamptz{Valid: false},
			},
			orgName: "meridian-broadcasting",
			checkFunc: func(t *testing.T, pb *apiv1.Key) {
				assert.Equal(t, "organizations/meridian-broadcasting/keys/my-key", pb.Name)
				assert.Empty(t, pb.KeyString, "key_string should always be empty")
				assert.Equal(t, map[string]string{"created-by": "terraform"}, pb.Annotations)
				require.NotNil(t, pb.Restrictions)
				browser := pb.Restrictions.GetBrowserKeyRestrictions()
				require.NotNil(t, browser)
				assert.Equal(t, []string{"https://example.com/*"}, browser.AllowedReferrers)
				assert.Nil(t, pb.DeleteTime)
			},
		},
		{
			name: "key with delete time, no annotations or restrictions",
			key: db.ApiKey{
				ID:          uuid.New(),
				OrgID:       uuid.New(),
				KeyID:       "old-key",
				DisplayName: "Old Key",
				KeyString:   "secret",
				Etag:        "etag-old",
				Revision:    3,
				CreateTime:  now,
				UpdateTime:  updated,
				DeleteTime:  pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
			},
			orgName: "acme",
			checkFunc: func(t *testing.T, pb *apiv1.Key) {
				assert.Equal(t, "organizations/acme/keys/old-key", pb.Name)
				assert.Empty(t, pb.KeyString)
				assert.Nil(t, pb.Annotations)
				assert.Nil(t, pb.Restrictions)
				assert.NotNil(t, pb.DeleteTime)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := ApiKeyToProto(tt.key, tt.orgName)
			require.NotNil(t, pb)
			assert.Equal(t, tt.key.DisplayName, pb.DisplayName)
			assert.Equal(t, tt.key.Etag, pb.Etag)
			if tt.checkFunc != nil {
				tt.checkFunc(t, pb)
			}
		})
	}
}
