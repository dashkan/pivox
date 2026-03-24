package convert

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// TagBindingToProto converts a DB tag binding to proto.
func TagBindingToProto(tb db.TagBinding, tagValue db.TagValue) *apiv1.TagBinding {
	return &apiv1.TagBinding{
		Name:       "tagBindings/" + tb.ID.String(),
		TagValue:   "tagKeys/" + tagValue.TagKeyID.String() + "/tagValues/" + tagValue.ID.String(),
		Etag:       tb.Etag,
		CreateTime: timestamppb.New(tb.CreateTime),
		UpdateTime: timestamppb.New(tb.UpdateTime),
	}
}

// EffectiveTagToProto converts a ListEffectiveTags row to proto.
func EffectiveTagToProto(row db.ListEffectiveTagsRow) *apiv1.EffectiveTag {
	return &apiv1.EffectiveTag{
		TagValue:  "tagKeys/" + row.TagKeyID.String() + "/tagValues/" + row.TagValueID.String(),
		TagKey:    "tagKeys/" + row.TagKeyID.String(),
		Inherited: false,
	}
}
