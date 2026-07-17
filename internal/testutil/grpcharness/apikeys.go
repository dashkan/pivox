package grpcharness

import (
	"strings"

	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/appkey"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/service/apikeys"
)

// WithApiKeysServer registers the ApiKeysServer on the harness's gRPC server
// using sane defaults pulled from the harness's Pool/Queries/Codec. Pair with
// WithOrganizationsServer when the test seeds an org first: the ApiKeys RPCs
// are org-scoped and the founder owner binding is what satisfies the
// membership/permission interceptors.
func WithApiKeysServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, registerApiKeysServer)
	}
}

func registerApiKeysServer(h *Harness, s *grpc.Server) {
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	if err != nil {
		panic("grpcharness: hard-coded test app key is malformed: " + err.Error())
	}
	apiv1.RegisterApiKeysServer(s, apikeys.NewApiKeysServer(apikeys.Config{
		Pool:    h.Pool,
		Queries: h.Queries,
		Codec:   codec,
	}))
}
