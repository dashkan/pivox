// Package connector is the single secret-injecting execution path for the
// workflow engine. It holds the ConnectorBroker (which resolves a connector's
// credentialed config and performs the outbound call) and the http Activity
// that drives it. This is the ONLY place the CEL secret() function is live:
// secrets are decrypted here and written straight into the outbound request,
// never into the run context, a step output, or a log.
//
// The security boundary is two structurally separate CEL environments:
//
//   - the run-context env (internal/engine.Evaluator) — {trigger, params,
//     steps, vars}, NO secret(). The http activity evaluates its OWN inputs
//     (path, query, headers, body) there.
//   - the connector-config env (this file) — evaluates a connector's config
//     fields (HttpConnector.headers) and is the ONLY env where secret() exists.
//
// Because the two envs are built by different code with different declarations,
// a secret() call in a run-context expression fails to compile (proved by
// internal/engine's TestEvaluator_SecretFunctionFailsToCompile), and a
// run-context reference in a connector header fails to compile here (the
// connector env declares no trigger/params/steps/vars).
package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine"
)

// Store is the narrow read surface this package needs from the database:
// GetSecret (secret resolution) and GetConnector (connector resolution by the
// activity). db.Querier satisfies it in production; it is deliberately not the
// full Querier so the broker's dependency is exactly the two rows it reads.
type Store interface {
	GetSecret(ctx context.Context, id uuid.UUID) (db.Secret, error)
	GetConnector(ctx context.Context, id uuid.UUID) (db.Connector, error)
}

// db.Querier is the production Store.
var _ Store = db.Querier(nil)

// secretAAD binds a secret's ciphertext to its immutable id. It MUST match the
// AAD the Secrets service encrypts with — see internal/service/secrets.secretAAD:
// the ASCII "secret:" prefix followed by the id's 16 raw bytes. A drift here
// makes every Decrypt fail closed (authenticated-decryption rejects a mismatched
// AAD), so the two definitions are kept identical by this comment.
func secretAAD(id uuid.UUID) []byte {
	return append([]byte("secret:"), id[:]...)
}

// secretResolution captures the first real (classified) error a secret() call
// hit during a connector-config evaluation. The CEL binding can surface only a
// generic ref.Val error, so the true error — terminal (missing/out-of-scope) or
// infra ([engine.Retryable]) — is recorded here and returned by the caller after
// Eval, preserving its retry classification.
type secretResolution struct {
	err error
}

// buildConnectorEnv builds the connector-config CEL env: it declares exactly one
// function, secret(string) string, bound to resolve. No run-context roots are
// declared, so a connector header referencing trigger/params/etc. fails to
// compile. resolve returns the decrypted plaintext or an error; on error the
// binding yields a generic CEL error while resolve's caller records the real one.
func buildConnectorEnv(resolve func(name string) (string, error)) (*cel.Env, error) {
	env, err := cel.NewEnv(
		cel.Function("secret",
			cel.Overload("secret_string_string",
				[]*cel.Type{cel.StringType}, cel.StringType,
				cel.UnaryBinding(func(arg ref.Val) ref.Val {
					name, ok := arg.Value().(string)
					if !ok {
						return types.NewErr("secret() requires a string argument")
					}
					val, err := resolve(name)
					if err != nil {
						// Detail is captured out-of-band; keep the CEL error
						// generic so a plaintext-adjacent value never leaks here.
						return types.NewErr("secret %q could not be resolved", name)
					}
					return types.String(val)
				}),
			),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("connector: building connector-config CEL env: %w", err)
	}
	return env, nil
}

// resolveSecret loads, scope-checks, and decrypts the Secret named by a
// secret("…") reference, returning its plaintext.
//
// Scope: the secret must live in the CONNECTOR's own scope (exact org + space
// match), the same rule the Secrets/Connectors services enforce at write time
// (see internal/service/connectors.trackSecretRefs). The connector's scope — not
// the run's — is authoritative for which secrets it may inject.
//
// Classification: a DB fault is [engine.Retryable] (the whole run job retries); a
// missing ref, a bad ref name, an out-of-scope secret, and a decrypt failure are
// all terminal (they never heal on retry).
func resolveSecret(ctx context.Context, store Store, enc decryptor, name string, connOrg uuid.UUID, connSpace pgtype.UUID) (string, error) {
	id, err := parseSecretLeaf(name)
	if err != nil {
		return "", fmt.Errorf("connector: secret reference %q is not a valid resource name: %w", name, err)
	}

	row, err := store.GetSecret(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("connector: secret %q does not exist", name)
		}
		// DB blip — hand the whole run job back to River rather than fail the run.
		return "", engine.Retryable(fmt.Errorf("connector: resolve secret %q: %w", name, err))
	}

	if row.OrgID != connOrg || row.SpaceID != connSpace {
		return "", fmt.Errorf("connector: secret %q is not in the connector's scope", name)
	}

	plaintext, err := enc.Decrypt(row.ValueCiphertext, secretAAD(id))
	if err != nil {
		// A decrypt failure under the correct AAD is corruption or a key
		// mismatch — it will not heal on retry, so it is terminal rather than a
		// job retry that would loop forever.
		return "", fmt.Errorf("connector: decrypt secret %q: %w", name, err)
	}
	return string(plaintext), nil
}

// parseSecretLeaf extracts the leaf UUID from a Secret resource name
// ("organizations/{org}[/spaces/{space}]/secrets/{uuid}"). Mirrors the secrets
// service's own name parsing.
func parseSecretLeaf(name string) (uuid.UUID, error) {
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
