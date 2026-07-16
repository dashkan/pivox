package connectors_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	secretsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/secrets/v1"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// secretRefHeader builds an HttpConnector header value that references a
// vault Secret by resource name via a literal secret("…") CEL call — the
// shape the ref extractor statically scans for.
func secretRefHeader(secretName string) string {
	return `"Bearer " + secret("` + secretName + `")`
}

// TestE2E_Connector_CRUD covers the create→get→list→update→delete happy path
// and pins that the typed `oneof config` (an HttpConnector with base_url +
// headers) round-trips through the JSONB column unchanged.
func TestE2E_Connector_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "conn", "Conn Co", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	// A CEL header value with no secret("…") reference — this test pins config
	// round-tripping, not ref tracking (which resolves refs against the vault
	// and would reject a dangling name). Secret-ref behavior is covered by the
	// TestE2E_Connector_SecretRef_* suite below.
	httpCfg := &workflowsv1.HttpConnector{
		BaseUrl: "https://api.example.com",
		Headers: map[string]string{"X-Env": `"prod"`},
	}
	created, err := client.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/" + owned.Slug,
		ConnectorId: "vizrt-hub",
		Connector: &workflowsv1.Connector{
			DisplayName: "VizRT Hub",
			Description: "HTTP into the VizRT hub",
			Agent:       "",
			Config:      &workflowsv1.Connector_Http{Http: httpCfg},
			Annotations: map[string]string{"team": "playout"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "VizRT Hub", created.GetDisplayName())
	assert.Equal(t, "HTTP into the VizRT hub", created.GetDescription())
	// The resource name's leaf is the user-assigned slug, not the internal uuid.
	assert.Equal(t, "organizations/"+owned.Slug+"/connectors/vizrt-hub", created.GetName())
	require.NotEmpty(t, created.GetName())
	require.NotEmpty(t, created.GetEtag())
	assert.Equal(t, map[string]string{"team": "playout"}, created.GetAnnotations())
	// The oneof config round-trips through JSONB.
	require.NotNil(t, created.GetHttp())
	assert.Equal(t, "https://api.example.com", created.GetHttp().GetBaseUrl())
	assert.Equal(t, httpCfg.GetHeaders(), created.GetHttp().GetHeaders())

	// Get returns the same resource, config intact.
	fetched, err := client.GetConnector(ctx, &workflowsv1.GetConnectorRequest{Name: created.GetName()})
	require.NoError(t, err)
	assert.Equal(t, created.GetName(), fetched.GetName())
	require.NotNil(t, fetched.GetHttp())
	assert.Equal(t, "https://api.example.com", fetched.GetHttp().GetBaseUrl())
	assert.Equal(t, httpCfg.GetHeaders(), fetched.GetHttp().GetHeaders())

	// List surfaces it under the parent.
	listed, err := client.ListConnectors(ctx, &workflowsv1.ListConnectorsRequest{
		Parent: "organizations/" + owned.Slug,
	})
	require.NoError(t, err)
	require.Len(t, listed.GetConnectors(), 1)
	assert.Equal(t, created.GetName(), listed.GetConnectors()[0].GetName())

	// Update rotates the base_url and display_name; etag changes.
	updated, err := client.UpdateConnector(ctx, &workflowsv1.UpdateConnectorRequest{
		Connector: &workflowsv1.Connector{
			Name:        created.GetName(),
			DisplayName: "VizRT Hub (prod)",
			Config:      &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{BaseUrl: "https://prod.example.com"}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "VizRT Hub (prod)", updated.GetDisplayName())
	assert.Equal(t, "https://prod.example.com", updated.GetHttp().GetBaseUrl())
	assert.NotEqual(t, created.GetEtag(), updated.GetEtag())

	// Delete removes it.
	_, err = client.DeleteConnector(ctx, &workflowsv1.DeleteConnectorRequest{Name: created.GetName()})
	require.NoError(t, err)
	_, err = client.GetConnector(ctx, &workflowsv1.GetConnectorRequest{Name: created.GetName()})
	require.Error(t, err, "deleted connector must not be gettable")
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestE2E_Connector_ValidateOnly pins the AIP validate_only contract: the
// request runs through the same validation a live one would (so a would-fail
// request still fails) but persists nothing.
func TestE2E_Connector_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "conn-vo", "Conn VO", "connectors")
	ctx := context.Background()
	client := workflowsv1.NewConnectorsClient(h.Conn())

	// A dry-run Create returns the would-be resource but writes nothing.
	dry, err := client.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:       "organizations/" + owned.Slug,
		ConnectorId:  "dry",
		ValidateOnly: true,
		Connector: &workflowsv1.Connector{
			DisplayName: "Dry",
			Config:      &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{BaseUrl: "https://x.example.com"}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "Dry", dry.GetDisplayName())

	// Nothing persisted → a real Create can reuse the same connector_id.
	_, err = client.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/" + owned.Slug,
		ConnectorId: "dry",
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{BaseUrl: "https://x.example.com"}},
		},
	})
	require.NoError(t, err, "validate_only must not have persisted the connector")

	// A dry-run that WOULD fail live (duplicate connector_id now exists) fails.
	_, err = client.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:       "organizations/" + owned.Slug,
		ConnectorId:  "dry",
		ValidateOnly: true,
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{BaseUrl: "https://x.example.com"}},
		},
	})
	require.Error(t, err, "validate_only must fail if the live request would")
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

// TestE2E_Connector_ScopeIsolation pins that a connector can't be read or
// deleted through a different org's name prefix. The resource-name leaf is the
// slug, unique only within its parent; the handler resolves it by (org, space,
// slug), so naming another org's connector under this org's prefix finds no row
// (NotFound) rather than leaking cross-scope existence.
func TestE2E_Connector_ScopeIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithConnectorsServer())
	// One owner owns both orgs.
	h.SeedOwnedOrg(t, "iso-a", "Iso A", "iso")
	ctx := context.Background()

	op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx,
		&apiv1.CreateOrganizationRequest{
			OrganizationId: "iso-b",
			Organization:   &apiv1.Organization{DisplayName: "Iso B"},
		})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	client := workflowsv1.NewConnectorsClient(h.Conn())
	_, err = client.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/iso-b",
		ConnectorId: "b-connector",
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{BaseUrl: "https://b.example.com"}},
		},
	})
	require.NoError(t, err)

	// Read iso-b's connector through iso-a's name prefix → NotFound, not leaked.
	// (The leaf is the slug; iso-a has no connector by that slug, so the scoped
	// lookup finds nothing — cross-scope existence never leaks.)
	crossName := "organizations/iso-a/connectors/b-connector"
	_, err = client.GetConnector(ctx, &workflowsv1.GetConnectorRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope read must be NotFound")

	// And delete must not reach across scopes either.
	_, err = client.DeleteConnector(ctx, &workflowsv1.DeleteConnectorRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope delete must be NotFound")
}

// countSecretRefs returns how many connector_secret_refs rows link the named
// connector and secret (both by slug, within the org named orgSlug) — used to
// assert a config's secret("…") reference was tracked in the same tx that wrote
// the connector.
func countSecretRefs(t *testing.T, h *grpcharness.Harness, orgSlug, connSlug, secretSlug string) int {
	t.Helper()
	var n int
	require.NoError(t, h.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM connector_secret_refs r
		 JOIN connectors c ON c.id = r.connector_id
		 JOIN secrets s ON s.id = r.secret_id
		 JOIN organizations o ON o.id = c.org_id
		 WHERE o.name = $1 AND c.slug = $2 AND s.slug = $3`,
		orgSlug, connSlug, secretSlug).Scan(&n))
	return n
}

// TestE2E_Connector_SecretRef_TrackedAndGuarded is the Phase-3b core loop: a
// connector's config references a vault Secret via secret("…"); the ref is
// tracked on write, DeleteSecret is then blocked (FailedPrecondition) while
// the connector references it, and unblocks once the connector is deleted.
func TestE2E_Connector_SecretRef_TrackedAndGuarded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSecretsServer(),
		grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "ref-track", "Ref Co", "connectors")
	ctx := context.Background()
	secretsClient := secretsv1.NewSecretsClient(h.Conn())
	connClient := workflowsv1.NewConnectorsClient(h.Conn())

	secret, err := secretsClient.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/" + owned.Slug,
		SecretId: "hub-token",
		Secret:   &secretsv1.Secret{Value: []byte("s3cr3t")},
	})
	require.NoError(t, err)

	created, err := connClient.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/" + owned.Slug,
		ConnectorId: "vizrt-hub",
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
				BaseUrl: "https://api.example.com",
				Headers: map[string]string{"Authorization": secretRefHeader(secret.GetName())},
			}},
		},
	})
	require.NoError(t, err)

	// The ref was tracked in the same tx as the connector write.
	assert.Equal(t, 1, countSecretRefs(t, h, owned.Slug, "vizrt-hub", "hub-token"))

	// The secret is now referenced → DeleteSecret is blocked.
	_, err = secretsClient.DeleteSecret(ctx, &secretsv1.DeleteSecretRequest{Name: secret.GetName()})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err),
		"deleting a referenced secret must be FailedPrecondition")
	assert.Contains(t, status.Convert(err).Message(), "vizrt-hub",
		"the guard error should name the referencing connector")

	// Drop the connector → the ref cascades away → DeleteSecret succeeds.
	_, err = connClient.DeleteConnector(ctx, &workflowsv1.DeleteConnectorRequest{Name: created.GetName()})
	require.NoError(t, err)
	_, err = secretsClient.DeleteSecret(ctx, &secretsv1.DeleteSecretRequest{Name: secret.GetName()})
	require.NoError(t, err, "with no connector referencing it, the secret is deletable")
}

// TestE2E_Connector_SecretRef_Nonexistent pins that saving a connector whose
// config references a secret that doesn't exist is a client error — you can't
// point a connector at a secret that isn't there.
func TestE2E_Connector_SecretRef_Nonexistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSecretsServer(),
		grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "ref-missing", "Ref Missing", "connectors")
	ctx := context.Background()
	connClient := workflowsv1.NewConnectorsClient(h.Conn())

	// A well-formed name whose slug leaf resolves to no secret.
	danglingName := "organizations/" + owned.Slug + "/secrets/" + uuid.New().String()
	_, err := connClient.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/" + owned.Slug,
		ConnectorId: "dangling",
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
				BaseUrl: "https://api.example.com",
				Headers: map[string]string{"Authorization": secretRefHeader(danglingName)},
			}},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	// And a bare ref with no collection path (no "/") is likewise rejected as a
	// malformed Secret resource name.
	_, err = connClient.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/" + owned.Slug,
		ConnectorId: "malformed",
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
				BaseUrl: "https://api.example.com",
				Headers: map[string]string{"Authorization": secretRefHeader("bare-name")},
			}},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestE2E_Connector_SecretRef_WrongScopePrefixRejected pins that the ref name's
// PARENT segments are validated against the connector's scope — a ref written as
// organizations/OTHER/secrets/foo must NOT silently rebind to the connector's
// own local "foo". Without parent validation the leaf slug would resolve in the
// connector's scope, quietly pointing the config at a different secret than the
// name says.
func TestE2E_Connector_SecretRef_WrongScopePrefixRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSecretsServer(),
		grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "ref-scope", "Ref Scope", "connectors")
	ctx := context.Background()
	secretsClient := secretsv1.NewSecretsClient(h.Conn())
	connClient := workflowsv1.NewConnectorsClient(h.Conn())

	// A real secret "foo" exists in the connector's OWN org.
	_, err := secretsClient.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/" + owned.Slug,
		SecretId: "foo",
		Secret:   &secretsv1.Secret{Value: []byte("v")},
	})
	require.NoError(t, err)

	// The connector references "foo" under a DIFFERENT org prefix. The leaf slug
	// matches the local secret, but the parent scope does not — it must be
	// rejected, not silently rebound to the local "foo".
	wrongScopeRef := "organizations/some-other-org/secrets/foo"
	_, err = connClient.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/" + owned.Slug,
		ConnectorId: "c",
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
				BaseUrl: "https://api.example.com",
				Headers: map[string]string{"Authorization": secretRefHeader(wrongScopeRef)},
			}},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"a ref whose parent scope differs from the connector's must be rejected")
}

// TestE2E_Connector_SecretRef_CrossScope pins that a connector can't reference
// a secret in a different org — the ref is resolved (by slug) against the
// connector's own scope, so another org's secret is simply not found there.
func TestE2E_Connector_SecretRef_CrossScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSecretsServer(),
		grpcharness.WithConnectorsServer())
	// One owner owns both orgs.
	h.SeedOwnedOrg(t, "xs-a", "XS A", "iso")
	ctx := context.Background()

	op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx,
		&apiv1.CreateOrganizationRequest{
			OrganizationId: "xs-b",
			Organization:   &apiv1.Organization{DisplayName: "XS B"},
		})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	secretsClient := secretsv1.NewSecretsClient(h.Conn())
	_, err = secretsClient.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/xs-b",
		SecretId: "b-secret",
		Secret:   &secretsv1.Secret{Value: []byte("b")},
	})
	require.NoError(t, err)

	// Connector in org A referencing org B's secret by name under A's prefix.
	// The slug is resolved in A's scope, where "b-secret" doesn't exist.
	crossName := "organizations/xs-a/secrets/b-secret"
	_, err = workflowsv1.NewConnectorsClient(h.Conn()).CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/xs-a",
		ConnectorId: "cross",
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
				BaseUrl: "https://api.example.com",
				Headers: map[string]string{"Authorization": secretRefHeader(crossName)},
			}},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"referencing a secret outside the connector's scope must be InvalidArgument")
}

// TestE2E_Connector_SecretRef_UpdateRetracks pins that a config update
// re-derives the tracked refs: an update pointing at a missing secret is
// rejected, and an update that drops the reference frees the secret to be
// deleted.
func TestE2E_Connector_SecretRef_UpdateRetracks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSecretsServer(),
		grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "ref-upd", "Ref Upd", "connectors")
	ctx := context.Background()
	secretsClient := secretsv1.NewSecretsClient(h.Conn())
	connClient := workflowsv1.NewConnectorsClient(h.Conn())

	secret, err := secretsClient.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/" + owned.Slug,
		SecretId: "tok",
		Secret:   &secretsv1.Secret{Value: []byte("v")},
	})
	require.NoError(t, err)

	created, err := connClient.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/" + owned.Slug,
		ConnectorId: "c",
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
				BaseUrl: "https://api.example.com",
				Headers: map[string]string{"Authorization": secretRefHeader(secret.GetName())},
			}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, countSecretRefs(t, h, owned.Slug, "c", "tok"))

	// A config update pointing at a missing secret is rejected (and the tx
	// rolls back, so the existing ref survives).
	danglingName := "organizations/" + owned.Slug + "/secrets/" + uuid.New().String()
	_, err = connClient.UpdateConnector(ctx, &workflowsv1.UpdateConnectorRequest{
		Connector: &workflowsv1.Connector{
			Name: created.GetName(),
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
				BaseUrl: "https://api.example.com",
				Headers: map[string]string{"Authorization": secretRefHeader(danglingName)},
			}},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, 1, countSecretRefs(t, h, owned.Slug, "c", "tok"), "failed update must not drop the ref")

	// An update that drops the reference (no secret in the new config) frees
	// the secret to be deleted.
	_, err = connClient.UpdateConnector(ctx, &workflowsv1.UpdateConnectorRequest{
		Connector: &workflowsv1.Connector{
			Name: created.GetName(),
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
				BaseUrl: "https://api.example.com",
				Headers: map[string]string{"X-Env": `"prod"`},
			}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, countSecretRefs(t, h, owned.Slug, "c", "tok"), "update dropping the ref must clear it")

	_, err = secretsClient.DeleteSecret(ctx, &secretsv1.DeleteSecretRequest{Name: secret.GetName()})
	require.NoError(t, err, "with the ref dropped, the secret is deletable")
}

// TestE2E_Connector_SecretRef_ValidateOnly pins that a validate_only create
// referencing a valid secret persists nothing — neither the connector nor its
// ref — so the secret remains deletable.
func TestE2E_Connector_SecretRef_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSecretsServer(),
		grpcharness.WithConnectorsServer())
	owned := h.SeedOwnedOrg(t, "ref-vo", "Ref VO", "connectors")
	ctx := context.Background()
	secretsClient := secretsv1.NewSecretsClient(h.Conn())
	connClient := workflowsv1.NewConnectorsClient(h.Conn())

	secret, err := secretsClient.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/" + owned.Slug,
		SecretId: "tok",
		Secret:   &secretsv1.Secret{Value: []byte("v")},
	})
	require.NoError(t, err)

	dry, err := connClient.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:       "organizations/" + owned.Slug,
		ConnectorId:  "dry",
		ValidateOnly: true,
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{
				BaseUrl: "https://api.example.com",
				Headers: map[string]string{"Authorization": secretRefHeader(secret.GetName())},
			}},
		},
	})
	require.NoError(t, err, "validate_only with a valid ref must pass validation")
	require.NotEmpty(t, dry.GetName())

	// Nothing persisted → the secret has no live reference and is deletable.
	_, err = secretsClient.DeleteSecret(ctx, &secretsv1.DeleteSecretRequest{Name: secret.GetName()})
	require.NoError(t, err, "validate_only must not have persisted the ref")
}
