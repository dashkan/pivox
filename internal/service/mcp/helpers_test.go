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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/filter"
)

func TestClampPageSize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   int32
		want int32
	}{
		{"unset defaults to 25", 0, defaultPageSize},
		{"negative defaults to 25", -10, defaultPageSize},
		{"in-range passes through", 50, 50},
		{"at ceiling passes through", 100, 100},
		{"above ceiling clamps to 100", 1000, maxPageSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, clampPageSize(tc.in))
		})
	}
}

func TestNamePrefixFilter(t *testing.T) {
	t.Parallel()

	// Empty prefix means "no filter".
	assert.Empty(t, namePrefixFilter(""))

	// A prefix becomes a partial-match filter expression whose value is
	// strconv.Quote'd, so the trailing `*` is the engine's ILIKE wildcard
	// and metacharacters in the input can't break the filter grammar.
	cases := map[string]string{
		"prod": `displayName = "prod*"`,
		`a"b`:  `displayName = "a\"b*"`, // double quote → escaped, grammar intact
		`a\b`:  `displayName = "a\\b*"`, // backslash → escaped
		`p*d`:  `displayName = "p*d*"`,  // literal * carried into the value
	}
	for in, want := range cases {
		assert.Equal(t, want, namePrefixFilter(in), "namePrefixFilter(%q)", in)
	}
}

// TestNamePrefixFilter_ParsesThroughEngine proves the escaped expression
// is accepted by the real filter transpiler for metacharacter inputs —
// i.e. quoting keeps the grammar well-formed rather than producing a
// string the engine rejects (which would surface as InvalidArgument to
// the caller). Values are bound params, so this is grammar-safety, not
// SQLi.
func TestNamePrefixFilter_ParsesThroughEngine(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"prod", `a"b`, `a\b`, `p*d`, `'; DROP`} {
		expr := namePrefixFilter(in)
		_, err := filter.Transpile(filter.SpaceFilter(), expr, 1)
		require.NoErrorf(t, err, "engine must parse escaped filter %q for input %q", expr, in)
	}
}
