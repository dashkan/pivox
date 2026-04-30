package convert

import (
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// TagBindingToProto converts a DB tag binding to proto. `actors` is
// the pre-resolved Actor map; pass nil to skip Actor inflation.
func TagBindingToProto(tb db.TagBinding, tagValue db.TagValue, actors map[uuid.UUID]*typespb.Actor) *apiv1.TagBinding {
	return &apiv1.TagBinding{
		Name:       "tagBindings/" + tb.ID.String(),
		TagValue:   "tagKeys/" + tagValue.TagKeyID.String() + "/tagValues/" + tagValue.ID.String(),
		Etag:       tb.Etag,
		CreatedBy:  actorOrNil(actors, tb.CreatedBy),
		CreateTime: timestamppb.New(tb.CreateTime),
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
