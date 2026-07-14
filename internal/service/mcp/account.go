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

	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/iam"
)

// GetAccount returns the authenticated caller's own account (whoami).
// It reuses iam.BuildAccount — the exact resolution behind the
// Iam.GetAccount RPC — then projects it down to the lite MCP shape
// (the MCP agent reads active-org membership from ListOrgs instead).
//
// Membership-exempt: a mid-bootstrap caller with no memberships must
// still be able to learn who they are.
func (s *McpServer) GetAccount(ctx context.Context, _ *mcpv1.GetAccountRequest) (*mcpv1.Account, error) {
	identity := server.MustIdentity(ctx)
	acct, err := iam.BuildAccount(ctx, s.queries, identity)
	if err != nil {
		return nil, err
	}
	return &mcpv1.Account{
		Subject:     acct.GetSubject(),
		Email:       acct.GetEmail(),
		DisplayName: acct.GetDisplayName(),
	}, nil
}
