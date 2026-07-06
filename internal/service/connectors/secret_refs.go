package connectors

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// secretRefRe matches a literal secret("<name>") call in a connector-config
// CEL value, capturing the referenced Secret resource name.
//
// LITERAL-ONLY ASSUMPTION: the guard this feeds relies on every secret() arg
// being a string LITERAL, so refs are statically extractable here. The Phase-6
// CEL layer enforces literal-only args; a dynamic secret(expr) is out of scope
// for this phase and is deliberately not matched — such an expression simply
// tracks no ref (and the CEL layer will reject it before it can run).
var secretRefRe = regexp.MustCompile(`secret\(\s*"([^"]+)"\s*\)`)

// extractSecretRefs returns the distinct Secret resource names a connector's
// config references via secret("…"). Today only HttpConnector.headers carry
// CEL; as new connector config types gain CEL-bearing fields, collect their
// values here — the dedup + scan below is config-shape agnostic.
func extractSecretRefs(in *workflowsv1.Connector) []string {
	var celValues []string
	if http := in.GetHttp(); http != nil {
		for _, v := range http.GetHeaders() {
			celValues = append(celValues, v)
		}
	}

	var refs []string
	seen := make(map[string]struct{})
	for _, v := range celValues {
		for _, m := range secretRefRe.FindAllStringSubmatch(v, -1) {
			name := m[1]
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			refs = append(refs, name)
		}
	}
	return refs
}

// parseSecretRefLeaf extracts the leaf UUID from a Secret resource name
// ("organizations/{org}[/spaces/{space}]/secrets/{uuid}"). Mirrors the secrets
// package's own name parsing; a malformed name or non-UUID leaf is a ref error
// (there is no such secret to point at).
func parseSecretRefLeaf(name string) (uuid.UUID, error) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return uuid.Nil, errors.New("not a secret resource name")
	}
	id, err := uuid.Parse(name[idx+1:])
	if err != nil {
		return uuid.Nil, errors.New("secret name leaf is not a uuid")
	}
	return id, nil
}

// trackSecretRefs re-derives and records the vault Secrets a connector's
// config references, replacing any previously tracked set. It runs inside the
// connector-write tx (qtx) so the refs are atomic with the connector row and
// roll back with a validate_only request.
//
// Each secret("…") arg is resolved (in the same tx) against the connector's
// own scope: a ref to a missing or cross-scope secret is a client error —
// you can't save a connector pointing at a secret that isn't yours. This is
// the write half of the loop the DeleteSecret guard closes: a secret can't be
// deleted while a tracked ref points at it.
func trackSecretRefs(ctx context.Context, qtx db.Querier, connectorID, orgID uuid.UUID, spaceID pgtype.UUID, in *workflowsv1.Connector) error {
	refs := extractSecretRefs(in)

	ids := make([]uuid.UUID, 0, len(refs))
	seen := make(map[uuid.UUID]struct{}, len(refs))
	for _, name := range refs {
		secretID, err := parseSecretRefLeaf(name)
		if err != nil {
			return apierr.InvalidArgument(apierr.FieldViolation("connector.config",
				fmt.Sprintf("secret reference %q is not a valid Secret resource name", name)))
		}
		// FOR UPDATE locks the referenced secret for this tx, serializing
		// against a concurrent DeleteSecret (which locks the same row via
		// GetSecretForUpdate before its ref-guard check). Without the lock a
		// connector-create can race past an in-flight delete — the delete's
		// guard sees no ref, this plain read sees the still-live secret, the
		// ref is inserted, and the delete commits, leaving the config pointing
		// at a deleted secret. With it, either the delete commits first and
		// this returns ErrNoRows (ref rejected) or this commits first and the
		// guard sees the ref (delete blocked). Deadlocks (two connector-writes
		// locking overlapping secrets) are retried by RunInTx.
		row, err := qtx.GetSecretForUpdate(ctx, secretID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.InvalidArgument(apierr.FieldViolation("connector.config",
					fmt.Sprintf("secret reference %q does not exist", name)))
			}
			return apierr.Internal("resolve secret reference")
		}
		// Same scope check the Secrets service uses: the referenced secret must
		// belong to the connector's org (and space, if space-scoped).
		if row.OrgID != orgID || row.SpaceID != spaceID {
			return apierr.InvalidArgument(apierr.FieldViolation("connector.config",
				fmt.Sprintf("secret reference %q is not in this connector's scope", name)))
		}
		if _, ok := seen[row.ID]; ok {
			continue
		}
		seen[row.ID] = struct{}{}
		ids = append(ids, row.ID)
	}

	// Clear-then-insert: the tracked set always mirrors the current config.
	// On create the DELETE is a no-op; on update it drops refs removed from
	// the config before re-inserting the current ones.
	if err := qtx.DeleteConnectorSecretRefs(ctx, connectorID); err != nil {
		return apierr.Internal("clear secret references")
	}
	if len(ids) > 0 {
		if err := qtx.InsertConnectorSecretRefs(ctx, db.InsertConnectorSecretRefsParams{
			ConnectorID: connectorID,
			SecretIds:   ids,
		}); err != nil {
			return apierr.Internal("track secret references")
		}
	}
	return nil
}
