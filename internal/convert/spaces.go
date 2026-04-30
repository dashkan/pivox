package convert

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// SpaceToProto converts a DB space to proto.
// orgName is the organization slug (e.g. "meridian-broadcasting").
// `actors` is the pre-resolved Actor map for the calling page; pass
// nil when no actors are needed.
func SpaceToProto(p db.Space, orgName string, actors map[uuid.UUID]*typespb.Actor) *apiv1.Space {
	pb := &apiv1.Space{
		Name:        fmt.Sprintf("organizations/%s/spaces/%s", orgName, p.Name),
		DisplayName: p.DisplayName,
		State:       spaceState(p.State),
		Etag:        p.Etag,
		CreatedBy:   actorOrNil(actors, p.CreatedBy),
		CreateTime:  timestamppb.New(p.CreateTime),
		UpdatedBy:   actorOrNil(actors, p.UpdatedBy),
		UpdateTime:  timestamppb.New(p.UpdateTime),
		DeletedBy:   actorOrNil(actors, p.DeletedBy),
	}
	if p.DeleteTime.Valid {
		pb.DeleteTime = timestamppb.New(p.DeleteTime.Time)
	}
	if p.PurgeTime.Valid {
		pb.PurgeTime = timestamppb.New(p.PurgeTime.Time)
	}
	if len(p.Labels) > 0 {
		labels := make(map[string]string)
		_ = json.Unmarshal(p.Labels, &labels)
		pb.Labels = labels
	}
	return pb
}

func spaceState(s db.ResourceState) apiv1.Space_State {
	switch s {
	case db.ResourceStateACTIVE:
		return apiv1.Space_ACTIVE
	case db.ResourceStateDELETEREQUESTED:
		return apiv1.Space_DELETE_REQUESTED
	default:
		return apiv1.Space_STATE_UNSPECIFIED
	}
}
