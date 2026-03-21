package server

import (
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
)

func newValidator(t *testing.T) protovalidate.Validator {
	t.Helper()
	v, err := protovalidate.New()
	require.NoError(t, err)
	return v
}

func TestFindFieldMaskAndResource(t *testing.T) {
	t.Run("UpdateProjectRequest", func(t *testing.T) {
		maskFD, resourceFD := findFieldMaskAndResource((&apiv1.UpdateProjectRequest{}).ProtoReflect().Descriptor())
		require.NotNil(t, maskFD)
		require.NotNil(t, resourceFD)
		assert.Equal(t, "update_mask", string(maskFD.Name()))
		assert.Equal(t, "project", string(resourceFD.Name()))
	})

	t.Run("UpdateTagKeyRequest", func(t *testing.T) {
		maskFD, resourceFD := findFieldMaskAndResource((&apiv1.UpdateTagKeyRequest{}).ProtoReflect().Descriptor())
		require.NotNil(t, maskFD)
		require.NotNil(t, resourceFD)
		assert.Equal(t, "update_mask", string(maskFD.Name()))
		assert.Equal(t, "tag_key", string(resourceFD.Name()))
	})

	t.Run("GetProjectRequest_NoMask", func(t *testing.T) {
		maskFD, resourceFD := findFieldMaskAndResource((&apiv1.GetProjectRequest{}).ProtoReflect().Descriptor())
		assert.Nil(t, maskFD)
		assert.Nil(t, resourceFD)
	})
}

func TestValidateWithFieldMaskAwareness(t *testing.T) {
	v := newValidator(t)

	t.Run("UpdateWithMask_SkipsResourceValidation", func(t *testing.T) {
		// Only name + display_name — no project_id (which has min_len: 6).
		// Should pass because the mask makes us skip resource validation.
		err := validateWithFieldMaskAwareness(&apiv1.UpdateProjectRequest{
			Project: &apiv1.Project{
				Name:        "projects/123",
				DisplayName: "Updated",
			},
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"display_name"},
			},
		}, v)
		assert.NoError(t, err)
	})

	t.Run("UpdateWithMask_NilResource_Fails", func(t *testing.T) {
		err := validateWithFieldMaskAwareness(&apiv1.UpdateProjectRequest{
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"display_name"},
			},
		}, v)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("UpdateWithoutMask_ValidatesNormally", func(t *testing.T) {
		// No mask — should validate the full resource normally.
		// With no mask, a valid project should pass.
		err := validateWithFieldMaskAwareness(&apiv1.UpdateProjectRequest{
			Project: &apiv1.Project{
				Name:        "projects/123",
				DisplayName: "Updated",
			},
		}, v)
		assert.NoError(t, err)
	})

	t.Run("NonUpdateRequest_ValidatesNormally", func(t *testing.T) {
		// GetProjectRequest with empty name — should fail validation.
		err := validateWithFieldMaskAwareness(&apiv1.GetProjectRequest{
			Name: "",
		}, v)
		assert.Error(t, err)
	})

	t.Run("UpdateTagKey_SkipsShortNameValidation", func(t *testing.T) {
		// Only name + description — no short_name/parent (which have min_len: 1).
		err := validateWithFieldMaskAwareness(&apiv1.UpdateTagKeyRequest{
			TagKey: &apiv1.TagKey{
				Name:        "tagKeys/123",
				Description: "updated",
			},
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"description"},
			},
		}, v)
		assert.NoError(t, err)
	})

	t.Run("UpdateKey_SkipsResourceValidation", func(t *testing.T) {
		err := validateWithFieldMaskAwareness(&apiv1.UpdateKeyRequest{
			Key: &apiv1.Key{
				Name:        "projects/123/keys/456",
				DisplayName: "updated",
			},
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"display_name"},
			},
		}, v)
		assert.NoError(t, err)
	})

	t.Run("UpdateWithMask_ValidatesMaskedField", func(t *testing.T) {
		// display_name has max_len: 30. Sending a value that exceeds it
		// in the mask should fail — proving masked fields ARE validated.
		err := validateWithFieldMaskAwareness(&apiv1.UpdateProjectRequest{
			Project: &apiv1.Project{
				Name:        "projects/123",
				DisplayName: "This display name is way too long to pass validation",
			},
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"display_name"},
			},
		}, v)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "display_name")
	})
}
