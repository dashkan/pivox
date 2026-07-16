package convert

import (
	"encoding/json"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	secretsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/secrets/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// SecretToProto maps a secrets row to its proto. namePrefix is the parent
// resource path ("organizations/{org}" or
// "organizations/{org}/spaces/{space}"). The value is deliberately OMITTED —
// secrets are write-only and never returned in any response.
func SecretToProto(s db.Secret, namePrefix string, actors map[uuid.UUID]*typespb.Actor) *secretsv1.Secret {
	var annotations map[string]string
	if len(s.Annotations) > 0 {
		_ = json.Unmarshal(s.Annotations, &annotations)
	}
	return &secretsv1.Secret{
		Name:        namePrefix + "/secrets/" + s.Slug,
		DisplayName: s.DisplayName,
		Annotations: annotations,
		Etag:        s.Etag,
		CreatedBy:   actorOrNil(actors, s.CreatedBy),
		CreateTime:  timestamppb.New(s.CreateTime),
		UpdatedBy:   actorOrNil(actors, s.UpdatedBy),
		UpdateTime:  timestamppb.New(s.UpdateTime),
	}
}
