package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// activeOrganizationAlias returns the single organization alias the caller's
// token is scoped to — Extra[ExtraOrganization][0], populated by the verifier
// from the token's `organization` claim. It errors when the token carries no
// organization: an org-scoped MCP request with no bound org cannot be served.
func activeOrganizationAlias(info *auth.TokenInfo) (string, error) {
	if info == nil {
		return "", errors.New("mcp: missing token info on request context")
	}
	aliases, ok := info.Extra[ExtraOrganization].([]string)
	if !ok || len(aliases) == 0 {
		return "", errors.New("mcp: token is not bound to an organization (request the `organization` scope)")
	}
	return aliases[0], nil
}

// resolveActiveOrganization resolves the caller's active-organization alias to a
// Pivox organization row. The KC org alias equals the Pivox org slug
// (organizations.name), so this is a direct slug lookup — no KC org id needed.
func resolveActiveOrganization(ctx context.Context, q db.Querier, info *auth.TokenInfo) (db.Organization, error) {
	alias, err := activeOrganizationAlias(info)
	if err != nil {
		return db.Organization{}, err
	}
	org, err := q.GetOrganizationByName(ctx, alias)
	if err != nil {
		// Don't surface storage internals (e.g. "no rows in result set") to the
		// MCP client. The alias is the caller's own token-bound org, so echoing it
		// on a not-found is fine; the raw DB error stays in server logs.
		if errors.Is(err, pgx.ErrNoRows) {
			slog.DebugContext(ctx, "mcp: active organization not found", "alias", alias)
			return db.Organization{}, fmt.Errorf("active organization %q not found", alias)
		}
		slog.ErrorContext(ctx, "mcp: resolve active organization failed", "alias", alias, "error", err)
		return db.Organization{}, errors.New("failed to resolve active organization")
	}
	return org, nil
}
