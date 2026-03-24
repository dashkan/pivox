package convert

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
)

// RequestToProto converts a DB request to proto.
// projectName is the full resource name of the parent project
// (e.g. "organizations/acme/projects/my-project").
func RequestToProto(row db.Request, projectName string) *assetsv1.Request {
	pb := &assetsv1.Request{
		Name:        fmt.Sprintf("%s/requests/%s", projectName, row.Name),
		DisplayName: row.DisplayName,
		Description: row.Description,
		State:       requestState(row.State),
		Priority:    requestPriority(row.Priority),
		Assignee:    row.Assignee,
		Etag:        row.Etag,
		Creator:     row.CreatedBy,
		Updater:     row.UpdatedBy,
		CreateTime:  timestamppb.New(row.CreateTime),
		UpdateTime:  timestamppb.New(row.UpdateTime),
	}
	if row.DeleteTime.Valid {
		pb.DeleteTime = timestamppb.New(row.DeleteTime.Time)
	}
	if row.PurgeTime.Valid {
		pb.PurgeTime = timestamppb.New(row.PurgeTime.Time)
	}
	if row.DueTime.Valid {
		pb.DueTime = timestamppb.New(row.DueTime.Time)
	}
	if row.DeliveredTime.Valid {
		pb.DeliveredTime = timestamppb.New(row.DeliveredTime.Time)
	}
	if row.ApprovedTime.Valid {
		pb.ApprovedTime = timestamppb.New(row.ApprovedTime.Time)
	}
	if len(row.Annotations) > 0 {
		annotations := make(map[string]string)
		_ = json.Unmarshal(row.Annotations, &annotations)
		pb.Annotations = annotations
	}
	return pb
}

// LineItemToProto converts a DB line item to proto.
// requestName is the full resource name of the parent request
// (e.g. "organizations/acme/projects/my-project/requests/req-1").
// projectName is the full resource name of the parent project
// (e.g. "organizations/acme/projects/my-project").
func LineItemToProto(row db.LineItem, requestName string, projectName string) *assetsv1.LineItem {
	pb := &assetsv1.LineItem{
		Name:        fmt.Sprintf("%s/lineItems/%s", requestName, row.Name),
		DisplayName: row.DisplayName,
		Description: row.Description,
		State:       lineItemState(row.State),
		Creator:     row.CreatedBy,
		CreateTime:  timestamppb.New(row.CreateTime),
		UpdateTime:  timestamppb.New(row.UpdateTime),
	}
	if row.MediaType.Valid {
		pb.MediaType = assetMediaType(row.MediaType.AssetMediaType)
	}
	if row.AssetID.Valid {
		pb.Asset = fmt.Sprintf("%s/assets/%s", projectName, row.Name)
	}
	if len(row.Annotations) > 0 {
		annotations := make(map[string]string)
		_ = json.Unmarshal(row.Annotations, &annotations)
		pb.Annotations = annotations
	}
	return pb
}

func requestState(s db.RequestState) assetsv1.Request_State {
	switch s {
	case db.RequestStateDRAFT:
		return assetsv1.Request_DRAFT
	case db.RequestStateOPEN:
		return assetsv1.Request_OPEN
	case db.RequestStateINPROGRESS:
		return assetsv1.Request_IN_PROGRESS
	case db.RequestStateDELIVERED:
		return assetsv1.Request_DELIVERED
	case db.RequestStateAPPROVED:
		return assetsv1.Request_APPROVED
	case db.RequestStateREVISIONREQUESTED:
		return assetsv1.Request_REVISION_REQUESTED
	case db.RequestStateREJECTED:
		return assetsv1.Request_REJECTED
	case db.RequestStateCANCELLED:
		return assetsv1.Request_CANCELLED
	default:
		return assetsv1.Request_STATE_UNSPECIFIED
	}
}

func requestPriority(p db.RequestPriority) assetsv1.Request_Priority {
	switch p {
	case db.RequestPriorityLOW:
		return assetsv1.Request_LOW
	case db.RequestPriorityNORMAL:
		return assetsv1.Request_NORMAL
	case db.RequestPriorityHIGH:
		return assetsv1.Request_HIGH
	case db.RequestPriorityURGENT:
		return assetsv1.Request_URGENT
	default:
		return assetsv1.Request_PRIORITY_UNSPECIFIED
	}
}

func lineItemState(s db.LineItemState) assetsv1.LineItem_State {
	switch s {
	case db.LineItemStatePENDING:
		return assetsv1.LineItem_PENDING
	case db.LineItemStateINPROGRESS:
		return assetsv1.LineItem_IN_PROGRESS
	case db.LineItemStateDELIVERED:
		return assetsv1.LineItem_DELIVERED
	case db.LineItemStateAPPROVED:
		return assetsv1.LineItem_APPROVED
	case db.LineItemStateREVISIONREQUESTED:
		return assetsv1.LineItem_REVISION_REQUESTED
	default:
		return assetsv1.LineItem_STATE_UNSPECIFIED
	}
}
