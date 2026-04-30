package convert

import (
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// OrganizationToProto converts a DB row to its proto representation.
// `actors` is the pre-resolved Actor map produced by audit.Resolver
// for the union of `*_by` IDs in the calling page. Pass nil when no
// actors are needed (e.g. a partial response that intentionally omits
// audit fields). Unknown IDs in the map yield no Actor on the proto
// — the resolver guarantees a placeholder for every requested id.
func OrganizationToProto(o db.Organization, actors map[uuid.UUID]*apiv1.Actor) *apiv1.Organization {
	pb := &apiv1.Organization{
		Name:        "organizations/" + o.Name,
		DisplayName: o.DisplayName,
		State:       orgState(o.State),
		Etag:        o.Etag,
		CreatedBy:   actorOrNil(actors, o.CreatedBy),
		CreateTime:  timestamppb.New(o.CreateTime),
		UpdatedBy:   actorOrNil(actors, o.UpdatedBy),
		UpdateTime:  timestamppb.New(o.UpdateTime),
		DeletedBy:   actorOrNil(actors, o.DeletedBy),
	}
	if o.DeleteTime.Valid {
		pb.DeleteTime = timestamppb.New(o.DeleteTime.Time)
	}
	if o.PurgeTime.Valid {
		pb.PurgeTime = timestamppb.New(o.PurgeTime.Time)
	}
	return pb
}

func orgState(s db.ResourceState) apiv1.Organization_State {
	switch s {
	case db.ResourceStateACTIVE:
		return apiv1.Organization_ACTIVE
	case db.ResourceStateDELETEREQUESTED:
		return apiv1.Organization_DELETE_REQUESTED
	default:
		return apiv1.Organization_STATE_UNSPECIFIED
	}
}
