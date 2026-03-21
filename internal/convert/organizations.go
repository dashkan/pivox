package convert

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox-server/internal/db/generated"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
)

func OrganizationToProto(o db.Organization) *apiv1.Organization {
	pb := &apiv1.Organization{
		Name:        "organizations/" + o.Name,
		DisplayName: o.DisplayName,
		State:       orgState(o.State),
		Etag:        o.Etag,
		CreateTime:  timestamppb.New(o.CreateTime),
		UpdateTime:  timestamppb.New(o.UpdateTime),
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
