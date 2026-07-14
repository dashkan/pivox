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
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
	"github.com/dashkan/pivox/internal/server"
)

// recordingQuerier is a minimal db.Querier that records the sequence of
// the (few) methods the MCP read handlers call. It exists ONLY to pin
// the constant-time ordering property — that membership resolution runs
// FIRST and unconditionally, so "org missing" and "org exists but caller
// not a member" perform identical work. Embedding db.Querier means any
// un-implemented method panics if a handler unexpectedly calls it.
type recordingQuerier struct {
	db.Querier
	calls        []string
	orgs         []db.ListAccountOrganizationsForIdentityRow
	orgsByName   map[string]db.Organization
	spacesByName map[string]db.Space
}

func (r *recordingQuerier) ListAccountOrganizationsForIdentity(context.Context, pgtype.UUID) ([]db.ListAccountOrganizationsForIdentityRow, error) {
	r.calls = append(r.calls, "ListAccountOrganizationsForIdentity")
	return r.orgs, nil
}

func (r *recordingQuerier) GetOrganizationByName(_ context.Context, name string) (db.Organization, error) {
	r.calls = append(r.calls, "GetOrganizationByName")
	o, ok := r.orgsByName[name]
	if !ok {
		return db.Organization{}, pgx.ErrNoRows
	}
	return o, nil
}

func (r *recordingQuerier) GetSpaceByName(_ context.Context, arg db.GetSpaceByNameParams) (db.Space, error) {
	r.calls = append(r.calls, "GetSpaceByName")
	s, ok := r.spacesByName[arg.Name]
	if !ok {
		return db.Space{}, pgx.ErrNoRows
	}
	return s, nil
}

func ctxWithCaller(caller uuid.UUID) context.Context {
	return server.WithUserID(context.Background(), caller)
}

// TestGetOrg_ConstantTimeGate pins that GetOrg resolves membership
// before/independent of the org lookup, so a non-member cannot tell a
// real org from a missing one by latency: both do the identical query
// sequence and return the same NotFound.
func TestGetOrg_ConstantTimeGate(t *testing.T) {
	t.Parallel()
	caller := uuid.New()
	orgID := uuid.New()
	ctx := ctxWithCaller(caller)

	// Case A: the org does not exist.
	missing := &recordingQuerier{orgsByName: map[string]db.Organization{}}
	_, errA := (&McpServer{queries: missing}).GetOrg(ctx, &mcpv1.GetOrgRequest{Org: "acme"})
	assert.Equal(t, codes.NotFound, status.Code(errA))

	// Case B: the org exists, but the caller has no membership.
	notMember := &recordingQuerier{
		orgsByName: map[string]db.Organization{"acme": {ID: orgID, Name: "acme"}},
		// caller's active orgs: empty → not a member of acme.
	}
	_, errB := (&McpServer{queries: notMember}).GetOrg(ctx, &mcpv1.GetOrgRequest{Org: "acme"})
	assert.Equal(t, codes.NotFound, status.Code(errB))

	// The load-bearing assertion: both non-authorized cases did the SAME
	// work in the SAME order, with membership resolved first.
	want := []string{"ListAccountOrganizationsForIdentity", "GetOrganizationByName"}
	assert.Equal(t, want, missing.calls, "missing-org path")
	assert.Equal(t, want, notMember.calls, "not-member path")
	assert.Equal(t, missing.calls, notMember.calls, "the two cases must be indistinguishable by work done")

	// Sanity: a member succeeds via the same leading sequence.
	member := &recordingQuerier{
		orgs:       []db.ListAccountOrganizationsForIdentityRow{{ID: orgID, Slug: "acme", DisplayName: "Acme"}},
		orgsByName: map[string]db.Organization{"acme": {ID: orgID, Name: "acme", DisplayName: "Acme"}},
	}
	org, err := (&McpServer{queries: member}).GetOrg(ctx, &mcpv1.GetOrgRequest{Org: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "acme", org.GetSlug())
	assert.Equal(t, want, member.calls)
}

// TestGetSpace_ConstantTimeOrgGate pins that GetSpace's ORG-existence
// path is constant-time: a missing org and an org the caller can't see
// both stop after [ListAccountOrgs, GetOrganizationByName] with NotFound,
// never reaching (and thus never timing) the space lookup.
func TestGetSpace_ConstantTimeOrgGate(t *testing.T) {
	t.Parallel()
	caller := uuid.New()
	orgID := uuid.New()
	ctx := ctxWithCaller(caller)

	missing := &recordingQuerier{orgsByName: map[string]db.Organization{}}
	_, errA := (&McpServer{queries: missing}).GetSpace(ctx, &mcpv1.GetSpaceRequest{Org: "acme", Space: "prod"})
	assert.Equal(t, codes.NotFound, status.Code(errA))

	notMember := &recordingQuerier{
		orgsByName: map[string]db.Organization{"acme": {ID: orgID, Name: "acme"}},
	}
	_, errB := (&McpServer{queries: notMember}).GetSpace(ctx, &mcpv1.GetSpaceRequest{Org: "acme", Space: "prod"})
	assert.Equal(t, codes.NotFound, status.Code(errB))

	want := []string{"ListAccountOrganizationsForIdentity", "GetOrganizationByName"}
	assert.Equal(t, want, missing.calls, "missing-org path never reaches the space lookup")
	assert.Equal(t, want, notMember.calls, "not-member path never reaches the space lookup")
	assert.Equal(t, missing.calls, notMember.calls)

	// A member proceeds to the space lookup (their own org's spaces are
	// legitimately visible).
	member := &recordingQuerier{
		orgs:         []db.ListAccountOrganizationsForIdentityRow{{ID: orgID, Slug: "acme"}},
		orgsByName:   map[string]db.Organization{"acme": {ID: orgID, Name: "acme"}},
		spacesByName: map[string]db.Space{"prod": {Name: "prod", DisplayName: "Prod"}},
	}
	sp, err := (&McpServer{queries: member}).GetSpace(ctx, &mcpv1.GetSpaceRequest{Org: "acme", Space: "prod"})
	require.NoError(t, err)
	assert.Equal(t, "prod", sp.GetSlug())
	assert.Equal(t, []string{"ListAccountOrganizationsForIdentity", "GetOrganizationByName", "GetSpaceByName"}, member.calls)
}
