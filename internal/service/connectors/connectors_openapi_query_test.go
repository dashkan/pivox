package connectors_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestListConnectorsOpenAPI_AggsIsQueryParam is the transport guard for the
// List-tier `aggs` faceting contract. ListConnectors is an AIP-132 GET, and the
// web app reaches it ONLY over REST through grpc-gateway. grpc-gateway can bind
// a `repeated string` to URL query params but CANNOT bind a `repeated message`
// — so a message-typed `aggs` is silently DROPPED from the generated GET spec
// and is unreachable over the only transport the frontend uses. The prior
// bufconn e2e set `aggs` on the request struct in-process, so it never
// exercised query-param binding and stayed green while REST was broken.
//
// This test asserts, against the GENERATED OpenAPI v3 document, that
// ListConnectors' GET carries `aggs` as an `in: query` array-of-string
// parameter. It fails the moment `aggs` regresses to a non-bindable shape.
//
// No grpc-gateway HTTP harness exists in the repo (grpcharness dials the gRPC
// server over bufconn), so this generated-spec assertion is the strongest
// available guard for query-param reachability (option (b)).
func TestListConnectorsOpenAPI_AggsIsQueryParam(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPIV3(t)

	// The primary org-level binding: GET /v1/organizations/{organization}/connectors.
	op := getOperation(t, spec, "/v1/organizations/{organization}/connectors", "get")
	require.Equal(t, "Connectors_ListConnectors", op["operationId"],
		"located the wrong operation")

	agg := findParam(op, "aggs")
	require.NotNil(t, agg, "aggs must be a parameter on the ListConnectors GET; "+
		"a repeated-message aggs is dropped by grpc-gateway and never reaches REST")
	assert.Equal(t, "query", agg["in"], "aggs must bind from the URL query string")

	schema, ok := agg["schema"].(map[string]any)
	require.True(t, ok, "aggs parameter needs a schema")
	assert.Equal(t, "array", schema["type"], "aggs is a repeated field → array")
	items, ok := schema["items"].(map[string]any)
	require.True(t, ok, "aggs array needs an items schema")
	assert.Equal(t, "string", items["type"], "each aggs element is a string (field[:size])")
}

// loadOpenAPIV3 parses the generated api/openapi/v3/pivox.yaml into a generic
// map. Path is resolved from this test file's location, so cwd is irrelevant.
func loadOpenAPIV3(t *testing.T) map[string]any {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller(0) returned !ok")
	// thisFile = .../internal/service/connectors/connectors_openapi_query_test.go
	// repo root = thisFile / ../../../..
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	path := filepath.Join(repoRoot, "api", "openapi", "v3", "pivox.yaml")

	raw, err := os.ReadFile(path)
	require.NoError(t, err, "read generated OpenAPI v3 spec")

	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &spec), "parse OpenAPI v3 YAML")
	return spec
}

// getOperation returns the given HTTP method's operation object under paths[path].
func getOperation(t *testing.T, spec map[string]any, path, method string) map[string]any {
	t.Helper()
	paths, ok := spec["paths"].(map[string]any)
	require.True(t, ok, "spec has no paths")
	item, ok := paths[path].(map[string]any)
	require.Truef(t, ok, "spec has no path %q", path)
	op, ok := item[method].(map[string]any)
	require.Truef(t, ok, "path %q has no %s operation", path, method)
	return op
}

// findParam returns the named parameter object from an operation, or nil.
func findParam(op map[string]any, name string) map[string]any {
	params, ok := op["parameters"].([]any)
	if !ok {
		return nil
	}
	for _, p := range params {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if pm["name"] == name {
			return pm
		}
	}
	return nil
}
