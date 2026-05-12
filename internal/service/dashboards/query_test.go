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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// Pure-function tests for the assetView extractors (`viewFromOrgRow`
// and `viewFromSpaceRow`). The extractors are the boundary that
// catches the silent-data-loss class the assetView godoc warns about
// — a missed field becomes a compile error here, but a wrong-mapping
// would only show up in production. These tests pin the per-field
// mapping for both shapes.
//
// Phase 6c fields the synthesizer's URL composer reads:
//
//   - AssetID         (UUID stringified — composer's URL path segment)
//   - VersionNumber   (int32, sentinel 0 = no version exists)
//   - MimeType        (string, sentinel "" — note ambiguity below)
//   - EndpointSlug    (string, sentinel "" = asset has no endpoint
//     bound; pgtype.Text Valid=false at the DB layer)
//   - GatewayHostname (string, sentinel "" = endpoint's gateway has
//     no hostname configured)
//
// Coverage matrix — one case per row, each variant exercising a
// distinct nullability path:
//
//   - "fully populated" — all fields set, including the new ones.
//   - "no version yet" — VersionNumber=0 sentinel, empty MimeType.
//   - "no endpoint bound" — pgtype.Text{Valid:false} on EndpointSlug.
//   - "no gateway hostname" — endpoint bound but gateway lacks a
//     hostname value; pgtype.Text{Valid:false} on GatewayHostname.

func TestViewFromOrgRow_CopiesAllFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	id := uuid.New()
	cases := []struct {
		name string
		in   db.ListAssetsByOrgRow
		want assetView
	}{
		{
			name: "fully populated",
			in: db.ListAssetsByOrgRow{
				Asset: db.Asset{
					ID:          id,
					Name:        "logo-final",
					DisplayName: "Logo Final",
					MediaType:   db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
					ContentType: "image/png",
					State:       db.AssetStateACTIVE,
					SizeBytes:   245678,
					CreateTime:  now,
				},
				LatestVersionNumber:   1,
				LatestVersionMimeType: "image/png",
				EndpointSlug:          pgtype.Text{String: "primary", Valid: true},
				GatewayHostname:       pgtype.Text{String: "pivox.ngrok.app", Valid: true},
			},
			want: assetView{
				NameSlug:        "logo-final",
				DisplayName:     "Logo Final",
				MediaType:       db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:     "image/png",
				State:           db.AssetStateACTIVE,
				SizeBytes:       245678,
				CreateTime:      now,
				AssetID:         id.String(),
				VersionNumber:   1,
				MimeType:        "image/png",
				EndpointSlug:    "primary",
				GatewayHostname: "pivox.ngrok.app",
			},
		},
		{
			name: "no version yet — sentinel 0 / empty mime preserved",
			in: db.ListAssetsByOrgRow{
				Asset: db.Asset{
					ID:          id,
					Name:        "placeholder",
					DisplayName: "Placeholder Asset",
					State:       db.AssetStatePLACEHOLDER,
					CreateTime:  now,
				},
				LatestVersionNumber:   0,
				LatestVersionMimeType: "",
				EndpointSlug:          pgtype.Text{Valid: false},
				GatewayHostname:       pgtype.Text{Valid: false},
			},
			want: assetView{
				NameSlug:      "placeholder",
				DisplayName:   "Placeholder Asset",
				State:         db.AssetStatePLACEHOLDER,
				CreateTime:    now,
				AssetID:       id.String(),
				VersionNumber: 0,
				MimeType:      "",
				EndpointSlug:  "",
			},
		},
		{
			name: "no endpoint bound — pgtype.Text invalid → empty slug + empty gateway",
			in: db.ListAssetsByOrgRow{
				Asset: db.Asset{
					ID:          id,
					Name:        "orphan",
					DisplayName: "Orphan",
					MediaType:   db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
					ContentType: "image/jpeg",
					State:       db.AssetStateACTIVE,
					SizeBytes:   1000,
					CreateTime:  now,
				},
				LatestVersionNumber:   1,
				LatestVersionMimeType: "image/jpeg",
				EndpointSlug:          pgtype.Text{Valid: false},
				GatewayHostname:       pgtype.Text{Valid: false},
			},
			want: assetView{
				NameSlug:      "orphan",
				DisplayName:   "Orphan",
				MediaType:     db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:   "image/jpeg",
				State:         db.AssetStateACTIVE,
				SizeBytes:     1000,
				CreateTime:    now,
				AssetID:       id.String(),
				VersionNumber: 1,
				MimeType:      "image/jpeg",
				EndpointSlug:  "",
			},
		},
		{
			name: "no gateway hostname — endpoint bound but gateway hostname empty",
			in: db.ListAssetsByOrgRow{
				Asset: db.Asset{
					ID:          id,
					Name:        "endpoint-but-no-host",
					DisplayName: "Endpoint Without Hostname",
					MediaType:   db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
					ContentType: "image/png",
					State:       db.AssetStateACTIVE,
					SizeBytes:   500,
					CreateTime:  now,
				},
				LatestVersionNumber:   1,
				LatestVersionMimeType: "image/png",
				EndpointSlug:          pgtype.Text{String: "primary", Valid: true},
				GatewayHostname:       pgtype.Text{Valid: false},
			},
			want: assetView{
				NameSlug:      "endpoint-but-no-host",
				DisplayName:   "Endpoint Without Hostname",
				MediaType:     db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:   "image/png",
				State:         db.AssetStateACTIVE,
				SizeBytes:     500,
				CreateTime:    now,
				AssetID:       id.String(),
				VersionNumber: 1,
				MimeType:      "image/png",
				EndpointSlug:  "primary",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := viewFromOrgRow(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestViewFromSpaceRow_CopiesAllFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	id := uuid.New()
	cases := []struct {
		name string
		in   db.ListAssetsBySpaceRow
		want assetView
	}{
		{
			name: "fully populated",
			in: db.ListAssetsBySpaceRow{
				Asset: db.Asset{
					ID:          id,
					Name:        "logo-final",
					DisplayName: "Logo Final",
					MediaType:   db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
					ContentType: "image/png",
					State:       db.AssetStateACTIVE,
					SizeBytes:   245678,
					CreateTime:  now,
				},
				LatestVersionNumber:   1,
				LatestVersionMimeType: "image/png",
				EndpointSlug:          pgtype.Text{String: "primary", Valid: true},
				GatewayHostname:       pgtype.Text{String: "pivox.ngrok.app", Valid: true},
			},
			want: assetView{
				NameSlug:        "logo-final",
				DisplayName:     "Logo Final",
				MediaType:       db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:     "image/png",
				State:           db.AssetStateACTIVE,
				SizeBytes:       245678,
				CreateTime:      now,
				AssetID:         id.String(),
				VersionNumber:   1,
				MimeType:        "image/png",
				EndpointSlug:    "primary",
				GatewayHostname: "pivox.ngrok.app",
			},
		},
		{
			name: "no version yet — sentinel 0 / empty mime preserved",
			in: db.ListAssetsBySpaceRow{
				Asset: db.Asset{
					ID:          id,
					Name:        "placeholder",
					DisplayName: "Placeholder Asset",
					State:       db.AssetStatePLACEHOLDER,
					CreateTime:  now,
				},
				LatestVersionNumber:   0,
				LatestVersionMimeType: "",
				EndpointSlug:          pgtype.Text{Valid: false},
				GatewayHostname:       pgtype.Text{Valid: false},
			},
			want: assetView{
				NameSlug:      "placeholder",
				DisplayName:   "Placeholder Asset",
				State:         db.AssetStatePLACEHOLDER,
				CreateTime:    now,
				AssetID:       id.String(),
				VersionNumber: 0,
				MimeType:      "",
				EndpointSlug:  "",
			},
		},
		{
			name: "no endpoint bound — pgtype.Text invalid → empty slug + empty gateway",
			in: db.ListAssetsBySpaceRow{
				Asset: db.Asset{
					ID:          id,
					Name:        "orphan",
					DisplayName: "Orphan",
					MediaType:   db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
					ContentType: "image/jpeg",
					State:       db.AssetStateACTIVE,
					SizeBytes:   1000,
					CreateTime:  now,
				},
				LatestVersionNumber:   1,
				LatestVersionMimeType: "image/jpeg",
				EndpointSlug:          pgtype.Text{Valid: false},
				GatewayHostname:       pgtype.Text{Valid: false},
			},
			want: assetView{
				NameSlug:      "orphan",
				DisplayName:   "Orphan",
				MediaType:     db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:   "image/jpeg",
				State:         db.AssetStateACTIVE,
				SizeBytes:     1000,
				CreateTime:    now,
				AssetID:       id.String(),
				VersionNumber: 1,
				MimeType:      "image/jpeg",
				EndpointSlug:  "",
			},
		},
		{
			name: "no gateway hostname — endpoint bound but gateway hostname empty",
			in: db.ListAssetsBySpaceRow{
				Asset: db.Asset{
					ID:          id,
					Name:        "endpoint-but-no-host",
					DisplayName: "Endpoint Without Hostname",
					MediaType:   db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
					ContentType: "image/png",
					State:       db.AssetStateACTIVE,
					SizeBytes:   500,
					CreateTime:  now,
				},
				LatestVersionNumber:   1,
				LatestVersionMimeType: "image/png",
				EndpointSlug:          pgtype.Text{String: "primary", Valid: true},
				GatewayHostname:       pgtype.Text{Valid: false},
			},
			want: assetView{
				NameSlug:      "endpoint-but-no-host",
				DisplayName:   "Endpoint Without Hostname",
				MediaType:     db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:   "image/png",
				State:         db.AssetStateACTIVE,
				SizeBytes:     500,
				CreateTime:    now,
				AssetID:       id.String(),
				VersionNumber: 1,
				MimeType:      "image/png",
				EndpointSlug:  "primary",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := viewFromSpaceRow(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestComposeStorageURL pins the composer's per-row decision logic
// for thumbnail URL synthesis. Output shape:
//
//	https://{GatewayHostname}/files/{EndpointSlug}/{org}/{space}/assets/{AssetID}/v{VersionNumber}/thumb_md.webp
//
// Empty-string return on any of:
//
//   - VersionNumber == 0   (CHECK FIRST — only unambiguous "no version" signal)
//   - EndpointSlug == ""
//   - GatewayHostname == ""
//   - !isImageShaped(v)    (media_type ∉ {IMAGE, GRAPHIC} AND
//     content_type doesn't start with "image/")
//
// Coverage matrix exercises every gate plus the GRAPHIC enum and
// content-type prefix-fallback paths so a future regression on any
// gate surfaces as a single named failure.
func TestComposeStorageURL(t *testing.T) {
	t.Parallel()

	const (
		assetID    = "0192a000-0030-7000-8000-310001000001"
		orgSlug    = "meridian-broad"
		spaceSlug  = "corp-site"
		gwHost     = "pivox.ngrok.app"
		epSlug     = "meridian-hq-west"
		expectedOK = "https://pivox.ngrok.app/files/meridian-hq-west/meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000001/v1/thumb_md.webp"
	)

	base := assetView{
		AssetID:         assetID,
		VersionNumber:   1,
		MimeType:        "image/png",
		EndpointSlug:    epSlug,
		GatewayHostname: gwHost,
		MediaType:       db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
		ContentType:     "image/png",
	}

	with := func(mut func(*assetView)) assetView {
		v := base
		mut(&v)
		return v
	}

	cases := []struct {
		name string
		in   assetView
		want string
	}{
		{
			name: "image asset, all fields populated",
			in:   base,
			want: expectedOK,
		},
		{
			name: "VersionNumber == 0 — sentinel for no version exists",
			in:   with(func(v *assetView) { v.VersionNumber = 0 }),
			want: "",
		},
		{
			name: "EndpointSlug empty — no endpoint bound",
			in:   with(func(v *assetView) { v.EndpointSlug = "" }),
			want: "",
		},
		{
			name: "GatewayHostname empty — gateway hostname not configured",
			in:   with(func(v *assetView) { v.GatewayHostname = "" }),
			want: "",
		},
		{
			name: "VIDEO media_type — non-image falls through to iconField path",
			in: with(func(v *assetView) {
				v.MediaType = db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeVIDEO, Valid: true}
				v.ContentType = "video/mp4"
				v.MimeType = "video/mp4"
			}),
			want: "",
		},
		{
			name: "GRAPHIC media_type — qualifies via the {IMAGE, GRAPHIC} enum branch",
			in: with(func(v *assetView) {
				v.MediaType = db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeGRAPHIC, Valid: true}
				v.ContentType = "image/svg+xml"
				v.MimeType = "image/svg+xml"
			}),
			want: "https://pivox.ngrok.app/files/meridian-hq-west/meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000001/v1/thumb_md.webp",
		},
		{
			name: "NULL media_type + image/webp — content-type prefix fallback (asset …000009)",
			in: with(func(v *assetView) {
				v.MediaType = db.NullAssetMediaType{Valid: false}
				v.ContentType = "image/webp"
				v.MimeType = "image/webp"
			}),
			want: "https://pivox.ngrok.app/files/meridian-hq-west/meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000001/v1/thumb_md.webp",
		},
		{
			name: "empty content_type — neither enum nor prefix qualifies (asset …00000a)",
			in: with(func(v *assetView) {
				v.MediaType = db.NullAssetMediaType{Valid: false}
				v.ContentType = ""
				v.MimeType = ""
			}),
			want: "",
		},
		{
			name: "DOCUMENT media_type with application/pdf — neither qualifies",
			in: with(func(v *assetView) {
				v.MediaType = db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeDOCUMENT, Valid: true}
				v.ContentType = "application/pdf"
				v.MimeType = "application/pdf"
			}),
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := composeStorageURL(tc.in, orgSlug, spaceSlug)
			assert.Equal(t, tc.want, got)
		})
	}
}
