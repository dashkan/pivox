package workflows_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// setStepDefinition builds a minimal-but-valid version definition: a single
// SetActivity step, one parameter, and an optional trigger left absent (a
// building-block version).
func setStepDefinition(paramKey, varName, celExpr string) *workflowsv1.WorkflowVersion {
	return &workflowsv1.WorkflowVersion{
		Note:       "def",
		Parameters: []*workflowsv1.ParameterDef{{Key: paramKey, Type: workflowsv1.ParamType_PARAM_STRING}},
		Root: &workflowsv1.Sequence{Steps: []*workflowsv1.Step{{
			Id: "s1",
			Kind: &workflowsv1.Step_Activity{Activity: &workflowsv1.Activity{
				Kind: &workflowsv1.Activity_Set{Set: &workflowsv1.SetActivity{
					Assignments: map[string]string{varName: celExpr},
				}},
			}},
		}}},
	}
}

// TestE2E_Workflow_CRUD covers create→get→list→update→delete, pinning that a
// workflow is created OWNED, version-less (no promoted pointer), and that a
// delete with no runs succeeds cleanly.
func TestE2E_Workflow_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithWorkflowsServer())
	owned := h.SeedOwnedOrg(t, "wkf-crud", "WF Co", "workflows")
	ctx := context.Background()
	client := workflowsv1.NewWorkflowsClient(h.Conn())

	cfg, err := structpb.NewStruct(map[string]any{"env": "prod"})
	require.NoError(t, err)

	created, err := client.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     "organizations/" + owned.Slug,
		WorkflowId: "nightly-ingest",
		Workflow: &workflowsv1.Workflow{
			DisplayName: "Nightly Ingest",
			Description: "runs every night",
			Enabled:     true,
			Config:      cfg,
			Annotations: map[string]string{"team": "playout"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "Nightly Ingest", created.GetDisplayName())
	assert.True(t, created.GetEnabled())
	assert.Equal(t, workflowsv1.WorkflowOrigin_OWNED, created.GetOrigin(), "workflows are created OWNED")
	assert.Empty(t, created.GetVersion(), "a new workflow has no promoted version")
	// The resource name's leaf is the user-assigned slug, not the internal uuid.
	assert.Equal(t, "organizations/"+owned.Slug+"/workflows/nightly-ingest", created.GetName())
	require.NotEmpty(t, created.GetName())
	require.NotEmpty(t, created.GetEtag())
	assert.Equal(t, "prod", created.GetConfig().GetFields()["env"].GetStringValue())

	fetched, err := client.GetWorkflow(ctx, &workflowsv1.GetWorkflowRequest{Name: created.GetName()})
	require.NoError(t, err)
	assert.Equal(t, created.GetName(), fetched.GetName())
	assert.Empty(t, fetched.GetVersion())

	listed, err := client.ListWorkflows(ctx, &workflowsv1.ListWorkflowsRequest{Parent: "organizations/" + owned.Slug})
	require.NoError(t, err)
	require.Len(t, listed.GetWorkflows(), 1)
	assert.Equal(t, created.GetName(), listed.GetWorkflows()[0].GetName())

	updated, err := client.UpdateWorkflow(ctx, &workflowsv1.UpdateWorkflowRequest{
		Workflow: &workflowsv1.Workflow{Name: created.GetName(), DisplayName: "Nightly Ingest (v2)", Enabled: false},
	})
	require.NoError(t, err)
	assert.Equal(t, "Nightly Ingest (v2)", updated.GetDisplayName())
	assert.False(t, updated.GetEnabled())
	assert.NotEqual(t, created.GetEtag(), updated.GetEtag())

	_, err = client.DeleteWorkflow(ctx, &workflowsv1.DeleteWorkflowRequest{Name: created.GetName()})
	require.NoError(t, err, "delete with no runs must succeed")
	_, err = client.GetWorkflow(ctx, &workflowsv1.GetWorkflowRequest{Name: created.GetName()})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestE2E_Workflow_CreateRejectsManaged pins that a client cannot create a
// MANAGED workflow — MANAGED is Pivox-provisioned.
func TestE2E_Workflow_CreateRejectsManaged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithWorkflowsServer())
	owned := h.SeedOwnedOrg(t, "wf-mgd", "WF Managed", "workflows")
	ctx := context.Background()
	client := workflowsv1.NewWorkflowsClient(h.Conn())

	_, err := client.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     "organizations/" + owned.Slug,
		WorkflowId: "sys",
		Workflow:   &workflowsv1.Workflow{DisplayName: "System", Origin: workflowsv1.WorkflowOrigin_MANAGED},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestE2E_Workflow_ValidateOnly pins the AIP validate_only contract: a would-
// fail request still fails, but nothing persists.
func TestE2E_Workflow_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithWorkflowsServer())
	owned := h.SeedOwnedOrg(t, "wf-vo", "WF VO", "workflows")
	ctx := context.Background()
	client := workflowsv1.NewWorkflowsClient(h.Conn())

	dry, err := client.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:       "organizations/" + owned.Slug,
		WorkflowId:   "dry",
		ValidateOnly: true,
		Workflow:     &workflowsv1.Workflow{DisplayName: "Dry"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Dry", dry.GetDisplayName())

	// Nothing persisted → a live Create can reuse the id.
	_, err = client.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     "organizations/" + owned.Slug,
		WorkflowId: "dry",
		Workflow:   &workflowsv1.Workflow{},
	})
	require.NoError(t, err, "validate_only must not have persisted the workflow")

	// A dry-run that WOULD fail live (duplicate id) fails.
	_, err = client.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:       "organizations/" + owned.Slug,
		WorkflowId:   "dry",
		ValidateOnly: true,
		Workflow:     &workflowsv1.Workflow{},
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

// TestE2E_Workflow_ScopeIsolation pins that a workflow can't be read or deleted
// through a different org's name prefix. The slug leaf is unique only within
// its parent, so naming another org's workflow here resolves to no row.
func TestE2E_Workflow_ScopeIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithWorkflowsServer())
	h.SeedOwnedOrg(t, "wiso-a", "Iso A", "iso")
	ctx := context.Background()

	op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "wiso-b",
		Organization:   &apiv1.Organization{DisplayName: "Iso B"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	client := workflowsv1.NewWorkflowsClient(h.Conn())
	_, err = client.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     "organizations/wiso-b",
		WorkflowId: "b-wf",
		Workflow:   &workflowsv1.Workflow{},
	})
	require.NoError(t, err)

	crossName := "organizations/wiso-a/workflows/b-wf"
	_, err = client.GetWorkflow(ctx, &workflowsv1.GetWorkflowRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope read must be NotFound")

	_, err = client.DeleteWorkflow(ctx, &workflowsv1.DeleteWorkflowRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope delete must be NotFound")
}

// TestE2E_WorkflowVersion_CreateAndMonotonic pins that versions are numbered
// monotonically per workflow and listed under the parent.
func TestE2E_WorkflowVersion_CreateAndMonotonic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithWorkflowsServer(),
		grpcharness.WithWorkflowVersionsServer())
	owned := h.SeedOwnedOrg(t, "wkf-ver", "WFV Co", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())

	wf, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     "organizations/" + owned.Slug,
		WorkflowId: "flow",
		Workflow:   &workflowsv1.Workflow{},
	})
	require.NoError(t, err)

	for n := 1; n <= 3; n++ {
		v, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
			Parent:          wf.GetName(),
			WorkflowVersion: setStepDefinition("p"+strconv.Itoa(n), "v", `"x"`),
		})
		require.NoError(t, err)
		assert.True(t, strings.HasSuffix(v.GetName(), "/versions/"+strconv.Itoa(n)),
			"version %d name should end in /versions/%d, got %s", n, n, v.GetName())
		require.Len(t, v.GetParameters(), 1)
		require.Len(t, v.GetRoot().GetSteps(), 1)
	}

	listed, err := verClient.ListWorkflowVersions(ctx, &workflowsv1.ListWorkflowVersionsRequest{Parent: wf.GetName()})
	require.NoError(t, err)
	require.Len(t, listed.GetWorkflowVersions(), 3)

	// Get version 2 by its numbered name.
	v2, err := verClient.GetWorkflowVersion(ctx, &workflowsv1.GetWorkflowVersionRequest{Name: wf.GetName() + "/versions/2"})
	require.NoError(t, err)
	assert.Equal(t, wf.GetName()+"/versions/2", v2.GetName())
}

// TestE2E_WorkflowVersion_ErrorSequenceRoundTrips pins that a version's
// workflow-level error_sequence survives the write→read round-trip: it must be
// persisted by the create path and re-emitted on read (the canvas "on error"
// region depends on it). Regression guard for the silent-drop bug.
func TestE2E_WorkflowVersion_ErrorSequenceRoundTrips(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithWorkflowsServer(),
		grpcharness.WithWorkflowVersionsServer())
	owned := h.SeedOwnedOrg(t, "wkf-errseq", "WF ErrSeq", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())

	wf, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent:     "organizations/" + owned.Slug,
		WorkflowId: "flow",
		Workflow:   &workflowsv1.Workflow{},
	})
	require.NoError(t, err)

	// A version carrying BOTH a root and a distinct error_sequence (the handler
	// that runs when an uncaught failure bubbles to the workflow level).
	def := setStepDefinition("p", "v", `"x"`)
	def.ErrorSequence = &workflowsv1.Sequence{Steps: []*workflowsv1.Step{{
		Id: "on-error",
		Kind: &workflowsv1.Step_Activity{Activity: &workflowsv1.Activity{
			Kind: &workflowsv1.Activity_Set{Set: &workflowsv1.SetActivity{
				Assignments: map[string]string{"handled": "true"},
			}},
		}},
	}}}

	created, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
		Parent:          wf.GetName(),
		WorkflowVersion: def,
	})
	require.NoError(t, err)
	// The create response must echo the error_sequence back.
	require.NotNil(t, created.GetErrorSequence(), "create must return the error_sequence")
	require.Len(t, created.GetErrorSequence().GetSteps(), 1)
	assert.Equal(t, "on-error", created.GetErrorSequence().GetSteps()[0].GetId())

	// And a fresh Get must re-emit it intact (proves it was PERSISTED, not just
	// reflected from the request).
	got, err := verClient.GetWorkflowVersion(ctx, &workflowsv1.GetWorkflowVersionRequest{Name: created.GetName()})
	require.NoError(t, err)
	require.NotNil(t, got.GetErrorSequence(), "get must return the persisted error_sequence")
	require.Len(t, got.GetErrorSequence().GetSteps(), 1)
	assert.Equal(t, "on-error", got.GetErrorSequence().GetSteps()[0].GetId())
	assert.True(t, proto.Equal(def.GetErrorSequence(), got.GetErrorSequence()),
		"the round-tripped error_sequence must equal what was written")
}

// TestE2E_Workflow_Promote covers the happy promote (the container's version
// pointer becomes the promoted version's numbered name) and a cross-workflow
// promote (a version of another workflow) → FailedPrecondition.
func TestE2E_Workflow_Promote(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithWorkflowsServer(),
		grpcharness.WithWorkflowVersionsServer())
	owned := h.SeedOwnedOrg(t, "wkf-prom", "WFP Co", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())

	wfA, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent: "organizations/" + owned.Slug, WorkflowId: "a", Workflow: &workflowsv1.Workflow{},
	})
	require.NoError(t, err)
	verA, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
		Parent: wfA.GetName(), WorkflowVersion: setStepDefinition("p", "v", `"x"`),
	})
	require.NoError(t, err)

	promoted, err := wfClient.PromoteWorkflowVersion(ctx, &workflowsv1.PromoteWorkflowVersionRequest{
		Name: wfA.GetName(), Version: verA.GetName(),
	})
	require.NoError(t, err)
	assert.Equal(t, verA.GetName(), promoted.GetVersion(), "promoted version pointer should be the version's name")

	// A version belonging to a different workflow cannot be promoted onto A.
	wfB, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent: "organizations/" + owned.Slug, WorkflowId: "b", Workflow: &workflowsv1.Workflow{},
	})
	require.NoError(t, err)
	verB, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
		Parent: wfB.GetName(), WorkflowVersion: setStepDefinition("p", "v", `"x"`),
	})
	require.NoError(t, err)

	_, err = wfClient.PromoteWorkflowVersion(ctx, &workflowsv1.PromoteWorkflowVersionRequest{
		Name: wfA.GetName(), Version: verB.GetName(),
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err),
		"promoting another workflow's version must be FailedPrecondition")
}

// TestE2E_Workflow_Fork pins that a fork clones the source's live definition
// into a new OWNED workflow left as a DRAFT (empty version pointer), carrying
// the source's config.
func TestE2E_Workflow_Fork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithWorkflowsServer(),
		grpcharness.WithWorkflowVersionsServer())
	owned := h.SeedOwnedOrg(t, "wkf-fork", "WFF Co", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())

	cfg, err := structpb.NewStruct(map[string]any{"region": "us"})
	require.NoError(t, err)
	src, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent: "organizations/" + owned.Slug, WorkflowId: "src",
		Workflow: &workflowsv1.Workflow{DisplayName: "Source", Config: cfg},
	})
	require.NoError(t, err)

	def := setStepDefinition("token", "greeting", `"hello"`)
	srcVer, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
		Parent: src.GetName(), WorkflowVersion: def,
	})
	require.NoError(t, err)
	_, err = wfClient.PromoteWorkflowVersion(ctx, &workflowsv1.PromoteWorkflowVersionRequest{
		Name: src.GetName(), Version: srcVer.GetName(),
	})
	require.NoError(t, err)

	forked, err := wfClient.ForkWorkflow(ctx, &workflowsv1.ForkWorkflowRequest{
		Name:        src.GetName(),
		Parent:      "organizations/" + owned.Slug,
		WorkflowId:  "forked",
		DisplayName: "Forked",
	})
	require.NoError(t, err)
	assert.Equal(t, workflowsv1.WorkflowOrigin_OWNED, forked.GetOrigin())
	assert.Equal(t, "Forked", forked.GetDisplayName())
	assert.Empty(t, forked.GetVersion(), "a fork is left a DRAFT — no promoted version")
	assert.Equal(t, "us", forked.GetConfig().GetFields()["region"].GetStringValue(),
		"fork carries the source's config")
	assert.NotEqual(t, src.GetName(), forked.GetName())

	// The fork's version 1 definition matches the source's live version.
	forkVer, err := verClient.GetWorkflowVersion(ctx, &workflowsv1.GetWorkflowVersionRequest{
		Name: forked.GetName() + "/versions/1",
	})
	require.NoError(t, err)
	assert.True(t, proto.Equal(srcVer.GetRoot(), forkVer.GetRoot()), "fork root must match source")
	require.Len(t, forkVer.GetParameters(), 1)
	assert.Equal(t, "token", forkVer.GetParameters()[0].GetKey())
}

// TestE2E_Workflow_ForkNoPromotedVersion pins that forking a workflow with no
// promoted version and no source_version is a FailedPrecondition.
func TestE2E_Workflow_ForkNoPromotedVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithWorkflowsServer())
	owned := h.SeedOwnedOrg(t, "wfnp", "WFNP Co", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())

	src, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent: "organizations/" + owned.Slug, WorkflowId: "src", Workflow: &workflowsv1.Workflow{},
	})
	require.NoError(t, err)

	_, err = wfClient.ForkWorkflow(ctx, &workflowsv1.ForkWorkflowRequest{
		Name: src.GetName(), Parent: "organizations/" + owned.Slug, WorkflowId: "forked",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestE2E_WorkflowVersion_DeleteRefusesPromoted pins that the workflow's
// promoted version cannot be deleted, while a non-promoted one can.
func TestE2E_WorkflowVersion_DeleteRefusesPromoted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithWorkflowsServer(),
		grpcharness.WithWorkflowVersionsServer())
	owned := h.SeedOwnedOrg(t, "wkf-del", "WFD Co", "workflows")
	ctx := context.Background()
	wfClient := workflowsv1.NewWorkflowsClient(h.Conn())
	verClient := workflowsv1.NewWorkflowVersionsClient(h.Conn())

	wf, err := wfClient.CreateWorkflow(ctx, &workflowsv1.CreateWorkflowRequest{
		Parent: "organizations/" + owned.Slug, WorkflowId: "flow", Workflow: &workflowsv1.Workflow{},
	})
	require.NoError(t, err)
	v1, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
		Parent: wf.GetName(), WorkflowVersion: setStepDefinition("p", "v", `"x"`),
	})
	require.NoError(t, err)
	v2, err := verClient.CreateWorkflowVersion(ctx, &workflowsv1.CreateWorkflowVersionRequest{
		Parent: wf.GetName(), WorkflowVersion: setStepDefinition("p", "v", `"y"`),
	})
	require.NoError(t, err)

	// Promote v1, then deleting v1 (the live version) must be refused.
	_, err = wfClient.PromoteWorkflowVersion(ctx, &workflowsv1.PromoteWorkflowVersionRequest{
		Name: wf.GetName(), Version: v1.GetName(),
	})
	require.NoError(t, err)

	_, err = verClient.DeleteWorkflowVersion(ctx, &workflowsv1.DeleteWorkflowVersionRequest{Name: v1.GetName()})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err), "deleting the promoted version must be refused")

	// A non-promoted version deletes fine.
	_, err = verClient.DeleteWorkflowVersion(ctx, &workflowsv1.DeleteWorkflowVersionRequest{Name: v2.GetName()})
	require.NoError(t, err)
	_, err = verClient.GetWorkflowVersion(ctx, &workflowsv1.GetWorkflowVersionRequest{Name: v2.GetName()})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}
