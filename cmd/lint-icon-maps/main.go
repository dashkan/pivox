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

// Command lint-icon-maps validates that every value in the
// pivox.api.v1.Icon proto enum has a matching `case .X:` in each
// platform-specific SF Symbol / icon map source, and vice versa.
//
// The Icon enum is the contract between server-side widget templates
// (which set `IconConfig.fallback_icon`, `RowAction.icon`, etc.) and
// platform renderers. A missing entry in a platform map ships as a
// silent UX gap — empty thumbnail, "?" placeholder, or worse, a
// crash on a force-unwrapped lookup. Catching drift in CI prevents
// the gap from reaching customers.
//
// Usage:
//
//	lint-icon-maps -swift native/platform/macos/swift/Dashboards/Icons/IconSymbol.swift
//
// Adding a new platform target (e.g. WinUI Fluent map) is an
// additional `-windows` flag here plus the equivalent map source.
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

func main() {
	swiftPath := flag.String(
		"swift",
		"native/platform/macos/swift/Dashboards/Icons/IconSymbol.swift",
		"path to the macOS SF Symbol map source")
	flag.Parse()

	if err := run(*swiftPath); err != nil {
		fmt.Fprintln(os.Stderr, "lint-icon-maps:", err)
		os.Exit(1)
	}
}

func run(swiftPath string) error {
	want := protoIconCases()

	src, err := os.ReadFile(swiftPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", swiftPath, err)
	}
	got := swiftIconCases(string(src))

	missing, extra := diffSets(want, got)
	if len(missing) == 0 && len(extra) == 0 {
		// All good. Quiet success — CI consumers grep for non-zero
		// exit, not for output.
		return nil
	}

	var b strings.Builder
	b.WriteString("Icon enum drift detected — proto + platform map are out of sync.\n\n")
	if len(missing) > 0 {
		fmt.Fprintf(&b, "MISSING in %s (proto values without a matching `case`):\n", swiftPath)
		for _, c := range missing {
			fmt.Fprintf(&b, "  case .%s:\n", c)
		}
		b.WriteString("\n")
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "EXTRA in %s (cases that don't match any proto value):\n", swiftPath)
		for _, c := range extra {
			fmt.Fprintf(&b, "  case .%s:\n", c)
		}
		b.WriteString("\n")
	}
	b.WriteString("Resolve by either (a) adding the missing case to the map,\n")
	b.WriteString("or (b) adding/removing the proto enum value in icons.proto and\n")
	b.WriteString("re-running `make proto-generate`.\n")
	return fmt.Errorf("%s", b.String())
}

// protoIconCases returns the swift-protobuf case-name form of every
// Icon enum value except `ICON_UNSPECIFIED`. Unspecified is excluded
// because it's the zero-value sentinel — renderers fall back to a
// platform default when they encounter it, so requiring an explicit
// case in the map is busywork.
func protoIconCases() []string {
	var out []string
	for protoName, val := range apiv1.Icon_value {
		if val == 0 {
			continue
		}
		out = append(out, swiftCaseFromProtoName(protoName))
	}
	sort.Strings(out)
	return out
}

// swiftCaseFromProtoName converts an Icon enum proto value name
// (UPPER_SNAKE_CASE) into the swift-protobuf case-name form (lower
// camelCase, ICON_ prefix dropped).
//
// Examples (verified against the swift-protobuf-generated
// `Pivox_Api_V1_Icon` enum):
//
//	ICON_DOCUMENT  → "document"
//	ICON_X_MARK    → "xMark"
//	ICON_PHOTO     → "photo"
//	ICON_EXTRA_LARGE → "extraLarge"   // hypothetical multi-word
//
// Latent caveat — acronyms: swift-protobuf preserves runs of capital
// letters in the source name as-is. `ICON_HTTP` would generate
// `case HTTP`, not `case http`; `ICON_API_KEY` generates `case APIKey`,
// not `case apiKey`. The naive lowerCamel mapping below produces
// `http` / `apiKey` and the lint would falsely flag drift. No
// current Icon value exercises this, so the rule for now is "proto
// Icon names must be lower-case-friendly when split on `_` (no all-
// caps acronyms)". When the first acronym Icon shows up, switch this
// function to read `pivox_api_v1_icons.pb.swift` directly so the
// authoritative case names come from the swift-protobuf output.
func swiftCaseFromProtoName(name string) string {
	name = strings.TrimPrefix(name, "ICON_")
	parts := strings.Split(name, "_")
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 {
			b.WriteString(strings.ToLower(p))
			continue
		}
		// Title-case: first char upper, rest lower.
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(strings.ToLower(p[1:]))
	}
	return b.String()
}

// swiftCaseRegex matches `case .<name>:` patterns inside a Swift
// switch. The regex is deliberately narrow: it does not handle
// `case .a, .b:` (comma-separated multi-case) because the Icon map
// is one symbol per case by convention. If that convention ever
// changes, broaden the regex.
var swiftCaseRegex = regexp.MustCompile(`(?m)^\s*case\s+\.([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)

// swiftIconCases extracts every `case .X:` name from the supplied
// Swift source. UNRECOGNIZED (the swift-protobuf wildcard for
// unknown wire values) and `unspecified` (the proto zero-value) are
// filtered: they're noise relative to the drift guard's intent.
func swiftIconCases(src string) []string {
	matches := swiftCaseRegex.FindAllStringSubmatch(src, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := m[1]
		if name == "UNRECOGNIZED" || name == "unspecified" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// diffSets returns (in-a-not-in-b, in-b-not-in-a). Both inputs must
// be sorted; outputs are sorted by construction.
func diffSets(a, b []string) (missing, extra []string) {
	bset := make(map[string]struct{}, len(b))
	for _, x := range b {
		bset[x] = struct{}{}
	}
	aset := make(map[string]struct{}, len(a))
	for _, x := range a {
		aset[x] = struct{}{}
	}
	for _, x := range a {
		if _, ok := bset[x]; !ok {
			missing = append(missing, x)
		}
	}
	for _, x := range b {
		if _, ok := aset[x]; !ok {
			extra = append(extra, x)
		}
	}
	return missing, extra
}
