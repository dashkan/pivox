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
// New-in-Phase-6c fields the synthesizer's URL composer reads:
//
//   - VersionNumber (int32, sentinel 0 = no version exists)
//   - MimeType      (string, sentinel "" = no version exists)
//   - EndpointSlug  (string, sentinel "" = asset has no endpoint
//     bound; pgtype.Text Valid=false at the DB layer)
//
// Coverage matrix (one case per row, each variant exercises a
// distinct nullability path):
//
//   - "fully populated" — all fields set, including the new ones.
//   - "no version yet" — VersionNumber=0 sentinel, empty MimeType.
//   - "no endpoint bound" — pgtype.Text{Valid:false} → EndpointSlug "".

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
			},
			want: assetView{
				NameSlug:      "logo-final",
				DisplayName:   "Logo Final",
				MediaType:     db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:   "image/png",
				State:         db.AssetStateACTIVE,
				SizeBytes:     245678,
				CreateTime:    now,
				VersionNumber: 1,
				MimeType:      "image/png",
				EndpointSlug:  "primary",
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
			},
			want: assetView{
				NameSlug:      "placeholder",
				DisplayName:   "Placeholder Asset",
				State:         db.AssetStatePLACEHOLDER,
				CreateTime:    now,
				VersionNumber: 0,
				MimeType:      "",
				EndpointSlug:  "",
			},
		},
		{
			name: "no endpoint bound — pgtype.Text invalid → empty slug",
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
			},
			want: assetView{
				NameSlug:      "orphan",
				DisplayName:   "Orphan",
				MediaType:     db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:   "image/jpeg",
				State:         db.AssetStateACTIVE,
				SizeBytes:     1000,
				CreateTime:    now,
				VersionNumber: 1,
				MimeType:      "image/jpeg",
				EndpointSlug:  "",
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
			},
			want: assetView{
				NameSlug:      "logo-final",
				DisplayName:   "Logo Final",
				MediaType:     db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:   "image/png",
				State:         db.AssetStateACTIVE,
				SizeBytes:     245678,
				CreateTime:    now,
				VersionNumber: 1,
				MimeType:      "image/png",
				EndpointSlug:  "primary",
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
			},
			want: assetView{
				NameSlug:      "placeholder",
				DisplayName:   "Placeholder Asset",
				State:         db.AssetStatePLACEHOLDER,
				CreateTime:    now,
				VersionNumber: 0,
				MimeType:      "",
				EndpointSlug:  "",
			},
		},
		{
			name: "no endpoint bound — pgtype.Text invalid → empty slug",
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
			},
			want: assetView{
				NameSlug:      "orphan",
				DisplayName:   "Orphan",
				MediaType:     db.NullAssetMediaType{AssetMediaType: db.AssetMediaTypeIMAGE, Valid: true},
				ContentType:   "image/jpeg",
				State:         db.AssetStateACTIVE,
				SizeBytes:     1000,
				CreateTime:    now,
				VersionNumber: 1,
				MimeType:      "image/jpeg",
				EndpointSlug:  "",
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
