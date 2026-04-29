package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/permission"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	iamv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
)

// gatedServiceDescs mirrors cmd/gen-permission-registry's
// gatedServices set — every Pivox service whose RPCs the
// permission interceptor governs. The drift-guard test below
// asserts each service's RPCs are fully covered by the union of
// GeneratedRegistry and GeneratedExempt. Adding a new service to
// the gen-permission-registry set requires adding it here too.
var gatedServiceDescs = []*grpc.ServiceDesc{
	&apiv1.Organizations_ServiceDesc,
	&apiv1.Spaces_ServiceDesc,
	&apiv1.TagKeys_ServiceDesc,
	&apiv1.TagValues_ServiceDesc,
	&apiv1.TagBindings_ServiceDesc,
	&apiv1.ApiKeys_ServiceDesc,
	&iamv1.Iam_ServiceDesc,
	&assetsv1.Assets_ServiceDesc,
	&assetsv1.Requests_ServiceDesc,
	&storagev1.StorageGateways_ServiceDesc,
	&storagev1.Agents_ServiceDesc,
	&storagev1.Endpoints_ServiceDesc,
	&aiv1.AiChat_ServiceDesc,
}

// TestGeneratedRegistry_CoversEveryGatedRPC is the build-time
// drift guard: every RPC declared on every gated service must be
// present in either GeneratedRegistry or GeneratedExempt. Without
// this test, a developer could land a proto annotation without
// re-running the generator and the new RPC would silently
// default-deny in production until someone re-generates.
func TestGeneratedRegistry_CoversEveryGatedRPC(t *testing.T) {
	for _, desc := range gatedServiceDescs {
		t.Run(desc.ServiceName, func(t *testing.T) {
			uncovered := AssertRegistryCoversService(desc, GeneratedRegistry, GeneratedExempt)
			assert.Empty(t, uncovered,
				"every RPC on %s must be in GeneratedRegistry or GeneratedExempt; these are unwired (run `go generate ./internal/server/...`): %v",
				desc.ServiceName, uncovered)
		})
	}
}

// TestGeneratedRegistry_PermissionsExist is the drift guard
// against permission-ID typos in proto annotations. A typo like
// `organizations.update_typo` would slip through the generator's
// snake→camel transformation, emit a Go constant that doesn't
// exist, and fail the build only at the point of regen — which
// is too late if the proto change landed in a different commit.
// This test asserts every entry's permission ID is in
// permission.All so a typo fails CI immediately, not when someone
// touches the generator.
func TestGeneratedRegistry_PermissionsExist(t *testing.T) {
	known := make(map[string]bool, len(permission.All))
	for _, p := range permission.All {
		known[p] = true
	}
	for fullMethod, entry := range GeneratedRegistry {
		t.Run(fullMethod, func(t *testing.T) {
			assert.Truef(t, known[entry.Permission],
				"permission %q referenced by %s is not in permission.All; check the proto annotation for typos",
				entry.Permission, fullMethod)
		})
	}
}
