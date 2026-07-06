package convert

import (
	"encoding/json"
	"strconv"

	"github.com/google/uuid"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// workflowOrigin maps the DB origin text ('OWNED'/'MANAGED') to the proto
// enum. An unknown value degrades to UNSPECIFIED rather than panicking.
func workflowOrigin(s string) workflowsv1.WorkflowOrigin {
	if v, ok := workflowsv1.WorkflowOrigin_value[s]; ok {
		return workflowsv1.WorkflowOrigin(v)
	}
	return workflowsv1.WorkflowOrigin_WORKFLOW_ORIGIN_UNSPECIFIED
}

// WorkflowToProto maps a workflows row to its proto. namePrefix is the parent
// resource path ("organizations/{org}" or
// "organizations/{org}/spaces/{space}").
//
// The container's persistent `config` is a google.protobuf.Struct persisted as
// protojson in the config JSONB column; it round-trips back here. The
// OUTPUT_ONLY `version` pointer is the promoted version's numbered resource
// name — the DB stores the version's uuid, so versionNumbers supplies the
// uuid→version_number mapping the numbered name needs. A workflow with no
// promoted version (Version NULL, or its number absent from the map) leaves
// the field empty.
func WorkflowToProto(w db.Workflow, namePrefix string, actors map[uuid.UUID]*typespb.Actor, versionNumbers map[uuid.UUID]int64) *workflowsv1.Workflow {
	var annotations map[string]string
	if len(w.Annotations) > 0 {
		_ = json.Unmarshal(w.Annotations, &annotations)
	}
	out := &workflowsv1.Workflow{
		Name:        namePrefix + "/workflows/" + w.ID.String(),
		DisplayName: w.DisplayName,
		Description: w.Description,
		Enabled:     w.Enabled,
		Origin:      workflowOrigin(w.Origin),
		Etag:        w.Etag,
		Annotations: annotations,
		CreatedBy:   actorOrNil(actors, w.CreatedBy),
		CreateTime:  timestamppb.New(w.CreateTime),
		UpdatedBy:   actorOrNil(actors, w.UpdatedBy),
		UpdateTime:  timestamppb.New(w.UpdateTime),
	}
	// An empty config column ("{}") yields an empty struct; leave the field
	// nil in that case rather than rendering an empty Struct envelope.
	if len(w.Config) > 0 {
		var cfg structpb.Struct
		if err := protojson.Unmarshal(w.Config, &cfg); err == nil && len(cfg.GetFields()) > 0 {
			out.Config = &cfg
		}
	}
	if w.Version.Valid {
		if n, ok := versionNumbers[w.Version.Bytes]; ok {
			out.Version = out.Name + "/versions/" + strconv.FormatInt(n, 10)
		}
	}
	return out
}

// WorkflowVersionToProto maps a workflow_versions row to its proto.
// workflowName is the parent Workflow's resource name
// ("organizations/{org}[/spaces/{space}]/workflows/{wf-uuid}"); the version's
// own name appends the monotonic version_number ({version} segment).
//
// The definition JSONB carries the marshaled parameters + trigger + root
// (written by the CreateWorkflowVersion path via a scratch WorkflowVersion);
// here it round-trips back by unmarshaling onto a scratch WorkflowVersion and
// lifting the three definition fields onto the output.
func WorkflowVersionToProto(v db.WorkflowVersion, workflowName string, actors map[uuid.UUID]*typespb.Actor) *workflowsv1.WorkflowVersion {
	out := &workflowsv1.WorkflowVersion{
		Name:       workflowName + "/versions/" + strconv.FormatInt(v.VersionNumber, 10),
		Note:       v.Note,
		CreatedBy:  actorOrNil(actors, v.CreatedBy),
		CreateTime: timestamppb.New(v.CreateTime),
	}
	if len(v.Definition) > 0 {
		var scratch workflowsv1.WorkflowVersion
		if err := protojson.Unmarshal(v.Definition, &scratch); err == nil {
			out.Parameters = scratch.GetParameters()
			out.Trigger = scratch.GetTrigger()
			out.Root = scratch.GetRoot()
		}
	}
	return out
}

// runState maps the DB state text ('PENDING'/'RUNNING'/…) to the proto State
// enum. An unknown value degrades to STATE_UNSPECIFIED rather than panicking.
func runState(s string) workflowsv1.State {
	if v, ok := workflowsv1.State_value[s]; ok {
		return workflowsv1.State(v)
	}
	return workflowsv1.State_STATE_UNSPECIFIED
}

// WorkflowRunToProto maps a workflow_runs row to its proto. workflowName is the
// parent Workflow's resource name
// ("organizations/{org}[/spaces/{space}]/workflows/{wf-uuid}"); the run's own
// name appends "/runs/{run-uuid}".
//
// The OUTPUT_ONLY `version` pointer is the pinned WorkflowVersion's numbered
// resource name — the run stores the version's uuid (version_id), so
// versionNumbers supplies the uuid→version_number mapping the numbered name
// needs (mirrors WorkflowToProto's promoted-pointer rendering). If the number
// is absent from the map the field is left empty.
//
// trigger/input/output/steps/error are JSONB columns holding protojson-encoded
// proto values; each round-trips back here symmetric with the write path.
// triggered_by is the pivox.types.Actor for the user/service that fired the
// run — a NULL triggered_by (system trigger) leaves it unset.
func WorkflowRunToProto(r db.WorkflowRun, workflowName string, actors map[uuid.UUID]*typespb.Actor, versionNumbers map[uuid.UUID]int64) *workflowsv1.WorkflowRun {
	out := &workflowsv1.WorkflowRun{
		Name:        workflowName + "/runs/" + r.ID.String(),
		State:       runState(r.State),
		Subject:     r.Subject,
		TriggeredBy: actorOrNil(actors, r.TriggeredBy),
		CreateTime:  timestamppb.New(r.CreateTime),
	}
	if n, ok := versionNumbers[r.VersionID]; ok {
		out.Version = workflowName + "/versions/" + strconv.FormatInt(n, 10)
	}
	if len(r.Trigger) > 0 {
		var trig workflowsv1.RunTrigger
		if err := protojson.Unmarshal(r.Trigger, &trig); err == nil {
			out.Trigger = &trig
		}
	}
	out.Input = unmarshalStruct(r.Input)
	out.Output = unmarshalStruct(r.Output)
	if len(r.Steps) > 0 {
		var elems []json.RawMessage
		if err := json.Unmarshal(r.Steps, &elems); err == nil {
			steps := make([]*workflowsv1.StepState, 0, len(elems))
			for _, elem := range elems {
				var ss workflowsv1.StepState
				if err := protojson.Unmarshal(elem, &ss); err == nil {
					steps = append(steps, &ss)
				}
			}
			if len(steps) > 0 {
				out.Steps = steps
			}
		}
	}
	if len(r.Error) > 0 {
		var st statuspb.Status
		if err := protojson.Unmarshal(r.Error, &st); err == nil {
			out.Error = &st
		}
	}
	if r.StartTime.Valid {
		out.StartTime = timestamppb.New(r.StartTime.Time)
	}
	if r.EndTime.Valid {
		out.EndTime = timestamppb.New(r.EndTime.Time)
	}
	return out
}

// unmarshalStruct decodes a JSONB google.protobuf.Struct column back to its
// proto. A NULL/empty column or an empty object yields nil, so the proto field
// stays unset rather than rendering an empty Struct envelope.
func unmarshalStruct(b []byte) *structpb.Struct {
	if len(b) == 0 {
		return nil
	}
	var s structpb.Struct
	if err := protojson.Unmarshal(b, &s); err != nil || len(s.GetFields()) == 0 {
		return nil
	}
	return &s
}
