package connectors_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

func idFromName(t *testing.T, name string) uuid.UUID {
	t.Helper()
	parts := strings.Split(name, "/")
	id, err := uuid.Parse(parts[len(parts)-1])
	require.NoError(t, err, "resource name leaf must be a uuid: %s", name)
	return id
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

	httpCfg := &workflowsv1.HttpConnector{
		BaseUrl: "https://api.example.com",
		Headers: map[string]string{"Authorization": `"Bearer " + secret("org/secrets/tok")`},
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

// TestE2E_Connector_ScopeIsolation pins that a connector's uuid can't be read
// or deleted through a different org's name prefix. The resource-name leaf is
// a global uuid; the interceptor gates on the name's org, so the handler must
// verify the fetched connector actually belongs to that org (else a member of
// org A could reach org B's connector via
// "organizations/A/connectors/{B-uuid}").
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
	created, err := client.CreateConnector(ctx, &workflowsv1.CreateConnectorRequest{
		Parent:      "organizations/iso-b",
		ConnectorId: "b-connector",
		Connector: &workflowsv1.Connector{
			Config: &workflowsv1.Connector_Http{Http: &workflowsv1.HttpConnector{BaseUrl: "https://b.example.com"}},
		},
	})
	require.NoError(t, err)
	bID := idFromName(t, created.GetName())

	// Read iso-b's connector through iso-a's name prefix → NotFound, not leaked.
	crossName := "organizations/iso-a/connectors/" + bID.String()
	_, err = client.GetConnector(ctx, &workflowsv1.GetConnectorRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope read must be NotFound")

	// And delete must not reach across scopes either.
	_, err = client.DeleteConnector(ctx, &workflowsv1.DeleteConnectorRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope delete must be NotFound")
}
