package grpcharness

import (
	"strings"

	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/appkey"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/service/workflows"
)

// WithWorkflowsServer registers the Workflows (container) service on the
// harness's gRPC server. This layer owns no plaintext, so no encryptor is
// wired here.
func WithWorkflowsServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, registerWorkflowsServer)
	}
}

func registerWorkflowsServer(h *Harness, s *grpc.Server) {
	workflowsv1.RegisterWorkflowsServer(s, workflows.NewWorkflowsServer(workflows.Config{
		Pool:    h.Pool,
		Queries: h.Queries,
		Codec:   testCodec(),
	}))
}

// WithWorkflowVersionsServer registers the WorkflowVersions (immutable
// definition) service on the harness's gRPC server.
func WithWorkflowVersionsServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, registerWorkflowVersionsServer)
	}
}

func registerWorkflowVersionsServer(h *Harness, s *grpc.Server) {
	workflowsv1.RegisterWorkflowVersionsServer(s, workflows.NewWorkflowVersionsServer(workflows.Config{
		Pool:    h.Pool,
		Queries: h.Queries,
		Codec:   testCodec(),
	}))
}

// WithWorkflowRunsServer registers the WorkflowRuns (execution) service on the
// harness's gRPC server.
func WithWorkflowRunsServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, registerWorkflowRunsServer)
	}
}

func registerWorkflowRunsServer(h *Harness, s *grpc.Server) {
	workflowsv1.RegisterWorkflowRunsServer(s, workflows.NewWorkflowRunsServer(workflows.Config{
		Pool:    h.Pool,
		Queries: h.Queries,
		Codec:   testCodec(),
		// RunWorkflow enqueues the execution job transactionally with the run's
		// INSERT, exactly as pivox-cloud does. Tests that want the job actually
		// executed register a RunWorkflowWorker via Harness.StartRiverWorkers.
		River: h.River,
	}))
}

// testCodec builds the hard-coded test app key codec shared by the workflow
// harness registrations.
func testCodec() *appkey.Codec {
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	if err != nil {
		panic("grpcharness: hard-coded test app key is malformed: " + err.Error())
	}
	return codec
}
