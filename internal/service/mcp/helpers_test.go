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

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

// TestNamePrefixPattern pins the ILIKE pattern the static ListSpacesForMCP
// query receives. An empty prefix yields a NULL text (the query treats NULL
// as "no filter"); a non-empty prefix yields a case-insensitive prefix
// pattern with LIKE metacharacters neutralized. The mapping is preserved
// byte-for-byte from the removed filter-engine transpiler, so behaviour is
// unchanged across the migration.
func TestNamePrefixPattern(t *testing.T) {
	t.Parallel()

	// Empty prefix means "no filter" — a NULL text bound parameter.
	assert.Equal(t, pgtype.Text{}, namePrefixPattern(""))

	cases := map[string]string{
		"prod": `prod%`, // plain prefix + trailing wildcard
		`a%b`:  `a\%b%`, // '%' escaped so it matches literally
		`a_b`:  `a\_b%`, // '_' escaped so it matches literally
		`p*d`:  `p%d%`,  // AIP-160 '*' carried through as a wildcard
	}
	for in, want := range cases {
		got := namePrefixPattern(in)
		assert.True(t, got.Valid, "namePrefixPattern(%q) must be non-NULL", in)
		assert.Equal(t, want, got.String, "namePrefixPattern(%q)", in)
	}
}
