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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwiftCaseFromProtoName(t *testing.T) {
	cases := []struct {
		proto string
		want  string
	}{
		// Single-word, ICON_ stripped + lowercased.
		{"ICON_DOCUMENT", "document"},
		{"ICON_PHOTO", "photo"},
		{"ICON_PLUS", "plus"},
		// Multi-word, lowerCamelCase.
		{"ICON_X_MARK", "xMark"},
		{"ICON_EXTRA_LARGE", "extraLarge"},
		// Long compound (hypothetical) — verifies the loop's
		// title-casing of every part after the first.
		{"ICON_FOO_BAR_BAZ", "fooBarBaz"},
		// No ICON_ prefix — a name that doesn't match the convention
		// shouldn't crash; lowercasing is fine.
		{"DOCUMENT", "document"},
	}
	for _, tc := range cases {
		t.Run(tc.proto, func(t *testing.T) {
			assert.Equal(t, tc.want, swiftCaseFromProtoName(tc.proto))
		})
	}
}

func TestSwiftIconCases_HappyPath(t *testing.T) {
	src := `
extension Pivox_Api_V1_Icon {
    var sfSymbol: String {
        switch self {
        case .unspecified: return ""
        case .document: return "doc"
        case .photo: return "photo"
        case .xMark: return "xmark"
        case .UNRECOGNIZED: return ""
        }
    }
}
`
	got := swiftIconCases(src)
	// unspecified + UNRECOGNIZED filtered out.
	assert.Equal(t, []string{"document", "photo", "xMark"}, got)
}

func TestSwiftIconCases_IgnoresLeadingWhitespace(t *testing.T) {
	src := "        case .document: return \"doc\"\n"
	assert.Equal(t, []string{"document"}, swiftIconCases(src))
}

func TestSwiftIconCases_NoMatches(t *testing.T) {
	src := "// just a comment, no cases here"
	assert.Empty(t, swiftIconCases(src))
}

func TestDiffSets(t *testing.T) {
	cases := []struct {
		name        string
		a, b        []string
		wantMissing []string
		wantExtra   []string
	}{
		{
			name:        "identical",
			a:           []string{"document", "photo"},
			b:           []string{"document", "photo"},
			wantMissing: nil,
			wantExtra:   nil,
		},
		{
			name:        "missing in b",
			a:           []string{"document", "photo", "video"},
			b:           []string{"document"},
			wantMissing: []string{"photo", "video"},
			wantExtra:   nil,
		},
		{
			name:        "extra in b",
			a:           []string{"document"},
			b:           []string{"document", "photo", "video"},
			wantMissing: nil,
			wantExtra:   []string{"photo", "video"},
		},
		{
			name:        "both directions",
			a:           []string{"document", "photo"},
			b:           []string{"document", "video"},
			wantMissing: []string{"photo"},
			wantExtra:   []string{"video"},
		},
		{
			name:        "both empty",
			a:           nil,
			b:           nil,
			wantMissing: nil,
			wantExtra:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			missing, extra := diffSets(tc.a, tc.b)
			assert.Equal(t, tc.wantMissing, missing)
			assert.Equal(t, tc.wantExtra, extra)
		})
	}
}

// TestRun_ProductionMapMatchesProto is the production drift guard.
// It runs the lint against the actual Swift map file shipped in
// this repo. A failure here means the proto + Swift map are out
// of sync — fix one or the other and re-run.
func TestRun_ProductionMapMatchesProto(t *testing.T) {
	// The lint-icon-maps test runs from `cmd/lint-icon-maps/` —
	// resolve the Swift path relative to the repo root.
	const swiftPath = "../../native/platform/macos/swift/Dashboards/Icons/IconSymbol.swift"
	require.NoError(t, run(swiftPath),
		"production Icon enum and Swift SF Symbol map are out of sync")
}
