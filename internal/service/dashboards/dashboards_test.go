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

package dashboards

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseParent(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantKind  scopeKind
		wantOrg   string
		wantSpace string
	}{
		{name: "org parent", input: "organizations/acme", wantKind: scopeOrg, wantOrg: "acme"},
		{name: "space parent", input: "organizations/acme/spaces/dev", wantKind: scopeSpace, wantOrg: "acme", wantSpace: "dev"},
		{name: "empty", input: "", wantKind: scopeMalformed},
		{name: "missing org slug", input: "organizations/", wantKind: scopeMalformed},
		{name: "wrong collection", input: "users/foo", wantKind: scopeMalformed},
		{name: "trailing slash", input: "organizations/acme/", wantKind: scopeMalformed},
		{name: "space without slug", input: "organizations/acme/spaces/", wantKind: scopeMalformed},
		{name: "space without keyword", input: "organizations/acme/foo/dev", wantKind: scopeMalformed},
		{name: "extra segments", input: "organizations/acme/spaces/dev/extra", wantKind: scopeMalformed},
		{name: "dashboard-shaped name (not parent)", input: "organizations/acme/dashboards/library", wantKind: scopeMalformed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, org, space := parseParent(tc.input)
			assert.Equal(t, tc.wantKind, kind)
			assert.Equal(t, tc.wantOrg, org)
			assert.Equal(t, tc.wantSpace, space)
		})
	}
}

func TestParseDashboardName(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantKind  scopeKind
		wantOrg   string
		wantSpace string
		wantID    string
	}{
		{name: "org-scoped", input: "organizations/acme/dashboards/library", wantKind: scopeOrg, wantOrg: "acme", wantID: "library"},
		{name: "space-scoped", input: "organizations/acme/spaces/dev/dashboards/sprint", wantKind: scopeSpace, wantOrg: "acme", wantSpace: "dev", wantID: "sprint"},
		{name: "empty", input: "", wantKind: scopeMalformed},
		{name: "org-only no id", input: "organizations/acme/dashboards/", wantKind: scopeMalformed},
		{name: "wrong sub-collection", input: "organizations/acme/widgets/library", wantKind: scopeMalformed},
		{name: "space path without dashboard", input: "organizations/acme/spaces/dev", wantKind: scopeMalformed},
		{name: "missing space slug", input: "organizations/acme/spaces//dashboards/x", wantKind: scopeMalformed},
		{name: "extra trailing", input: "organizations/acme/dashboards/library/extra", wantKind: scopeMalformed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, org, space, id := parseDashboardName(tc.input)
			assert.Equal(t, tc.wantKind, kind)
			assert.Equal(t, tc.wantOrg, org)
			assert.Equal(t, tc.wantSpace, space)
			assert.Equal(t, tc.wantID, id)
		})
	}
}
