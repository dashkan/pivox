package tags_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// TestE2E_TagValue_CreateThenList proves the CreateTagValue + ListTagValues
// RPCs are reachable through the real interceptor chain. Before the
// org-scoped-name fix these were 100% dead: the permission registry's
// ScopeFromPath("parent") requires an `organizations/{org}/...` parent, while
// the handler's parseTagKeyParent required a bare `tagKeys/{uuid}` parent — no
// single parent value satisfied both (bare → InvalidArgument at the interceptor;
// org-scoped → error at the handler). With org-scoped names both agree.
func TestE2E_TagValue_CreateThenList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, owned := newTagsHarness(t, "tags-value-rt")
	ctx := context.Background()
	keysClient := apiv1.NewTagKeysClient(h.Conn())
	valuesClient := apiv1.NewTagValuesClient(h.Conn())

	orgParent := "organizations/" + owned.Slug

	// Create a tag key; its bare name is "tagKeys/{uuid}". The org-scoped tag
	// key parent is org + the returned bare name.
	tagKeyName := createTagKeyName(t, ctx, keysClient, orgParent, "env")
	tagKeyParent := orgParent + "/" + tagKeyName // organizations/{org}/tagKeys/{uuid}

	// CreateTagValue must reach the handler and return the new value with an
	// org-scoped name.
	op, err := valuesClient.CreateTagValue(ctx, &apiv1.CreateTagValueRequest{
		Parent:     tagKeyParent,
		TagValueId: "production",
		TagValue:   &apiv1.TagValue{Description: "Production env"},
	})
	require.NoError(t, err, "CreateTagValue must be reachable through the interceptor chain")
	require.True(t, op.GetDone())

	var created apiv1.TagValue
	require.NoError(t, op.GetResponse().UnmarshalTo(&created))
	assert.True(t, strings.HasPrefix(created.GetName(), tagKeyParent+"/tagValues/"),
		"created value name must be org-scoped under the tag key parent, got %q", created.GetName())

	// ListTagValues must reach the handler and surface the value we created.
	resp, err := valuesClient.ListTagValues(ctx, &apiv1.ListTagValuesRequest{Parent: tagKeyParent})
	require.NoError(t, err, "ListTagValues must be reachable through the interceptor chain")
	require.Len(t, resp.GetTagValues(), 1)
	assert.Equal(t, created.GetName(), resp.GetTagValues()[0].GetName())

	// The org-scoped name round-trips through GetTagValue.
	got, err := valuesClient.GetTagValue(ctx, &apiv1.GetTagValueRequest{Name: created.GetName()})
	require.NoError(t, err, "GetTagValue must accept the org-scoped name it returned")
	assert.Equal(t, "Production env", got.GetDescription())
}

// TestE2E_TagBinding_CreateThenList proves CreateTagBinding + ListTagBindings
// are reachable through the interceptor chain using an org-scoped tag_value
// reference in the binding body.
func TestE2E_TagBinding_CreateThenList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h, owned := newTagsHarness(t, "tags-binding-rt")
	ctx := context.Background()
	keysClient := apiv1.NewTagKeysClient(h.Conn())
	valuesClient := apiv1.NewTagValuesClient(h.Conn())
	bindingsClient := apiv1.NewTagBindingsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug

	tagKeyName := createTagKeyName(t, ctx, keysClient, orgParent, "env")
	tagKeyParent := orgParent + "/" + tagKeyName

	valOp, err := valuesClient.CreateTagValue(ctx, &apiv1.CreateTagValueRequest{
		Parent:     tagKeyParent,
		TagValueId: "production",
		TagValue:   &apiv1.TagValue{Description: "Production env"},
	})
	require.NoError(t, err)
	var tagValue apiv1.TagValue
	require.NoError(t, valOp.GetResponse().UnmarshalTo(&tagValue))

	// CreateTagBinding binds the org-scoped tag value to the org resource.
	bindOp, err := bindingsClient.CreateTagBinding(ctx, &apiv1.CreateTagBindingRequest{
		Parent:     orgParent,
		TagBinding: &apiv1.TagBinding{TagValue: tagValue.GetName()},
	})
	require.NoError(t, err, "CreateTagBinding must be reachable through the interceptor chain")
	require.True(t, bindOp.GetDone())

	var created apiv1.TagBinding
	require.NoError(t, bindOp.GetResponse().UnmarshalTo(&created))
	assert.Equal(t, tagValue.GetName(), created.GetTagValue(),
		"binding must echo the org-scoped tag value name")
	assert.True(t, strings.HasPrefix(created.GetName(), orgParent+"/tagBindings/"),
		"binding name must be org-scoped under the parent resource, got %q", created.GetName())

	// ListTagBindings surfaces the binding under the org parent.
	resp, err := bindingsClient.ListTagBindings(ctx, &apiv1.ListTagBindingsRequest{Parent: orgParent})
	require.NoError(t, err, "ListTagBindings must be reachable through the interceptor chain")
	require.Len(t, resp.GetTagBindings(), 1)
	assert.Equal(t, tagValue.GetName(), resp.GetTagBindings()[0].GetTagValue())
	assert.Equal(t, created.GetName(), resp.GetTagBindings()[0].GetName())
}
