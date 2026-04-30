package convert

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// ApiKeyToProto converts a DB API key to proto.
// orgName is the organization slug (e.g. "meridian-broadcasting").
// `actors` is the pre-resolved Actor map; pass nil to skip Actor
// inflation.
func ApiKeyToProto(k db.ApiKey, orgName string, actors map[uuid.UUID]*typespb.Actor) *apiv1.Key {
	pb := &apiv1.Key{
		Name:        fmt.Sprintf("organizations/%s/keys/%s", orgName, k.KeyID),
		DisplayName: k.DisplayName,
		KeyString:   "", // Never return key_string in regular responses
		Etag:        k.Etag,
		CreatedBy:   actorOrNil(actors, k.CreatedBy),
		CreateTime:  timestamppb.New(k.CreateTime),
		UpdatedBy:   actorOrNil(actors, k.UpdatedBy),
		UpdateTime:  timestamppb.New(k.UpdateTime),
		DeletedBy:   actorOrNil(actors, k.DeletedBy),
	}
	if k.DeleteTime.Valid {
		pb.DeleteTime = timestamppb.New(k.DeleteTime.Time)
	}
	if len(k.Annotations) > 0 {
		annotations := make(map[string]string)
		_ = json.Unmarshal(k.Annotations, &annotations)
		pb.Annotations = annotations
	}
	if len(k.Restrictions) > 0 {
		restrictions := &apiv1.Restrictions{}
		if err := protojson.Unmarshal(k.Restrictions, restrictions); err == nil {
			pb.Restrictions = restrictions
		}
	}
	return pb
}
