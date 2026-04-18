#include "auth_interceptor.h"

#include <optional>
#include <string>
#include <utility>

#include "token_provider.h"

namespace pivox::auth {

void AuthInterceptor::Intercept(
    grpc::experimental::InterceptorBatchMethods* methods) {
  // We only care about the outbound initial-metadata hook — that's where
  // the Authorization header goes. Every other hook point (send-message,
  // recv-initial-metadata, etc.) proceeds immediately.
  if (!methods->QueryInterceptionHookPoint(
          grpc::experimental::InterceptionHookPoints::
              PRE_SEND_INITIAL_METADATA)) {
    methods->Proceed();
    return;
  }

  // Fetch a token asynchronously. Once the provider completes we mutate
  // the outbound metadata and resume the RPC. gRPC keeps `methods` valid
  // until Proceed() is called, so deferring to another thread is safe.
  FetchToken([methods](std::optional<std::string> token) {
    if (token.has_value() && !token->empty()) {
      auto* md = methods->GetSendInitialMetadata();
      md->insert(std::make_pair(
          std::string("authorization"),
          std::string("Bearer ") + *token));
    }
    methods->Proceed();
  });
}

grpc::experimental::Interceptor*
AuthInterceptorFactory::CreateClientInterceptor(
    grpc::experimental::ClientRpcInfo* /*info*/) {
  return new AuthInterceptor();
}

std::vector<std::unique_ptr<
    grpc::experimental::ClientInterceptorFactoryInterface>>
MakeInterceptorFactories() {
  std::vector<std::unique_ptr<
      grpc::experimental::ClientInterceptorFactoryInterface>>
      factories;
  factories.push_back(std::make_unique<AuthInterceptorFactory>());
  return factories;
}

std::shared_ptr<grpc::Channel> CreateAuthenticatedChannel(
    const std::string& endpoint) {
  return CreateAuthenticatedChannel(
      endpoint, grpc::InsecureChannelCredentials(), grpc::ChannelArguments());
}

std::shared_ptr<grpc::Channel> CreateAuthenticatedChannel(
    const std::string& endpoint,
    std::shared_ptr<grpc::ChannelCredentials> creds,
    const grpc::ChannelArguments& args) {
  return grpc::experimental::CreateCustomChannelWithInterceptors(
      endpoint, std::move(creds), args, MakeInterceptorFactories());
}

}  // namespace pivox::auth
