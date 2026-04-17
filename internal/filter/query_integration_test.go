//go:build dev

package filter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func createTestOrg(t *testing.T, queries *db.Queries, suffix string) db.Organization {
	t.Helper()
	id := uuid.New()
	org, err := queries.CreateOrganization(context.Background(), db.CreateOrganizationParams{
		ID:          id,
		Name:        "organizations/" + id.String()[:8] + "-" + suffix,
		DisplayName: "Org " + suffix,
		CreatedBy:   "test",
	})
	require.NoError(t, err)
	return org
}

func createTestProject(t *testing.T, queries *db.Queries, orgID uuid.UUID, displayName string) db.Project {
	t.Helper()
	id := uuid.New()
	p, err := queries.CreateProject(context.Background(), db.CreateProjectParams{
		ID:          id,
		OrgID:       orgID,
		Name:        "projects/" + id.String()[:8],
		DisplayName: displayName,
		Labels:      json.RawMessage("{}"),
		CreatedBy:   "test",
	})
	require.NoError(t, err)
	return p
}

func createTestTagKey(t *testing.T, queries *db.Queries, orgID uuid.UUID, shortName string) db.TagKey {
	t.Helper()
	id := uuid.New()
	tk, err := queries.CreateTagKey(context.Background(), db.CreateTagKeyParams{
		ID:             id,
		OrgID:          orgID,
		ShortName:      shortName,
		NamespacedName: orgID.String() + "/" + shortName,
		Description:    "tag key " + shortName,
		CreatedBy:      "test",
	})
	require.NoError(t, err)
	return tk
}

func createTestTagValue(t *testing.T, queries *db.Queries, tagKeyID uuid.UUID, shortName, nsPrefix string) db.TagValue {
	t.Helper()
	id := uuid.New()
	tv, err := queries.CreateTagValue(context.Background(), db.CreateTagValueParams{
		ID:             id,
		TagKeyID:       tagKeyID,
		ShortName:      shortName,
		NamespacedName: nsPrefix + "/" + shortName,
		Description:    "tag value " + shortName,
		CreatedBy:      "test",
	})
	require.NoError(t, err)
	return tv
}

func createTestTagBinding(t *testing.T, queries *db.Queries, parentResource string, tagValueID uuid.UUID) db.TagBinding {
	t.Helper()
	tb, err := queries.CreateTagBinding(context.Background(), db.CreateTagBindingParams{
		ID:             uuid.New(),
		ParentResource: parentResource,
		TagValueID:     tagValueID,
		CreatedBy:      "test",
	})
	require.NoError(t, err)
	return tb
}

func createTestApiKey(t *testing.T, queries *db.Queries, orgID uuid.UUID, displayName string) db.ApiKey {
	t.Helper()
	id := uuid.New()
	k, err := queries.CreateApiKey(context.Background(), db.CreateApiKeyParams{
		ID:           id,
		OrgID:        orgID,
		KeyID:        uuid.New().String()[:8],
		DisplayName:  displayName,
		KeyString:    uuid.New().String(),
		Annotations:  json.RawMessage("{}"),
		Restrictions: nil,
		CreatedBy:    "test",
	})
	require.NoError(t, err)
	return k
}

// ---------------------------------------------------------------------------
// Query + ScanOrganizations
// ---------------------------------------------------------------------------

func TestQueryIntegration_Organizations_NoFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org1 := createTestOrg(t, queries, uuid.New().String()[:8])
	org2 := createTestOrg(t, queries, uuid.New().String()[:8])

	rf := OrganizationFilter()
	rows, err := Query(ctx, pool, rf, QueryParams{})
	require.NoError(t, err)

	orgs, err := ScanOrganizations(rows)
	require.NoError(t, err)

	ids := make(map[uuid.UUID]bool)
	for _, o := range orgs {
		ids[o.ID] = true
	}
	assert.True(t, ids[org1.ID], "org1 should be in results")
	assert.True(t, ids[org2.ID], "org2 should be in results")
}

func TestQueryIntegration_Organizations_WithFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tag := uuid.New().String()[:8]
	createTestOrg(t, queries, "acme-"+tag)
	createTestOrg(t, queries, "other-"+tag)

	rf := OrganizationFilter()
	rows, err := Query(ctx, pool, rf, QueryParams{
		Filter: `displayName = "Org acme*"`,
	})
	require.NoError(t, err)

	orgs, err := ScanOrganizations(rows)
	require.NoError(t, err)

	require.Len(t, orgs, 1)
	assert.Contains(t, orgs[0].DisplayName, "acme")
}

func TestQueryIntegration_Organizations_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	createTestOrg(t, queries, uuid.New().String()[:8])
	createTestOrg(t, queries, uuid.New().String()[:8])
	createTestOrg(t, queries, uuid.New().String()[:8])

	rf := OrganizationFilter()
	rows, err := Query(ctx, pool, rf, QueryParams{PageSize: 2})
	require.NoError(t, err)

	orgs, err := ScanOrganizations(rows)
	require.NoError(t, err)

	// pageSize=2 → LIMIT 3, so up to 3 rows returned for next-token detection.
	assert.Equal(t, 3, len(orgs), "expected pageSize+1 rows for next-token detection")
}

// ---------------------------------------------------------------------------
// Query + ScanProjects
// ---------------------------------------------------------------------------

func TestQueryIntegration_Projects_NoFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	p1 := createTestProject(t, queries, org.ID, "Project Alpha")
	p2 := createTestProject(t, queries, org.ID, "Project Beta")

	rf := ProjectFilter()
	rows, err := Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String()})
	require.NoError(t, err)

	projects, err := ScanProjects(rows)
	require.NoError(t, err)

	require.Len(t, projects, 2)
	ids := map[uuid.UUID]bool{projects[0].ID: true, projects[1].ID: true}
	assert.True(t, ids[p1.ID])
	assert.True(t, ids[p2.ID])
}

func TestQueryIntegration_Projects_SoftDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	p := createTestProject(t, queries, org.ID, "Doomed Project")

	_, err := queries.SoftDeleteProject(ctx, db.SoftDeleteProjectParams{
		ID:        p.ID,
		DeletedBy: "test",
	})
	require.NoError(t, err)

	rf := ProjectFilter()

	// Without ShowDeleted: should return 0.
	rows, err := Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String()})
	require.NoError(t, err)
	projects, err := ScanProjects(rows)
	require.NoError(t, err)
	assert.Empty(t, projects, "soft-deleted project should be hidden")

	// With ShowDeleted: should return 1.
	rows, err = Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String(), ShowDeleted: true})
	require.NoError(t, err)
	projects, err = ScanProjects(rows)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, p.ID, projects[0].ID)
}

func TestQueryIntegration_Projects_OrderBy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	createTestProject(t, queries, org.ID, "Alpha")
	createTestProject(t, queries, org.ID, "Zeta")

	rf := ProjectFilter()
	rows, err := Query(ctx, pool, rf, QueryParams{
		ParentID: org.ID.String(),
		OrderBy:  "displayName desc",
	})
	require.NoError(t, err)

	projects, err := ScanProjects(rows)
	require.NoError(t, err)

	require.Len(t, projects, 2)
	assert.Equal(t, "Zeta", projects[0].DisplayName)
	assert.Equal(t, "Alpha", projects[1].DisplayName)
}

// ---------------------------------------------------------------------------
// Query + ScanTagKeys
// ---------------------------------------------------------------------------

func TestQueryIntegration_TagKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	tk1 := createTestTagKey(t, queries, org.ID, "env-"+uuid.New().String()[:6])
	tk2 := createTestTagKey(t, queries, org.ID, "team-"+uuid.New().String()[:6])

	rf := TagKeyFilter()
	rows, err := Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String()})
	require.NoError(t, err)

	keys, err := ScanTagKeys(rows)
	require.NoError(t, err)

	require.Len(t, keys, 2)
	ids := map[uuid.UUID]bool{keys[0].ID: true, keys[1].ID: true}
	assert.True(t, ids[tk1.ID])
	assert.True(t, ids[tk2.ID])
}

// ---------------------------------------------------------------------------
// Query + ScanTagValues
// ---------------------------------------------------------------------------

func TestQueryIntegration_TagValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	tk := createTestTagKey(t, queries, org.ID, "env-"+uuid.New().String()[:6])

	tv1 := createTestTagValue(t, queries, tk.ID, "prod", tk.NamespacedName)
	tv2 := createTestTagValue(t, queries, tk.ID, "staging", tk.NamespacedName)

	rf := TagValueFilter()
	rows, err := Query(ctx, pool, rf, QueryParams{ParentID: tk.ID.String()})
	require.NoError(t, err)

	values, err := ScanTagValues(rows)
	require.NoError(t, err)

	require.Len(t, values, 2)
	ids := map[uuid.UUID]bool{values[0].ID: true, values[1].ID: true}
	assert.True(t, ids[tv1.ID])
	assert.True(t, ids[tv2.ID])
}

// ---------------------------------------------------------------------------
// Query + ScanTagBindings
// ---------------------------------------------------------------------------

func TestQueryIntegration_TagBindings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	tk := createTestTagKey(t, queries, org.ID, "env-"+uuid.New().String()[:6])
	tv := createTestTagValue(t, queries, tk.ID, "prod", tk.NamespacedName)

	parentRes := "//pivox.dashkan.com/projects/" + uuid.New().String()
	tb := createTestTagBinding(t, queries, parentRes, tv.ID)

	rf := TagBindingFilter()
	rows, err := Query(ctx, pool, rf, QueryParams{ParentID: parentRes})
	require.NoError(t, err)

	bindings, err := ScanTagBindings(rows)
	require.NoError(t, err)

	require.Len(t, bindings, 1)
	assert.Equal(t, tb.ID, bindings[0].ID)
	assert.Equal(t, parentRes, bindings[0].ParentResource)
	assert.Equal(t, tv.ID, bindings[0].TagValueID)
	// Verify the origin column scans correctly.
	assert.Equal(t, db.TagBindingOriginUSER, bindings[0].Origin)
}

// ---------------------------------------------------------------------------
// Query + ScanApiKeys
// ---------------------------------------------------------------------------

func TestQueryIntegration_ApiKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	k1 := createTestApiKey(t, queries, org.ID, "Key Alpha")
	k2 := createTestApiKey(t, queries, org.ID, "Key Beta")

	rf := ApiKeyFilter()
	rows, err := Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String()})
	require.NoError(t, err)

	keys, err := ScanApiKeys(rows)
	require.NoError(t, err)

	require.Len(t, keys, 2)
	ids := map[uuid.UUID]bool{keys[0].ID: true, keys[1].ID: true}
	assert.True(t, ids[k1.ID])
	assert.True(t, ids[k2.ID])
}

func TestQueryIntegration_ApiKeys_SoftDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	k := createTestApiKey(t, queries, org.ID, "Ephemeral Key")

	_, err := queries.SoftDeleteApiKey(ctx, db.SoftDeleteApiKeyParams{
		ID:        k.ID,
		DeletedBy: "test",
	})
	require.NoError(t, err)

	rf := ApiKeyFilter()

	// Without ShowDeleted: 0 results.
	rows, err := Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String()})
	require.NoError(t, err)
	keys, err := ScanApiKeys(rows)
	require.NoError(t, err)
	assert.Empty(t, keys, "soft-deleted API key should be hidden")

	// With ShowDeleted: 1 result.
	rows, err = Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String(), ShowDeleted: true})
	require.NoError(t, err)
	keys, err = ScanApiKeys(rows)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, k.ID, keys[0].ID)
}

// ---------------------------------------------------------------------------
// Query edge cases
// ---------------------------------------------------------------------------

func TestQueryIntegration_InvalidFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, _, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	rf := ProjectFilter()
	_, err := Query(ctx, pool, rf, QueryParams{
		Filter: `"unclosed string`,
	})
	require.Error(t, err)
}

func TestQueryIntegration_InvalidOrderBy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, _, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	rf := ProjectFilter()
	_, err := Query(ctx, pool, rf, QueryParams{
		OrderBy: "nonExistentField",
	})
	require.Error(t, err)
}

func TestQueryIntegration_CursorPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	createTestProject(t, queries, org.ID, "P1")
	createTestProject(t, queries, org.ID, "P2")
	createTestProject(t, queries, org.ID, "P3")

	rf := ProjectFilter()

	// First, fetch all projects ordered by id ASC to discover the actual order.
	rows, err := Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String()})
	require.NoError(t, err)
	allProjects, err := ScanProjects(rows)
	require.NoError(t, err)
	require.Len(t, allProjects, 3)

	// Use the first project's ID as cursor — should get items after it.
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	require.NoError(t, err)
	cursorID := allProjects[0].ID
	cursorTok, err := EncodeNextPageToken(codec, cursorID)
	require.NoError(t, err)
	rows, err = Query(ctx, pool, rf, QueryParams{
		ParentID: org.ID.String(),
		Cursor:   cursorTok,
		Codec:    codec,
	})
	require.NoError(t, err)

	remaining, err := ScanProjects(rows)
	require.NoError(t, err)

	require.Len(t, remaining, 2)
	ids := map[uuid.UUID]bool{remaining[0].ID: true, remaining[1].ID: true}
	assert.True(t, ids[allProjects[1].ID], "second project should appear after cursor")
	assert.True(t, ids[allProjects[2].ID], "third project should appear after cursor")
	assert.False(t, ids[cursorID], "cursor project should NOT appear")
}

func TestQueryIntegration_PageSizeClamping(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	org := createTestOrg(t, queries, uuid.New().String()[:8])
	// Create 2 projects so we have something to query.
	createTestProject(t, queries, org.ID, "P1")
	createTestProject(t, queries, org.ID, "P2")

	rf := ProjectFilter()

	// pageSize=0 should default to 100 (LIMIT 101). With only 2 rows, both returned.
	rows, err := Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String(), PageSize: 0})
	require.NoError(t, err)
	projects, err := ScanProjects(rows)
	require.NoError(t, err)
	assert.Len(t, projects, 2, "pageSize=0 defaults to 100, so both projects returned")

	// pageSize=9999 should clamp to 1000 (LIMIT 1001). With only 2 rows, both returned.
	rows, err = Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String(), PageSize: 9999})
	require.NoError(t, err)
	projects, err = ScanProjects(rows)
	require.NoError(t, err)
	assert.Len(t, projects, 2, "pageSize=9999 clamps to 1000, so both projects returned")

	// pageSize=1 should return 2 rows (LIMIT 2 = pageSize+1).
	rows, err = Query(ctx, pool, rf, QueryParams{ParentID: org.ID.String(), PageSize: 1})
	require.NoError(t, err)
	projects, err = ScanProjects(rows)
	require.NoError(t, err)
	assert.Len(t, projects, 2, "pageSize=1 → LIMIT 2 → 2 rows returned (next-token detection)")
}
