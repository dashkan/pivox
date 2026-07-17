package tags_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// foreignTags seeds a tag key + tag value + tag binding directly in the DB under
// `org`, returning their ids. Used to plant another org's resources so a caller
// authorized for a DIFFERENT org can attempt to reach them by leaf UUID.
type foreignTags struct {
	tagKeyID   uuid.UUID
	tagValueID uuid.UUID
	bindingID  uuid.UUID
}

func seedForeignTags(t *testing.T, ctx context.Context, q *db.Queries, org grpcharness.OwnedOrg) foreignTags {
	t.Helper()
	createdBy := convert.PgUUID(org.Owner.IdentityID)
	key, err := q.CreateTagKey(ctx, db.CreateTagKeyParams{
		ID:             uuid.New(),
		OrgID:          org.Row.ID,
		ShortName:      "foreign-env",
		NamespacedName: org.Row.ID.String() + "/foreign-env",
		Description:    "k",
		CreatedBy:      createdBy,
	})
	require.NoError(t, err)
	val, err := q.CreateTagValue(ctx, db.CreateTagValueParams{
		ID:             uuid.New(),
		TagKeyID:       key.ID,
		ShortName:      "foreign-prod",
		NamespacedName: key.NamespacedName + "/foreign-prod",
		Description:    "v",
		CreatedBy:      createdBy,
	})
	require.NoError(t, err)
	binding, err := q.CreateTagBinding(ctx, db.CreateTagBindingParams{
		ID:             uuid.New(),
		ParentResource: "organizations/" + org.Slug,
		TagValueID:     val.ID,
		CreatedBy:      createdBy,
	})
	require.NoError(t, err)
	return foreignTags{tagKeyID: key.ID, tagValueID: val.ID, bindingID: binding.ID}
}

// newCrossOrgHarness seeds a victim org (orgB) with tag resources, then an
// attacker org (orgA) whose owner becomes the harness caller. The returned
// paths embed orgA's slug (which the caller IS authorized for) but orgB's leaf
// UUIDs — the exact IDOR shape the org-scoping fix must defeat.
func newCrossOrgHarness(t *testing.T) (*grpcharness.Harness, grpcharness.OwnedOrg, foreignTags) {
	t.Helper()
	// Seed the victim org first and plant its tag resources.
	h, victim := newTagsHarness(t, "victim-org")
	foreign := seedForeignTags(t, context.Background(), h.Queries, victim)
	// Seed the attacker org LAST so the harness caller is the attacker owner.
	attacker := h.SeedOwnedOrg(t, "attacker-org", "Attacker", "attacker")
	return h, attacker, foreign
}

func TestE2E_TagValue_CrossOrgGet_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, attacker, foreign := newCrossOrgHarness(t)
	client := apiv1.NewTagValuesClient(h.Conn())

	// Path: attacker's own org slug (authorized) + victim's leaf UUIDs.
	name := "organizations/" + attacker.Slug + "/tagKeys/" + foreign.tagKeyID.String() +
		"/tagValues/" + foreign.tagValueID.String()
	_, err := client.GetTagValue(context.Background(), &apiv1.GetTagValueRequest{Name: name})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err),
		"cross-org GetTagValue must be NotFound, not leak the other org's value")
}

func TestE2E_TagValue_CrossOrgList_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, attacker, foreign := newCrossOrgHarness(t)
	client := apiv1.NewTagValuesClient(h.Conn())

	parent := "organizations/" + attacker.Slug + "/tagKeys/" + foreign.tagKeyID.String()
	_, err := client.ListTagValues(context.Background(), &apiv1.ListTagValuesRequest{Parent: parent})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err),
		"cross-org ListTagValues must be NotFound, not list the other org's values")
}

func TestE2E_TagValue_CrossOrgCreate_NotFoundAndNoWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, attacker, foreign := newCrossOrgHarness(t)
	ctx := context.Background()
	client := apiv1.NewTagValuesClient(h.Conn())

	before, err := h.Queries.CountTagValuesByTagKey(ctx, foreign.tagKeyID)
	require.NoError(t, err)

	parent := "organizations/" + attacker.Slug + "/tagKeys/" + foreign.tagKeyID.String()
	_, err = client.CreateTagValue(ctx, &apiv1.CreateTagValueRequest{
		Parent:     parent,
		TagValueId: "sneaky",
		TagValue:   &apiv1.TagValue{Description: "should not persist"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err),
		"cross-org CreateTagValue must be NotFound")

	after, err := h.Queries.CountTagValuesByTagKey(ctx, foreign.tagKeyID)
	require.NoError(t, err)
	assert.Equal(t, before, after, "no tag value may be written under the other org's tag key")
}

func TestE2E_TagBinding_CrossOrgReference_NotFoundAndNoWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, attacker, foreign := newCrossOrgHarness(t)
	ctx := context.Background()
	client := apiv1.NewTagBindingsClient(h.Conn())

	before, err := h.Queries.CountTagBindingsByTagValue(ctx, foreign.tagValueID)
	require.NoError(t, err)

	// Attacker's own org parent (authorized) but references the victim's tag value.
	foreignTagValueName := "organizations/" + attacker.Slug + "/tagKeys/" + foreign.tagKeyID.String() +
		"/tagValues/" + foreign.tagValueID.String()
	_, err = client.CreateTagBinding(ctx, &apiv1.CreateTagBindingRequest{
		Parent:     "organizations/" + attacker.Slug,
		TagBinding: &apiv1.TagBinding{TagValue: foreignTagValueName},
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err),
		"binding a cross-org tag value must be NotFound")

	after, err := h.Queries.CountTagBindingsByTagValue(ctx, foreign.tagValueID)
	require.NoError(t, err)
	assert.Equal(t, before, after, "no binding may be written referencing the other org's tag value")
}

func TestE2E_TagBinding_CrossOrgGet_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, attacker, foreign := newCrossOrgHarness(t)
	client := apiv1.NewTagBindingsClient(h.Conn())

	name := "organizations/" + attacker.Slug + "/tagBindings/" + foreign.bindingID.String()
	_, err := client.GetTagBinding(context.Background(), &apiv1.GetTagBindingRequest{Name: name})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err),
		"cross-org GetTagBinding must be NotFound, not leak the other org's binding")
}

func TestE2E_TagValue_CrossOrgUpdate_NotFoundAndNoMutation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, attacker, foreign := newCrossOrgHarness(t)
	ctx := context.Background()
	client := apiv1.NewTagValuesClient(h.Conn())

	name := "organizations/" + attacker.Slug + "/tagKeys/" + foreign.tagKeyID.String() +
		"/tagValues/" + foreign.tagValueID.String()
	_, err := client.UpdateTagValue(ctx, &apiv1.UpdateTagValueRequest{
		TagValue:   &apiv1.TagValue{Name: name, Description: "hijacked"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description"}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-org UpdateTagValue must be NotFound")

	got, err := h.Queries.GetTagValue(ctx, foreign.tagValueID)
	require.NoError(t, err)
	assert.Equal(t, "v", got.Description, "the other org's value must not be mutated")
}

func TestE2E_TagValue_CrossOrgDelete_NotFoundAndNoDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, attacker, foreign := newCrossOrgHarness(t)
	ctx := context.Background()
	client := apiv1.NewTagValuesClient(h.Conn())

	name := "organizations/" + attacker.Slug + "/tagKeys/" + foreign.tagKeyID.String() +
		"/tagValues/" + foreign.tagValueID.String()
	_, err := client.DeleteTagValue(ctx, &apiv1.DeleteTagValueRequest{Name: name})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-org DeleteTagValue must be NotFound")

	_, err = h.Queries.GetTagValue(ctx, foreign.tagValueID)
	require.NoError(t, err, "the other org's value must still exist")
}

func TestE2E_TagBinding_CrossOrgDelete_NotFoundAndNoDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h, attacker, foreign := newCrossOrgHarness(t)
	ctx := context.Background()
	client := apiv1.NewTagBindingsClient(h.Conn())

	name := "organizations/" + attacker.Slug + "/tagBindings/" + foreign.bindingID.String()
	_, err := client.DeleteTagBinding(ctx, &apiv1.DeleteTagBindingRequest{Name: name})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-org DeleteTagBinding must be NotFound")

	_, err = h.Queries.GetTagBinding(ctx, foreign.bindingID)
	require.NoError(t, err, "the other org's binding must still exist")
}
