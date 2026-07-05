package grpcharness

import (
	"strings"

	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/appkey"
	secretsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/secrets/v1"
	"github.com/dashkan/pivox/internal/service/secrets"
)

// WithSecretsServer registers the Secrets vault service on the harness's
// gRPC server, wired to the harness's real Tink encryptor so AAD binding is
// exercised end-to-end (a fake encryptor would silently pass a mis-bound
// ciphertext).
func WithSecretsServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, registerSecretsServer)
	}
}

func registerSecretsServer(h *Harness, s *grpc.Server) {
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	if err != nil {
		panic("grpcharness: hard-coded test app key is malformed: " + err.Error())
	}
	secretsv1.RegisterSecretsServer(s, secrets.NewSecretsServer(secrets.Config{
		Pool:      h.Pool,
		Queries:   h.Queries,
		Codec:     codec,
		Encryptor: h.Encryptor,
	}))
}
