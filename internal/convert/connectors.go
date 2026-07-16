package convert

import (
	"encoding/json"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// ConnectorToProto maps a connectors row to its proto. namePrefix is the
// parent resource path ("organizations/{org}" or
// "organizations/{org}/spaces/{space}").
//
// The typed `oneof config` is persisted as protojson in the config JSONB
// column ({"http": {...}}); here it round-trips back by unmarshaling onto a
// scratch Connector and lifting the resolved oneof onto the output.
func ConnectorToProto(c db.Connector, namePrefix string, actors map[uuid.UUID]*typespb.Actor) *workflowsv1.Connector {
	var annotations map[string]string
	if len(c.Annotations) > 0 {
		_ = json.Unmarshal(c.Annotations, &annotations)
	}
	out := &workflowsv1.Connector{
		Name:        namePrefix + "/connectors/" + c.Slug,
		DisplayName: c.DisplayName,
		Description: c.Description,
		Agent:       c.Agent,
		Annotations: annotations,
		Etag:        c.Etag,
		CreatedBy:   actorOrNil(actors, c.CreatedBy),
		CreateTime:  timestamppb.New(c.CreateTime),
		UpdatedBy:   actorOrNil(actors, c.UpdatedBy),
		UpdateTime:  timestamppb.New(c.UpdateTime),
	}
	// An empty config column ("{}") yields a nil oneof; a stored
	// {"http": {...}} rehydrates the HttpConnector.
	if len(c.Config) > 0 {
		var scratch workflowsv1.Connector
		if err := protojson.Unmarshal(c.Config, &scratch); err == nil {
			out.Config = scratch.Config
		}
	}
	return out
}
