package convert

import (
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// TagKeyToProto converts a DB tag key to proto. `actors` is the
// pre-resolved Actor map; pass nil to skip Actor inflation.
func TagKeyToProto(tk db.TagKey, actors map[uuid.UUID]*typespb.Actor) *apiv1.TagKey {
	return &apiv1.TagKey{
		Name:        "tagKeys/" + tk.ID.String(),
		Description: tk.Description,
		Etag:        tk.Etag,
		CreatedBy:   actorOrNil(actors, tk.CreatedBy),
		CreateTime:  timestamppb.New(tk.CreateTime),
		UpdatedBy:   actorOrNil(actors, tk.UpdatedBy),
		UpdateTime:  timestamppb.New(tk.UpdateTime),
	}
}
