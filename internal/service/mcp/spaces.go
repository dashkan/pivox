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
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox/internal/apierr"
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
// REQUIRED for v1: the shared filter engine scopes by parent org, and
// expressing "spaces across the caller's orgs" would need cross-org
// iteration the engine can't do in one call — deferred rather than
// hand-rolled. The handler membership-gates on the parent org, then
// rides the SAME filter.Query path (config, pagination, ordering) the
// AIP Spaces.ListSpaces uses, so page tokens and page_size behave
// identically. name_prefix maps to a partial-match filter expression on
// the space's display name (the filter config's partial-capable field).
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

	pageSize := clampPageSize(req.GetPageSize())
	rows, err := filter.Query(ctx, s.pool, filter.SpaceFilter(), filter.QueryParams{
		Filter:   namePrefixFilter(req.GetNamePrefix()),
		ParentID: org.ID.String(),
		PageSize: pageSize,
		Cursor:   req.GetPageToken(),
		Codec:    s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", err.Error()))
	}
	results, err := filter.ScanSpaces(rows)
	if err != nil {
		return nil, apierr.Internal(err, "database error")
	}

	// The engine over-fetches by one to detect a further page. If we got
	// more than pageSize rows, trim to pageSize and anchor the next
	// token at the LAST RETURNED row: the cursor query is `id > token`
	// (strict), so encoding the last returned id makes the next page
	// resume at the first trimmed row. (NB: the AIP SpacesServer.ListSpaces
	// anchors at results[pageSize] instead, which strictly skips that
	// boundary row — a pre-existing off-by-one; not replicated here.)
	var nextToken string
	if len(results) > int(pageSize) {
		results = results[:pageSize]
		nextToken, err = filter.EncodeNextPageToken(s.codec, results[len(results)-1].ID)
		if err != nil {
			return nil, apierr.Internal(err, "encode page token")
		}
	}

	spaces := make([]*mcpv1.Space, 0, len(results))
	for _, r := range results {
		spaces = append(spaces, &mcpv1.Space{Org: orgSlug, Slug: r.Name, DisplayName: r.DisplayName})
	}
	return &mcpv1.ListSpacesResponse{Spaces: spaces, NextPageToken: nextToken}, nil
}

// namePrefixFilter renders an AIP-160 filter expression that
// prefix-matches the space display name, or "" when no prefix was
// given. The value is quoted so arbitrary client input can't break the
// filter grammar; the engine converts the trailing `*` to an ILIKE
// prefix on the partial-capable displayName field.
func namePrefixFilter(prefix string) string {
	if prefix == "" {
		return ""
	}
	return "displayName = " + strconv.Quote(prefix+"*")
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
