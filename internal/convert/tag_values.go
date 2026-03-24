package convert

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// TagValueToProto converts a DB tag value to proto.
func TagValueToProto(tv db.TagValue) *apiv1.TagValue {
	return &apiv1.TagValue{
		Name:        "tagKeys/" + tv.TagKeyID.String() + "/tagValues/" + tv.ID.String(),
		Description: tv.Description,
		Etag:        tv.Etag,
		CreateTime:  timestamppb.New(tv.CreateTime),
		UpdateTime:  timestamppb.New(tv.UpdateTime),
	}
}
