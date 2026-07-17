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

package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
	"github.com/dashkan/pivox/internal/server"
)

const (
	// defaultPageSize is used when a ListSpaces request omits page_size.
	defaultPageSize = 25
	// maxPageSize is the hard ceiling the server clamps page_size to.
	maxPageSize = 100
)

// clampPageSize applies the default/ceiling policy: unset (≤0) →
// default, above the ceiling → ceiling.
func clampPageSize(n int32) int32 {
	switch {
	case n <= 0:
		return defaultPageSize
	case n > maxPageSize:
		return maxPageSize
	default:
		return n
	}
}

// ListSpaces lists spaces within a single organization. `org` is
// REQUIRED for v1: the query scopes by parent org, and expressing "spaces
// across the caller's orgs" would need cross-org iteration deferred rather
// than hand-rolled. The handler membership-gates on the parent org, then
// runs a STATIC keyset query (ListSpacesForMCP) — deliberately NOT the
// dynamic internal/filter engine, which the MCP surface must not use.
// name_prefix is a case-insensitive prefix match on the space's display
// name; pagination is keyset-on-id with the same opaque, codec-encrypted
// page tokens the rest of the List surface uses, so tokens issued before
// this migration keep resolving.
func (s *McpServer) ListSpaces(ctx context.Context, req *mcpv1.ListSpacesRequest) (*mcpv1.ListSpacesResponse, error) {
	orgSlug := req.GetOrg()
	if orgSlug == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("org",
			"is required; MCP list_spaces lists one org at a time"))
	}

	// Resolve the caller's membership set FIRST and unconditionally, then
	// the org lookup — so "org does not exist" and "org exists but the
	// caller isn't a member" do identical work and both return NotFound,
	// closing the latency existence-oracle.
	callerID := server.MustUserID(ctx)
	orgs, err := s.callerActiveOrgs(ctx, callerID)
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("organization", orgSlug)
		}
		return nil, apierr.Internal(err, "get organization")
	}
	if !isMember(orgs, org.ID) {
		// Fail closed: don't reveal an org the caller has no binding on.
		return nil, apierr.NotFound("organization", orgSlug)
	}

	// Decode the opaque page token to its keyset anchor. Empty token → first
	// page (uuid.Nil → NULL cursor); a tampered token is a caller error on
	// page_token.
	cursor, err := filter.DecodePageToken(s.codec, req.GetPageToken())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", err.Error()))
	}

	pageSize := clampPageSize(req.GetPageSize())
	rows, err := s.queries.ListSpacesForMCP(ctx, db.ListSpacesForMCPParams{
		OrgID:      org.ID,
		NamePrefix: namePrefixPattern(req.GetNamePrefix()),
		Cursor:     convert.PgUUID(cursor),
		PageLimit:  pageSize + 1, // over-fetch one to detect a further page
	})
	if err != nil {
		return nil, apierr.Internal(err, "list spaces")
	}

	// Trim the over-fetched row and derive the next token from the LAST
	// RETURNED row — filter.Paginate owns both the trim and the encode, so
	// the strict `id > cursor` resume can't drop the boundary row. Paginate
	// is a generic keyset trim helper, NOT the dynamic filter engine.
	page, nextToken, err := filter.Paginate(rows, int(pageSize), func(last db.Space) (string, error) {
		return filter.EncodeNextPageToken(s.codec, last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	spaces := make([]*mcpv1.Space, 0, len(page))
	for _, r := range page {
		spaces = append(spaces, &mcpv1.Space{Org: orgSlug, Slug: r.Name, DisplayName: r.DisplayName})
	}
	return &mcpv1.ListSpacesResponse{Spaces: spaces, NextPageToken: nextToken}, nil
}

// namePrefixPattern builds the ILIKE pattern for the optional display-name
// prefix filter, or a NULL text (no filter) when prefix is empty. It
// preserves EXACTLY the match the removed filter-engine path produced: LIKE
// metacharacters in the caller's input are escaped so '%' and '_' match
// literally, an AIP-160 '*' is treated as a wildcard, and a trailing '*' is
// always appended to anchor a case-insensitive prefix match. The result is
// bound as a query parameter, so caller input can never alter query structure.
func namePrefixPattern(prefix string) pgtype.Text {
	if prefix == "" {
		return pgtype.Text{}
	}
	pattern := prefix + "*"
	pattern = strings.ReplaceAll(pattern, "%", `\%`)
	pattern = strings.ReplaceAll(pattern, "_", `\_`)
	pattern = strings.ReplaceAll(pattern, "*", "%")
	return pgtype.Text{String: pattern, Valid: true}
}

// GetSpace returns a single space by (org, slug), gated on the caller's
// membership in the parent org. It fails closed with a uniform NotFound
// on any miss — wrong org, missing space, or non-member — so the caller
// can't distinguish those cases and probe for existence.
func (s *McpServer) GetSpace(ctx context.Context, req *mcpv1.GetSpaceRequest) (*mcpv1.Space, error) {
	orgSlug := req.GetOrg()
	spaceSlug := req.GetSpace()
	if orgSlug == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("org", "must not be empty"))
	}
	if spaceSlug == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("space", "must not be empty"))
	}
	notFound := apierr.NotFound("space", orgSlug+"/"+spaceSlug)

	// Resolve membership FIRST and unconditionally, then the org lookup —
	// so the org-existence path is constant-time: "org missing" and "org
	// exists but caller not a member" both run [ListAccountOrgs,
	// GetOrganizationByName] and return the same NotFound, never leaking
	// org existence by latency. The space lookup only runs for a member,
	// whose read of their own org's spaces is legitimate.
	callerID := server.MustUserID(ctx)
	orgs, err := s.callerActiveOrgs(ctx, callerID)
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound
		}
		return nil, apierr.Internal(err, "get organization")
	}
	if !isMember(orgs, org.ID) {
		return nil, notFound
	}

	space, err := s.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{OrgID: org.ID, Name: spaceSlug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound
		}
		return nil, apierr.Internal(err, "get space")
	}

	return &mcpv1.Space{Org: org.Name, Slug: space.Name, DisplayName: space.DisplayName}, nil
}
