package convert

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// DomainToProto converts a db.Domain row into the wire-level
// apiv1.Domain. The resource name is constructed from the parent
// org slug (looked up by the caller — this fn doesn't issue
// queries) and the domain itself.
func DomainToProto(d db.Domain, orgSlug string) *apiv1.Domain {
	pb := &apiv1.Domain{
		Name:       "organizations/" + orgSlug + "/domains/" + d.Domain,
		Domain:     d.Domain,
		State:      domainState(d.State),
		CreateTime: timestamppb.New(d.CreateTime),
		UpdateTime: timestamppb.New(d.UpdateTime),
		Etag:       d.Etag,
	}
	if d.VerifiedTime.Valid {
		pb.VerifiedTime = timestamppb.New(d.VerifiedTime.Time)
	}
	return pb
}

func domainState(s db.DomainState) apiv1.Domain_State {
	switch s {
	case db.DomainStatePENDING:
		return apiv1.Domain_PENDING
	case db.DomainStateVERIFIED:
		return apiv1.Domain_VERIFIED
	case db.DomainStateFAILED:
		return apiv1.Domain_FAILED
	default:
		return apiv1.Domain_STATE_UNSPECIFIED
	}
}
