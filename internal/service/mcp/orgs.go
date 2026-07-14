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
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox/internal/apierr"
	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
	"github.com/dashkan/pivox/internal/server"
)

// ListOrgs lists the organizations the caller is a member of. It rides
// the same caller-scoped query the AIP Organizations.ListOrganizations
// uses (ListAccountOrganizationsForIdentity) rather than the generic
// filter engine — the organizations filter config has no membership
// predicate, so filter.Query would return every org, not the caller's.
// The caller's active-org set is small and bounded, so a single page is
// returned (next_page_token stays empty); name_prefix is a
// case-insensitive slug filter applied in-process.
func (s *McpServer) ListOrgs(ctx context.Context, req *mcpv1.ListOrgsRequest) (*mcpv1.ListOrgsResponse, error) {
	callerID := server.MustUserID(ctx)
	rows, err := s.callerActiveOrgs(ctx, callerID)
	if err != nil {
		return nil, err
	}

	prefix := strings.ToLower(req.GetNamePrefix())
	orgs := make([]*mcpv1.Organization, 0, len(rows))
	for _, r := range rows {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(r.Slug), prefix) {
			continue
		}
		orgs = append(orgs, &mcpv1.Organization{Slug: r.Slug, DisplayName: r.DisplayName})
	}
	// Stable slug ordering — deterministic output for an agent surface
	// (the underlying query orders by org id, not slug).
	slices.SortFunc(orgs, func(a, b *mcpv1.Organization) int {
		return strings.Compare(a.GetSlug(), b.GetSlug())
	})
	return &mcpv1.ListOrgsResponse{Orgs: orgs}, nil
}

// GetOrg returns a single organization by slug, gated on the caller's
// membership. It fails closed with NotFound when the org doesn't exist
// OR the caller isn't a member — the same answer either way.
//
// Constant-time: the caller's membership set is resolved FIRST and
// unconditionally, then the org lookup runs regardless. So "org does not
// exist" and "org exists but the caller isn't a member" perform the
// identical two queries (ListAccountOrganizationsForIdentity +
// GetOrganizationByName) — closing the latency existence-oracle that a
// short-circuit on the missing-org path would open.
func (s *McpServer) GetOrg(ctx context.Context, req *mcpv1.GetOrgRequest) (*mcpv1.Organization, error) {
	slug := req.GetOrg()
	if slug == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("org", "must not be empty"))
	}
	notFound := apierr.NotFound("organization", slug)

	callerID := server.MustUserID(ctx)
	orgs, err := s.callerActiveOrgs(ctx, callerID)
	if err != nil {
		return nil, err
	}

	org, err := s.queries.GetOrganizationByName(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound
		}
		return nil, apierr.Internal(err, "get organization")
	}
	if !isMember(orgs, org.ID) {
		return nil, notFound
	}

	return &mcpv1.Organization{Slug: org.Name, DisplayName: org.DisplayName}, nil
}
