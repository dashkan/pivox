// Copyright 2025 Pivox
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dashboards

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
)

// assetResourceType is the canonical AIP resource type for an
// asset (per `api/proto/pivox/assets/v1/asset.proto` Asset message).
// Phase 5 only supports this single resource_type; other types
// return Unimplemented.
const assetResourceType = "pivox.assets/Asset"

// defaultQueryPageSize / maxQueryPageSize cap the number of rows
// returned per QueryDashboardData call. The widget renders best at
// well under 1000 rows; the upper bound matches AIP-158 guidance.
const (
	defaultQueryPageSize = 100
	maxQueryPageSize     = 1000
)

// QueryDashboardData resolves a `ResourceQuery` into rows of data
// for rendering inside a CollectionWidget. v1 supports
// `query.resource_type == "pivox.assets/Asset"` only; other types
// return Unimplemented until per-type handlers land.
//
// At org parent the handler walks every active asset across every
// space in the organization (via ListAssetsByOrg). At space parent
// it walks the single space's active assets (via ListAssetsBySpace).
func (s *Server) QueryDashboardData(ctx context.Context, req *apiv1.QueryDashboardDataRequest) (*apiv1.QueryDashboardDataResponse, error) {
	if req.GetQuery() == nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("query",
			"query is required"))
	}
	if req.GetQuery().GetResourceType() != assetResourceType {
		return nil, apierr.Unimplemented(
			"QueryDashboardData v1 supports query.resource_type=" + assetResourceType +
				" only; got " + req.GetQuery().GetResourceType())
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = defaultQueryPageSize
	}
	if pageSize > maxQueryPageSize {
		pageSize = maxQueryPageSize
	}

	offset, err := decodePageToken(req.GetPageToken())
	if err != nil {
		return nil, err
	}

	kind, orgSlug, spaceSlug := parseParent(req.GetParent())
	switch kind {
	case scopeOrg:
		return s.queryAssetsAtOrg(ctx, orgSlug, pageSize, offset)
	case scopeSpace:
		return s.queryAssetsAtSpace(ctx, orgSlug, spaceSlug, pageSize, offset)
	default:
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			"expected organizations/{org} or organizations/{org}/spaces/{space}"))
	}
}

func (s *Server) queryAssetsAtOrg(ctx context.Context, orgSlug string, pageSize, offset int32) (*apiv1.QueryDashboardDataResponse, error) {
	resolved := server.MustResolvedOrgFromContext(ctx)

	rows, err := s.queries.ListAssetsByOrg(ctx, db.ListAssetsByOrgParams{
		OrgID:  resolved.ID,
		Limit:  pageSize,
		Offset: offset,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset",
			"organizations/"+orgSlug+":queryDashboardData")
	}

	out := &apiv1.QueryDashboardDataResponse{
		Rows: make([]*structpb.Struct, 0, len(rows)),
	}
	for _, r := range rows {
		row, err := assetRowToStruct(viewFromOrgRow(r), orgSlug, r.SpaceSlug)
		if err != nil {
			return nil, apierr.Internal("synthesize asset row")
		}
		out.Rows = append(out.Rows, row)
	}

	if int32(len(rows)) == pageSize {
		out.NextPageToken = encodePageToken(offset + pageSize)
	}
	return out, nil
}

func (s *Server) queryAssetsAtSpace(ctx context.Context, orgSlug, spaceSlug string, pageSize, offset int32) (*apiv1.QueryDashboardDataResponse, error) {
	resolved := server.MustResolvedSpaceFromContext(ctx)

	rows, err := s.queries.ListAssetsBySpace(ctx, db.ListAssetsBySpaceParams{
		SpaceID: resolved.ID,
		Limit:   pageSize,
		Offset:  offset,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset",
			spaceParentName(orgSlug, spaceSlug)+":queryDashboardData")
	}

	out := &apiv1.QueryDashboardDataResponse{
		Rows: make([]*structpb.Struct, 0, len(rows)),
	}
	for _, r := range rows {
		row, err := assetRowToStruct(viewFromSpaceRow(r), orgSlug, spaceSlug)
		if err != nil {
			return nil, apierr.Internal("synthesize asset row")
		}
		out.Rows = append(out.Rows, row)
	}

	if int32(len(rows)) == pageSize {
		out.NextPageToken = encodePageToken(offset + pageSize)
	}
	return out, nil
}

// assetView is the minimal projection the synthesizer needs from an
// Asset row. Both `db.ListAssetsBySpaceRow` (space-scope query) and
// `db.ListAssetsByOrgRow` (org-scope JOIN query) project into this
// shape via tiny extractors.
//
// Why a separate view rather than passing the row struct directly:
// space-scope and org-scope queries return DIFFERENT struct types
// (the org-scope JOIN adds `space_slug`). Forcing both through a
// shared view that hand-copies fields makes "did I forget a new
// column" a compile error rather than silent data loss in
// production.
//
// Phase 6c additions: VersionNumber, MimeType, EndpointSlug. These
// feed the Phase 6c.2 URL composer (lands in the next sub-phase).
// VersionNumber == 0 is the "no version exists" sentinel (real
// versions start at 1 per the asset_versions monotonic-from-1
// contract; ListAssetsBy* uses COALESCE to surface a sentinel
// because sqlc v1.31 mistypes the LEFT-JOIN-derived nullable columns
// as NOT NULL — see internal/db/queries/assets.sql for details).
// Empty EndpointSlug means the asset has no storage endpoint bound
// (also a "no URL" signal). The composer in 6c.2 reads these to
// decide whether to emit a URL or fall through to the iconField
// path.
type assetView struct {
	NameSlug      string
	DisplayName   string
	MediaType     db.NullAssetMediaType
	ContentType   string
	State         db.AssetState
	SizeBytes     int64
	CreateTime    time.Time
	VersionNumber int32  // 0 = no version exists yet
	MimeType      string // empty when VersionNumber == 0
	EndpointSlug  string // empty when the asset has no endpoint bound
}

func viewFromSpaceRow(r db.ListAssetsBySpaceRow) assetView {
	return assetView{
		NameSlug:      r.Asset.Name,
		DisplayName:   r.Asset.DisplayName,
		MediaType:     r.Asset.MediaType,
		ContentType:   r.Asset.ContentType,
		State:         r.Asset.State,
		SizeBytes:     r.Asset.SizeBytes,
		CreateTime:    r.Asset.CreateTime,
		VersionNumber: r.LatestVersionNumber,
		MimeType:      r.LatestVersionMimeType,
		EndpointSlug:  textOrEmpty(r.EndpointSlug),
	}
}

func viewFromOrgRow(r db.ListAssetsByOrgRow) assetView {
	return assetView{
		NameSlug:      r.Asset.Name,
		DisplayName:   r.Asset.DisplayName,
		MediaType:     r.Asset.MediaType,
		ContentType:   r.Asset.ContentType,
		State:         r.Asset.State,
		SizeBytes:     r.Asset.SizeBytes,
		CreateTime:    r.Asset.CreateTime,
		VersionNumber: r.LatestVersionNumber,
		MimeType:      r.LatestVersionMimeType,
		EndpointSlug:  textOrEmpty(r.EndpointSlug),
	}
}

// textOrEmpty unwraps a pgtype.Text into a plain string, treating
// NULL as "". Used by the assetView extractors for the
// LEFT-JOIN-nullable endpoint_slug column.
func textOrEmpty(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// assetRowToStruct synthesizes a single row's worth of data from
// an Asset view + the slugs needed to compose the full AIP resource
// name. Fields mirror the Asset CollectionWidget's column set
// (display_name, media_type, state, size_bytes, create_time) plus
// the IconConfig contract's `icon` (numeric Icon enum value,
// derived per content) and `thumbnail_url` (empty in v1;
// storage-gateway URL composition lands when the session work
// merges — explicit empty over field-omission keeps the row shape
// stable across the v1 → storage-gateway transition).
func assetRowToStruct(v assetView, orgSlug, spaceSlug string) (*structpb.Struct, error) {
	mediaType := ""
	if v.MediaType.Valid {
		mediaType = string(v.MediaType.AssetMediaType)
	}

	return structpb.NewStruct(map[string]any{
		"name":          "organizations/" + orgSlug + "/spaces/" + spaceSlug + "/assets/" + v.NameSlug,
		"display_name":  v.DisplayName,
		"media_type":    mediaType,
		"state":         string(v.State),
		"size_bytes":    float64(v.SizeBytes), // structpb numbers are float64
		"create_time":   v.CreateTime.UTC().Format(time.RFC3339Nano),
		"icon":          float64(iconForAsset(v)),
		"thumbnail_url": "",
	})
}

// iconForAsset maps an asset's media_type / content_type into the
// numeric Icon enum value the renderer's IconConfig.icon_field
// resolves. Prefers media_type (column, normalized at ingestion);
// falls back to a content_type prefix scan; ultimately defaults to
// ICON_DOCUMENT.
func iconForAsset(v assetView) apiv1.Icon {
	if v.MediaType.Valid {
		switch v.MediaType.AssetMediaType {
		case db.AssetMediaTypeIMAGE, db.AssetMediaTypeGRAPHIC:
			return apiv1.Icon_ICON_PHOTO
		case db.AssetMediaTypeVIDEO:
			return apiv1.Icon_ICON_VIDEO
		case db.AssetMediaTypeAUDIO:
			return apiv1.Icon_ICON_AUDIO
		case db.AssetMediaTypeDOCUMENT:
			return apiv1.Icon_ICON_DOCUMENT
		}
	}

	switch {
	case strings.HasPrefix(v.ContentType, "image/"):
		return apiv1.Icon_ICON_PHOTO
	case strings.HasPrefix(v.ContentType, "video/"):
		return apiv1.Icon_ICON_VIDEO
	case strings.HasPrefix(v.ContentType, "audio/"):
		return apiv1.Icon_ICON_AUDIO
	case strings.HasPrefix(v.ContentType, "text/"):
		return apiv1.Icon_ICON_CODE
	case v.ContentType == "application/zip" ||
		v.ContentType == "application/x-tar" ||
		v.ContentType == "application/x-gzip" ||
		v.ContentType == "application/x-7z-compressed":
		return apiv1.Icon_ICON_ARCHIVE
	default:
		return apiv1.Icon_ICON_DOCUMENT
	}
}

// encodePageToken / decodePageToken implement minimal offset-based
// pagination. v1 is decimal-string-of-offset; it works because the
// resource_type is closed (v1 only queries Asset rows) and the SQL
// ORDER BY pin (`create_time DESC, id DESC`) makes offsets stable
// across calls. The format is NOT opaque-by-design — a customer
// can decode the integer and reuse offsets cross-call, which AIP-158
// discourages.
//
// TODO: when `query.order_by` becomes customer-controllable, switch
// to a keyset cursor (last-row's create_time + id, base64-encoded)
// so concurrent ingest doesn't drop / duplicate rows under offset
// pagination. Today's fixed ORDER BY makes the offset shape safe;
// once ordering varies, it isn't.
func encodePageToken(offset int32) string {
	return strconv.FormatInt(int64(offset), 10)
}

func decodePageToken(tok string) (int32, error) {
	if tok == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(tok, 10, 32)
	if err != nil || n < 0 {
		return 0, apierr.InvalidArgument(apierr.FieldViolation("page_token",
			"page_token must be a non-negative integer (opaque token from a prior response)"))
	}
	return int32(n), nil
}
