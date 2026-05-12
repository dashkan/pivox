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

package dashboards_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/testutil"
)

// TestSeed_AssetVersionsMatchAssetContentType pins the v1
// asset_versions ↔ assets seed-file invariant: every asset row in
// 13_assets.sql must have exactly one v1 row in 14_asset_versions.sql,
// and that v1 row's mime_type must match the asset's content_type.
//
// Why pin this: the Phase 6c synthesizer's URL composer reads
// `latest_version_number` from the LATERAL latest-version JOIN. A
// future edit to 13_assets.sql (adding a row, changing a content_type)
// without the matching update to 14_asset_versions.sql would silently
// break dev-Library thumbnail rendering — composer would skip rows
// where the JOIN returns the COALESCE sentinel, with no obvious
// signal beyond "no thumbnails for the new row." This test surfaces
// the drift at CI time.
//
// Lives in the dashboards package because that's the consumer that
// breaks if the invariant drifts. The test loads the relevant
// seeds chain (orgs → spaces → storage gateways → assets → versions)
// into a fresh per-test DB via testutil.SetupTestDB and exercises
// the invariant via a real SQL query — no text parsing of seed
// files, so future seed-shape changes (column reordering, NULL vs
// ”, etc.) don't break this test.
func TestSeed_AssetVersionsMatchAssetContentType(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, _ := testutil.SetupTestDB(t)
	ctx := context.Background()

	seedsDir := repoSeedsDir(t)

	// Minimal chain: orgs → spaces → storage gateways → assets →
	// asset_versions. 13_assets.sql FK-references storage_endpoints
	// (rows in 10_storage_gateways.sql) and spaces; loading anything
	// past 14 isn't needed to exercise this invariant.
	for _, name := range []string{
		"01_organizations.sql",
		"04_spaces.sql",
		"10_storage_gateways.sql",
		"13_assets.sql",
		"14_asset_versions.sql",
	} {
		sqlBytes, err := os.ReadFile(filepath.Join(seedsDir, name))
		require.NoError(t, err, "read seed %s", name)
		_, err = pool.Exec(ctx, string(sqlBytes))
		require.NoError(t, err, "exec seed %s", name)
	}

	// Discrepancy 1: asset rows with NO v1 in asset_versions.
	rows, err := pool.Query(ctx, `
		SELECT a.id::text, a.name
		  FROM assets a
		  LEFT JOIN asset_versions av
		    ON av.asset_id = a.id AND av.version_number = 1
		 WHERE av.id IS NULL
		 ORDER BY a.name`)
	require.NoError(t, err)
	var missing []string
	for rows.Next() {
		var id, name string
		require.NoError(t, rows.Scan(&id, &name))
		missing = append(missing, name+" ("+id+")")
	}
	rows.Close()
	require.Empty(t, missing,
		"every active asset in 13_assets.sql must have a v1 row in 14_asset_versions.sql; missing: %v",
		missing)

	// Discrepancy 2: v1 rows whose mime_type doesn't match the asset's
	// content_type. Both columns default to '' so the predicate has to
	// treat the empty-content-type case (legacy-doc) as a match.
	rows, err = pool.Query(ctx, `
		SELECT a.name, a.content_type, av.mime_type
		  FROM assets a
		  JOIN asset_versions av
		    ON av.asset_id = a.id AND av.version_number = 1
		 WHERE av.mime_type IS DISTINCT FROM a.content_type
		 ORDER BY a.name`)
	require.NoError(t, err)
	var mismatches []string
	for rows.Next() {
		var name, ct, mt string
		require.NoError(t, rows.Scan(&name, &ct, &mt))
		mismatches = append(mismatches, name+" (asset.content_type="+ct+", v1.mime_type="+mt+")")
	}
	rows.Close()
	require.Empty(t, mismatches,
		"v1 asset_versions rows must have mime_type matching the asset's content_type; mismatches: %v",
		mismatches)
}

// repoSeedsDir resolves the absolute path to scripts/seeds/ from the
// test file's location via runtime.Caller. Robust to whatever cwd a
// runner picks (go test -run, IDE-launched, etc.) — we don't trust
// `os.Getwd()` to be the package directory.
func repoSeedsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller(0) returned !ok")
	// thisFile = .../internal/service/dashboards/seeds_test.go
	// repo root = thisFile / ../../../..
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	return filepath.Join(repoRoot, "scripts", "seeds")
}
