#ifndef PIVOX_AUTH_AUTH_INTERCEPTOR_H
#define PIVOX_AUTH_AUTH_INTERCEPTOR_H

#include <memory>
#include <string>
#include <vector>

#include <grpcpp/grpcpp.h>
#include <grpcpp/support/client_interceptor.h>

// Client-side gRPC interceptor that attaches a Firebase Bearer token to
// every outbound RPC on the attached channel. Token is fetched via the
// registered TokenFetchFn (see token_provider.h) — async-safe, so the
// call is paused (not sent) until the provider completes. If no token
// is available the call proceeds without an Authorization header and
// the server rejects with UNAUTHENTICATED.
//
// This interceptor is reusable across ALL Pivox gRPC services (chat,
// assets, dashboards, whatever). Attach it once per channel and every
// RPC on that channel is authenticated.
namespace pivox::auth {

class AuthInterceptor final : public grpc::experimental::Interceptor {
 public:
  void Intercept(
      grpc::experimental::InterceptorBatchMethods* methods) override;
};

class AuthInterceptorFactory final
    : public grpc::experimental::ClientInterceptorFactoryInterface {
 public:
  grpc::experimental::Interceptor* CreateClientInterceptor(
      grpc::experimental::ClientRpcInfo* info) override;
};

// Convenience for new services: returns a factory list containing a
// single AuthInterceptor factory, suitable for
// `CreateCustomChannelWithInterceptors`.
std::vector<std::unique_ptr<
    grpc::experimental::ClientInterceptorFactoryInterface>>
MakeInterceptorFactories();

// One-liner for services that want the default authenticated channel:
// insecure credentials, default channel args, single auth interceptor.
// Overloads exist for callers that need custom creds or args.
std::shared_ptr<grpc::Channel> CreateAuthenticatedChannel(
    const std::string& endpoint);

std::shared_ptr<grpc::Channel> CreateAuthenticatedChannel(
    const std::string& endpoint,
    std::shared_ptr<grpc::ChannelCredentials> creds,
    const grpc::ChannelArguments& args);

}  // namespace pivox::auth

#endif  // PIVOX_AUTH_AUTH_INTERCEPTOR_H
