// Copyright 2025 Pivox
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package convert

import (
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// DashboardToProto converts a dashboards row into the wire-shape
// Dashboard proto used by the gRPC service.
//
// The row's `payload` JSONB carries the marshaled Dashboard at write
// time; we unmarshal it as the base, then overlay the column-mirrored
// fields (resource name, etag, audit, timestamps, management_mode)
// because those are the source of truth on read. If payload and
// columns disagree (which they shouldn't — every Update re-marshals
// the proto into payload before committing), columns win.
//
// `parentName` is the dashboard's parent resource path (e.g.
// `organizations/acme/spaces/dev`). The handler upstream knows the
// parent's slug already and passes it in so this function does not
// need to look up the org / space.
//
// `actors` is the pre-resolved Actor map for the calling page; pass
// nil when the audit resolver is unavailable or the caller does not
// need actor-inflated fields.
//
// Returns an error iff payload unmarshaling fails — that's a data-
// integrity issue worth surfacing rather than swallowing.
func DashboardToProto(p db.Dashboard, parentName string, actors map[uuid.UUID]*typespb.Actor) (*apiv1.Dashboard, error) {
	d := &apiv1.Dashboard{}
	if len(p.Payload) > 0 {
		if err := protojson.Unmarshal(p.Payload, d); err != nil {
			return nil, fmt.Errorf("dashboard %s: unmarshal payload: %w", p.ID, err)
		}
	}

	// Column-mirrored fields are the source of truth on read. Overwrite
	// whatever the unmarshaled payload supplied so a stale payload
	// can never override the live row state.
	d.Name = fmt.Sprintf("%s/dashboards/%s", parentName, p.Name)
	d.DisplayName = p.DisplayName
	d.Description = p.Description
	d.ManagementMode = managementModeFromString(p.ManagementMode)
	d.Etag = p.Etag
	d.CreateTime = timestamppb.New(p.CreateTime)
	d.UpdateTime = timestamppb.New(p.UpdateTime)

	// Audit resolver is optional; nil leaves Actor fields unset.
	// (Actor fields aren't on the wire-stable Dashboard surface today,
	// but we keep the parameter in the signature so callers don't
	// have to refactor when actor fields are added later — same
	// pattern as SpaceToProto.)
	_ = actors

	return d, nil
}

// DashboardPayload marshals a Dashboard proto into the JSONB shape
// the dashboards.payload column stores. Used by Create / Update
// handlers right before INSERT / UPDATE.
//
// The marshaled bytes capture the entire proto including
// display_name, description, management_mode, layout, variables,
// annotations — every column-mirrored field plus the rich nested
// payload. On read the columns are re-overlaid (see
// DashboardToProto).
func DashboardPayload(d *apiv1.Dashboard) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("dashboard payload: nil proto")
	}
	out, err := protojson.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("dashboard payload: marshal: %w", err)
	}
	return out, nil
}

// managementModeFromString lifts the dashboards.management_mode
// column value into the proto enum. Unrecognized values resolve to
// UNSPECIFIED — every CHECK-constraint-valid row maps to a real
// enum.
func managementModeFromString(s string) apiv1.Dashboard_ManagementMode {
	switch s {
	case "USER_MANAGED":
		return apiv1.Dashboard_USER_MANAGED
	case "SYSTEM_MANAGED":
		return apiv1.Dashboard_SYSTEM_MANAGED
	default:
		return apiv1.Dashboard_MANAGEMENT_MODE_UNSPECIFIED
	}
}
