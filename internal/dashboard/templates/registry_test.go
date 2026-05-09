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

package templates_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/dashboard/templates"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// stubTemplate produces a minimally-populated Template good enough
// to exercise registry mechanics. Structural correctness of the
// Widget itself is the concern of resource-specific tests.
func stubTemplate(id string) templates.Template {
	return templates.Template{
		Widget:         &apiv1.Widget{Id: id},
		ListPermission: "test.permission",
	}
}

// uniqueResourceType derives a per-test resource_type so tests
// don't collide with each other (or with stale state from earlier
// runs) on the package-level registry. The tests do NOT use
// t.Parallel() — the registry has no lock by design (Register is
// init()-only in production), so parallel writes from tests would
// race the underlying map. If you ever need parallel-safe tests
// here, expose a test-only constructor that returns an isolated
// Registry instance rather than retrofitting a lock onto the
// package-level state.
func uniqueResourceType(t *testing.T) string {
	t.Helper()
	return "test.fixtures/" + t.Name()
}

func TestRegister_AndGet_RoundTrip(t *testing.T) {
	rt := uniqueResourceType(t)
	templates.Register(rt, stubTemplate("stub"))

	got, ok := templates.Get(rt)
	require.True(t, ok, "Get must find a just-registered template")
	require.NotNil(t, got.Widget)
	assert.Equal(t, "stub", got.Widget.GetId())
	assert.Equal(t, "test.permission", got.ListPermission)
}

func TestGet_Missing_ReturnsFalse(t *testing.T) {
	_, ok := templates.Get("never-registered/" + t.Name())
	assert.False(t, ok)
}

func TestRegister_Duplicate_Panics(t *testing.T) {
	rt := uniqueResourceType(t)
	templates.Register(rt, stubTemplate("first"))

	expected := fmt.Sprintf(
		"dashboard/templates: duplicate registration for resource_type %q",
		rt,
	)
	require.PanicsWithValue(t, expected, func() {
		templates.Register(rt, stubTemplate("second"))
	})

	// First-write-wins: the original Template stays in the registry,
	// duplicates do not silently shadow it.
	got, ok := templates.Get(rt)
	require.True(t, ok)
	assert.Equal(t, "first", got.Widget.GetId())
}

func TestAll_ReturnsDefensiveCopy(t *testing.T) {
	rt := uniqueResourceType(t)
	templates.Register(rt, stubTemplate("snapshot"))

	snapshot := templates.All()
	require.Contains(t, snapshot, rt, "All must include just-registered template")

	// Mutating the returned map must not affect the registry.
	delete(snapshot, rt)

	_, ok := templates.Get(rt)
	assert.True(t, ok, "Get must still find the template after caller mutates All() result")
}
