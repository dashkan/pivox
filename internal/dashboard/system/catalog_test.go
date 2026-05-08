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

package system_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/dashboard/system"
)

func TestCatalog_AllReturnsAtLeastOneEntry(t *testing.T) {
	entries := system.All()
	require.NotEmpty(t, entries,
		"the catalog must list at least one system dashboard once Phase 3 lands the Library entry")
}

func TestCatalog_AllEntriesAreValid(t *testing.T) {
	entries := system.All()

	seen := map[string]bool{}
	for _, e := range entries {
		assert.NotEmpty(t, e.ID, "Entry.ID is required (becomes the trailing resource-name segment)")
		assert.NotNil(t, e.Build, "Entry.Build is required")

		require.False(t, seen[e.ID],
			"duplicate Entry.ID %q — the catalog must enumerate distinct dashboards", e.ID)
		seen[e.ID] = true
	}
}

func TestCatalog_GetMissing(t *testing.T) {
	_, ok := system.Get("never-registered")
	assert.False(t, ok)
}

func TestCatalog_GetLibrary(t *testing.T) {
	e, ok := system.Get("library")
	require.True(t, ok, "Library is the v1 system dashboard; the catalog must include it")

	assert.Equal(t, "library", e.ID)
	require.NotNil(t, e.Build)

	// Smoke-check the Build closure produces something at all —
	// detailed structural checks live in library_test.go.
	d := e.Build("acme")
	require.NotNil(t, d)
	assert.Equal(t, "organizations/acme/dashboards/library", d.GetName())
}

func TestCatalog_AllReturnsDefensiveCopy(t *testing.T) {
	first := system.All()
	require.NotEmpty(t, first)

	// Mutating the returned slice must not affect subsequent calls.
	original := first[0].ID
	first[0].ID = "mutated-id"

	second := system.All()
	require.NotEmpty(t, second)
	assert.Equal(t, original, second[0].ID,
		"All() must return a defensive copy — caller mutations do not bleed into the catalog")
}
