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

// Package mcp implements the McpService gRPC handlers — the curated,
// non-AIP surface backing the Model Context Protocol server
// (internal/mcp). Handlers are thin and deliberately STATIC: this
// agent-facing surface must NOT use the dynamic internal/filter engine
// (AIP-160 filter + order_by). ListOrgs rides the caller-scoped
// membership query with an in-process slug prefix filter; ListSpaces
// rides a dedicated hand-written keyset query (ListSpacesForMCP) with a
// fixed display-name prefix match; the single-gets reuse existing
// queries. Pagination uses the shared codec-encrypted page tokens
// (filter.Paginate is a generic keyset trim helper, not the engine).
//
// TRANSPORT vs. SECURITY BOUNDARY. McpService is registered on the
// shared gRPC server, so it is reachable on the public TCP listener as
// well as the in-process bufconn — it is NOT transport-isolated. The
// security boundary is instead: (1) the MCP-audience token (the auth
// interceptor verifies pivox.mcp.v1.* with the MCP verifier, so a
// main-API token is rejected here and vice versa), (2) the membership
// interceptor, and (3) the per-handler read gate below.
//
// AUTHORIZATION. These handlers run behind the full production
// interceptor chain (auth → membership → permission), but the
// McpService methods are `exempt` from the permission interceptor by
// design (see mcp_service.proto). GetOrg/GetSpace/ListSpaces therefore
// membership-gate IN THE HANDLER against the caller's own org
// memberships, resolving membership FIRST (constant-time) and failing
// closed with NotFound — never PermissionDenied — so this agent-facing
// surface can't be used as an existence oracle (by response OR latency)
// for orgs/spaces the caller has no binding on.
package mcp

import (
	"context"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
)

// McpServer implements mcpv1.McpServiceServer.
type McpServer struct {
	mcpv1.UnimplementedMcpServiceServer
	queries db.Querier
	codec   *appkey.Codec
}

// Config is the constructor input for McpServer.
type Config struct {
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec encodes/decodes the opaque, codec-encrypted page tokens the
	// list surface issues. Required.
	Codec *appkey.Codec
}

// NewMcpServer constructs the server from cfg. Panics on a missing
// required field — a startup-time programmer error rather than a
// runtime nil-deref mid-RPC.
func NewMcpServer(cfg Config) *McpServer {
	if cfg.Queries == nil {
		panic("mcp: Config.Queries is required")
	}
	if cfg.Codec == nil {
		panic("mcp: Config.Codec is required")
	}
	return &McpServer{queries: cfg.Queries, codec: cfg.Codec}
}

// callerActiveOrgs returns the caller's active (org, role) memberships.
// It is the single source of the in-handler membership gate: an org (or
// space's parent org) not in this set is one the caller has no binding
// on, and every read that can't confirm membership fails closed.
func (s *McpServer) callerActiveOrgs(ctx context.Context, callerID uuid.UUID) ([]db.ListAccountOrganizationsForIdentityRow, error) {
	rows, err := s.queries.ListAccountOrganizationsForIdentity(ctx, convert.PgUUID(callerID))
	if err != nil {
		return nil, apierr.Internal(err, "list organizations")
	}
	return rows, nil
}

// isMember reports whether the caller is an active member of orgID.
func isMember(orgs []db.ListAccountOrganizationsForIdentityRow, orgID uuid.UUID) bool {
	for _, o := range orgs {
		if o.ID == orgID {
			return true
		}
	}
	return false
}
