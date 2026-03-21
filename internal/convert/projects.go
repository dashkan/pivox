package convert

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox-server/internal/db/generated"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
)

// ProjectToProto converts a DB project to proto.
// orgName is the organization slug (e.g. "meridian-broadcasting").
func ProjectToProto(p db.Project, orgName string) *apiv1.Project {
	pb := &apiv1.Project{
		Name:        fmt.Sprintf("organizations/%s/projects/%s", orgName, p.Name),
		DisplayName: p.DisplayName,
		State:       projectState(p.State),
		Etag:        p.Etag,
		CreateTime:  timestamppb.New(p.CreateTime),
		UpdateTime:  timestamppb.New(p.UpdateTime),
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

func projectState(s db.ResourceState) apiv1.Project_State {
	switch s {
	case db.ResourceStateACTIVE:
		return apiv1.Project_ACTIVE
	case db.ResourceStateDELETEREQUESTED:
		return apiv1.Project_DELETE_REQUESTED
	default:
		return apiv1.Project_STATE_UNSPECIFIED
	}
}
