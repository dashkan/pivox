package convert

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// ApiKeyToProto converts a DB API key to proto.
// orgName is the organization slug (e.g. "meridian-broadcasting").
func ApiKeyToProto(k db.ApiKey, orgName string) *apiv1.Key {
	pb := &apiv1.Key{
		Name:        fmt.Sprintf("organizations/%s/keys/%s", orgName, k.KeyID),
		DisplayName: k.DisplayName,
		KeyString:   "", // Never return key_string in regular responses
		Etag:        k.Etag,
		CreateTime:  timestamppb.New(k.CreateTime),
		UpdateTime:  timestamppb.New(k.UpdateTime),
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
