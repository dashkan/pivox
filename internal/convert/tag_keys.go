package convert

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox-server/internal/db/generated"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
)

// TagKeyToProto converts a DB tag key to proto.
func TagKeyToProto(tk db.TagKey) *apiv1.TagKey {
	return &apiv1.TagKey{
		Name:        "tagKeys/" + tk.ID.String(),
		Description: tk.Description,
		Etag:        tk.Etag,
		CreateTime:  timestamppb.New(tk.CreateTime),
		UpdateTime:  timestamppb.New(tk.UpdateTime),
	}
}
