package grpcharness

import (
	"strings"

	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/appkey"
	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
	"github.com/dashkan/pivox/internal/service/mcp"
)

// WithMcpServer registers the McpServer on the harness's gRPC server
// using sane defaults pulled from the harness's Pool/Queries. Pair with
// WithOrganizationsServer / WithSpacesServer when the test seeds orgs
// and spaces to read back through the MCP surface.
func WithMcpServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, registerMcpServer)
	}
}

func registerMcpServer(h *Harness, s *grpc.Server) {
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	if err != nil {
		panic("grpcharness: hard-coded test app key is malformed: " + err.Error())
	}
	mcpv1.RegisterMcpServiceServer(s, mcp.NewMcpServer(mcp.Config{
		Pool:    h.Pool,
		Queries: h.Queries,
		Codec:   codec,
	}))
}
