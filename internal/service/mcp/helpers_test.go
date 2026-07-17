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

// TestNamePrefixPattern pins the ESCAPE '\' pattern the static
// ListSpacesForMCP query receives. An empty prefix yields a NULL text (the
// query treats NULL as "no filter"); a non-empty prefix is treated as a PURE
// LITERAL prefix — every LIKE metacharacter in the caller's input ('\', '%',
// '_') is escaped so it matches literally, and '*' is an ordinary character.
// The only wildcard is the implicit trailing '%' that anchors the prefix
// match. Backslash is escaped FIRST so the escapes we add aren't re-escaped.
func TestNamePrefixPattern(t *testing.T) {
	t.Parallel()

	// Empty prefix means "no filter" — a NULL text bound parameter.
	assert.Equal(t, pgtype.Text{}, namePrefixPattern(""))

	cases := map[string]string{
		"prod": `prod%`,   // plain prefix + implicit trailing wildcard
		`a%b`:  `a\%b%`,   // '%' escaped so it matches literally
		`a_b`:  `a\_b%`,   // '_' escaped so it matches literally
		`p*d`:  `p*d%`,    // '*' is a LITERAL, not a wildcard
		`a\b`:  `a\\b%`,   // '\' escaped so it matches literally
		`50%`:  `50\%%`,   // trailing literal '%' escaped; implicit '%' appended
		`a\%b`: `a\\\%b%`, // '\' escaped first, then the literal '%' escaped
	}
	for in, want := range cases {
		got := namePrefixPattern(in)
		assert.True(t, got.Valid, "namePrefixPattern(%q) must be non-NULL", in)
		assert.Equal(t, want, got.String, "namePrefixPattern(%q)", in)
	}
}
