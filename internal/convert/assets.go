package convert

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
)

// AssetToProto converts a DB asset to proto.
// projectName is the full resource name of the parent project
// (e.g. "organizations/acme/projects/my-project").
func AssetToProto(row db.Asset, projectName string) *assetsv1.Asset {
	pb := &assetsv1.Asset{
		Name:           fmt.Sprintf("%s/assets/%s", projectName, row.Name),
		DisplayName:    row.DisplayName,
		State:          assetState(row.State),
		ContentType:    row.ContentType,
		Filename:       row.Filename,
		ImportPath:     row.ImportPath,
		ChecksumSha256: row.ChecksumSha256,
		SizeBytes:      row.SizeBytes,
		Etag:           row.Etag,
		Creator:        row.CreatedBy,
		Updater:        row.UpdatedBy,
		CreateTime:     timestamppb.New(row.CreateTime),
		UpdateTime:     timestamppb.New(row.UpdateTime),
	}

	if row.MediaType.Valid {
		pb.MediaType = assetMediaType(row.MediaType.AssetMediaType)
	}

	if row.Width.Valid {
		pb.Width = row.Width.Int32
	}
	if row.Height.Valid {
		pb.Height = row.Height.Int32
	}

	if row.DurationSeconds.Valid {
		pb.Duration = secondsToDuration(row.DurationSeconds.Float64)
	}

	if len(row.Annotations) > 0 {
		annotations := make(map[string]string)
		_ = json.Unmarshal(row.Annotations, &annotations)
		pb.Annotations = annotations
	}

	if row.DeleteTime.Valid {
		pb.DeleteTime = timestamppb.New(row.DeleteTime.Time)
	}
	if row.PurgeTime.Valid {
		pb.PurgeTime = timestamppb.New(row.PurgeTime.Time)
	}
	if row.ExpireTime.Valid {
		pb.ExpireTime = timestamppb.New(row.ExpireTime.Time)
	}

	return pb
}

// AssetVersionToProto converts a DB asset version to proto.
// assetName is the full resource name of the parent asset
// (e.g. "organizations/acme/projects/my-project/assets/abc123").
func AssetVersionToProto(row db.AssetVersion, assetName string) *assetsv1.AssetVersion {
	return &assetsv1.AssetVersion{
		Name:           fmt.Sprintf("%s/versions/%s", assetName, row.ID.String()),
		VersionNumber:  row.VersionNumber,
		ChecksumSha256: row.ChecksumSha256,
		SizeBytes:      row.SizeBytes,
		MimeType:       row.MimeType,
		StorageKey:     row.StorageKey,
		ChangeNote:     row.ChangeNote,
		IngestionError: row.IngestionError,
		Creator:        row.CreatedBy,
		CreateTime:     timestamppb.New(row.CreateTime),
	}
}

// RenditionToProto converts a DB asset rendition to proto.
func RenditionToProto(row db.AssetRendition) *assetsv1.Rendition {
	pb := &assetsv1.Rendition{
		Type:       renditionType(row.Type),
		StorageKey: row.StorageKey,
		MimeType:   row.MimeType,
		SizeBytes:  row.SizeBytes,
	}
	if row.Width.Valid {
		pb.Width = row.Width.Int32
	}
	if row.Height.Valid {
		pb.Height = row.Height.Int32
	}
	return pb
}

// RenditionsToProto converts a slice of DB renditions to proto.
func RenditionsToProto(rows []db.AssetRendition) []*assetsv1.Rendition {
	result := make([]*assetsv1.Rendition, 0, len(rows))
	for _, r := range rows {
		result = append(result, RenditionToProto(r))
	}
	return result
}

func assetState(s db.AssetState) assetsv1.Asset_State {
	switch s {
	case db.AssetStatePLACEHOLDER:
		return assetsv1.Asset_PLACEHOLDER
	case db.AssetStatePROCESSING:
		return assetsv1.Asset_PROCESSING
	case db.AssetStateACTIVE:
		return assetsv1.Asset_ACTIVE
	case db.AssetStateFAILED:
		return assetsv1.Asset_FAILED
	case db.AssetStateDELETEREQUESTED:
		return assetsv1.Asset_DELETE_REQUESTED
	default:
		return assetsv1.Asset_STATE_UNSPECIFIED
	}
}

func assetMediaType(mt db.AssetMediaType) assetsv1.Asset_MediaType {
	switch mt {
	case db.AssetMediaTypeIMAGE:
		return assetsv1.Asset_IMAGE
	case db.AssetMediaTypeVIDEO:
		return assetsv1.Asset_VIDEO
	case db.AssetMediaTypeAUDIO:
		return assetsv1.Asset_AUDIO
	case db.AssetMediaTypeDOCUMENT:
		return assetsv1.Asset_DOCUMENT
	default:
		return assetsv1.Asset_MEDIA_TYPE_UNSPECIFIED
	}
}

func renditionType(t db.RenditionType) assetsv1.Rendition_Type {
	switch t {
	case db.RenditionTypeTHUMBNAILSMALL:
		return assetsv1.Rendition_THUMBNAIL_SMALL
	case db.RenditionTypeTHUMBNAILMEDIUM:
		return assetsv1.Rendition_THUMBNAIL_MEDIUM
	case db.RenditionTypeTHUMBNAILLARGE:
		return assetsv1.Rendition_THUMBNAIL_LARGE
	case db.RenditionTypeANIMATEDPREVIEW:
		return assetsv1.Rendition_ANIMATED_PREVIEW
	case db.RenditionTypeVIDEOPROXY:
		return assetsv1.Rendition_VIDEO_PROXY
	case db.RenditionTypeAUDIOPREVIEW:
		return assetsv1.Rendition_AUDIO_PREVIEW
	case db.RenditionTypePOSTERFRAME:
		return assetsv1.Rendition_POSTER_FRAME
	default:
		return assetsv1.Rendition_TYPE_UNSPECIFIED
	}
}

// secondsToDuration converts a float64 number of seconds to a protobuf Duration.
func secondsToDuration(secs float64) *durationpb.Duration {
	whole, frac := math.Modf(secs)
	return &durationpb.Duration{
		Seconds: int64(whole),
		Nanos:   int32(frac * float64(time.Second)),
	}
}
