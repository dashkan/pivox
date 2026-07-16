package convert_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/convert"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil"
)

// TestSampleWorkflowSeedConverts applies scripts/seeds/17_sample_workflow.sql
// against a real (migrated) test DB and proves the seeded connector + workflow
// + workflow_version rows round-trip through internal/convert without error —
// i.e. the hand-authored JSONB in the seed matches exactly what the read path
// unmarshals. If the definition shape drifts (wrong protojson field name, wrong
// enum spelling), convert silently drops fields and the step assertions here
// fail.
func TestSampleWorkflowSeedConverts(t *testing.T) {
	const (
		orgID     = "0192a000-0001-7000-8000-00000000000b"
		connID    = "0192a000-0060-7000-8000-00000000b001"
		wfID      = "0192a000-0061-7000-8000-00000000b001"
		verID     = "0192a000-0062-7000-8000-00000000b001"
		orgPrefix = "organizations/local-corp"
	)

	ctx := context.Background()
	pool, queries := testutil.SetupTestDB(t)

	// The seed attaches to local-corp; create just that org (the seed's
	// 11_local_corp.sql dependency), then apply the seed file itself.
	_, err := pool.Exec(ctx,
		`INSERT INTO organizations (id, name, display_name) VALUES ($1, 'local-corp', 'Local Corp')`,
		uuid.MustParse(orgID))
	require.NoError(t, err)

	seedSQL, err := os.ReadFile("../../scripts/seeds/17_sample_workflow.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(seedSQL))
	require.NoError(t, err, "seed SQL must apply cleanly against the real schema")

	// Connector round-trips to an http connector with the seeded base URL.
	connRow, err := queries.GetConnector(ctx, uuid.MustParse(connID))
	require.NoError(t, err)
	conn := convert.ConnectorToProto(connRow, orgPrefix, nil)
	assert.Equal(t, orgPrefix+"/connectors/sample-api", conn.GetName())
	require.NotNil(t, conn.GetHttp(), "connector config must rehydrate the http oneof")
	assert.Equal(t, "https://api.example.com/v1", conn.GetHttp().GetBaseUrl())

	// Workflow round-trips; its promoted version pointer resolves to version 1.
	wfRow, err := queries.GetWorkflow(ctx, uuid.MustParse(wfID))
	require.NoError(t, err)
	wf := convert.WorkflowToProto(wfRow, orgPrefix, nil,
		map[uuid.UUID]int64{uuid.MustParse(verID): 1})
	assert.Equal(t, orgPrefix+"/workflows/nightly-ingest", wf.GetName())
	assert.Equal(t, workflowsv1.WorkflowOrigin_OWNED, wf.GetOrigin())
	assert.True(t, wf.GetEnabled())
	assert.Equal(t, orgPrefix+"/workflows/nightly-ingest/versions/1", wf.GetVersion())

	// Version definition round-trips to a proto WITHOUT error, and the root
	// carries every node type in order.
	verRow, err := queries.GetWorkflowVersion(ctx, uuid.MustParse(verID))
	require.NoError(t, err)
	ver := convert.WorkflowVersionToProto(verRow, wf.GetName(), nil)

	// Parameters + trigger survived the round-trip.
	require.Len(t, ver.GetParameters(), 2)
	assert.Equal(t, "region", ver.GetParameters()[0].GetKey())
	assert.Equal(t, workflowsv1.ParamType_PARAM_STRING, ver.GetParameters()[0].GetType())
	require.NotNil(t, ver.GetTrigger().GetSchedule(), "schedule trigger must survive")

	root := ver.GetRoot()
	require.NotNil(t, root)
	steps := root.GetSteps()
	require.Len(t, steps, 7, "root must carry all seven top-level steps")

	// Assert each top-level step id + kind — proves every node type decoded.
	assert.Equal(t, "init", steps[0].GetId())
	require.NotNil(t, steps[0].GetActivity().GetSet())

	assert.Equal(t, "fetch", steps[1].GetId())
	fetchHTTP := steps[1].GetActivity().GetHttp()
	require.NotNil(t, fetchHTTP)
	assert.Equal(t, orgPrefix+"/connectors/sample-api", fetchHTTP.GetConnector())
	assert.Equal(t, "GET", fetchHTTP.GetMethod())
	assert.EqualValues(t, 3, fetchHTTP.GetRetry().GetMaxAttempts())

	assert.Equal(t, "route", steps[2].GetId())
	cond := steps[2].GetCondition()
	require.NotNil(t, cond)
	require.Len(t, cond.GetBranches(), 1)
	assert.Equal(t, "1 > 0", cond.GetBranches()[0].GetWhen())
	require.NotNil(t, cond.GetOtherwise(), "condition must carry an otherwise branch")

	assert.Equal(t, "fanout", steps[3].GetId())
	par := steps[3].GetParallel()
	require.NotNil(t, par)
	require.Len(t, par.GetBranches(), 2, "parallel must carry two lanes")

	assert.Equal(t, "guard", steps[4].GetId())
	try := steps[4].GetTry()
	require.NotNil(t, try)
	require.NotNil(t, try.GetBody())
	require.Len(t, try.GetCatch().GetSteps(), 1)
	require.NotNil(t, try.GetCatch().GetSteps()[0].GetActivity().GetFail(),
		"try catch must contain a fail")

	assert.Equal(t, "compose", steps[5].GetId())
	rw := steps[5].GetActivity().GetRunWorkflow()
	require.NotNil(t, rw)
	assert.Equal(t, orgPrefix+"/workflows/nightly-ingest", rw.GetWorkflow())

	assert.Equal(t, "done", steps[6].GetId())
	require.NotNil(t, steps[6].GetActivity().GetEnd(), "final step must be an end activity")

	// Collect every step id (root + nested + error_sequence) to prove
	// uniqueness across the whole version.
	seen := map[string]bool{}
	var walk func(seq *workflowsv1.Sequence)
	walk = func(seq *workflowsv1.Sequence) {
		if seq == nil {
			return
		}
		for _, s := range seq.GetSteps() {
			assert.False(t, seen[s.GetId()], "duplicate step id %q", s.GetId())
			seen[s.GetId()] = true
			if c := s.GetCondition(); c != nil {
				for _, b := range c.GetBranches() {
					walk(b.GetThen())
				}
				walk(c.GetOtherwise())
			}
			if p := s.GetParallel(); p != nil {
				for _, b := range p.GetBranches() {
					walk(b)
				}
			}
			if tr := s.GetTry(); tr != nil {
				walk(tr.GetBody())
				walk(tr.GetCatch())
			}
		}
	}
	walk(root)
	walk(ver.GetErrorSequence())
	// Nested root ids: init, fetch, route, route_live, route_idle, fanout,
	// fan_left, fan_right, guard, guard_probe, guard_fail, compose, done = 13.
	// errorSequence ids: err_note, err_notify = 2. Total = 15.
	assert.Len(t, seen, 15)
}
