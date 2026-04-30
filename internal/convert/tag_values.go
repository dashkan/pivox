package convert

import (
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// TagValueToProto converts a DB tag value to proto. `actors` is the
// pre-resolved Actor map; pass nil to skip Actor inflation.
func TagValueToProto(tv db.TagValue, actors map[uuid.UUID]*typespb.Actor) *apiv1.TagValue {
	return &apiv1.TagValue{
		Name:        "tagKeys/" + tv.TagKeyID.String() + "/tagValues/" + tv.ID.String(),
		Description: tv.Description,
		Etag:        tv.Etag,
		CreatedBy:   actorOrNil(actors, tv.CreatedBy),
		CreateTime:  timestamppb.New(tv.CreateTime),
		UpdatedBy:   actorOrNil(actors, tv.UpdatedBy),
		UpdateTime:  timestamppb.New(tv.UpdateTime),
	}
}
