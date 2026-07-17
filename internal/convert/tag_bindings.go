package convert

import (
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// TagBindingToProto converts a DB tag binding to proto. orgSlug is the
// organization slug the bound tag value belongs to; it makes the tag_value
// reference org-scoped. The binding's own name is derived from its stored
// parent_resource (already an `organizations/{org}[/...]` path), keeping the
// binding scoped under whatever resource it was bound to. `actors` is the
// pre-resolved Actor map; pass nil to skip Actor inflation.
func TagBindingToProto(tb db.TagBinding, tagValue db.TagValue, orgSlug string, actors map[uuid.UUID]*typespb.Actor) *apiv1.TagBinding {
	return &apiv1.TagBinding{
		Name:       tb.ParentResource + "/tagBindings/" + tb.ID.String(),
		TagValue:   "organizations/" + orgSlug + "/tagKeys/" + tagValue.TagKeyID.String() + "/tagValues/" + tagValue.ID.String(),
		Etag:       tb.Etag,
		CreatedBy:  actorOrNil(actors, tb.CreatedBy),
		CreateTime: timestamppb.New(tb.CreateTime),
	}
}

// EffectiveTagToProto converts a ListEffectiveTags row to proto. orgSlug is the
// organization slug the tag key/value belong to, making both references
// org-scoped.
func EffectiveTagToProto(row db.ListEffectiveTagsRow, orgSlug string) *apiv1.EffectiveTag {
	return &apiv1.EffectiveTag{
		TagValue:  "organizations/" + orgSlug + "/tagKeys/" + row.TagKeyID.String() + "/tagValues/" + row.TagValueID.String(),
		TagKey:    "organizations/" + orgSlug + "/tagKeys/" + row.TagKeyID.String(),
		Inherited: false,
	}
}
