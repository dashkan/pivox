package grpcharness

import (
	"strings"

	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/appkey"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/service/connectors"
)

// WithConnectorsServer registers the Connectors service on the harness's gRPC
// server. Connectors reference vault Secrets rather than holding plaintext, so
// no encryptor is wired here.
func WithConnectorsServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, registerConnectorsServer)
	}
}

func registerConnectorsServer(h *Harness, s *grpc.Server) {
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	if err != nil {
		panic("grpcharness: hard-coded test app key is malformed: " + err.Error())
	}
	workflowsv1.RegisterConnectorsServer(s, connectors.NewConnectorsServer(connectors.Config{
		Pool:    h.Pool,
		Queries: h.Queries,
		Codec:   codec,
	}))
}
