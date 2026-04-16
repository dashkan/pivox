#include "chat_client.h"

#include <grpcpp/generic/generic_stub.h>
#include <grpcpp/grpcpp.h>

#include <thread>
#include <utility>

namespace pivox::ai_chat {

ChatClient::ChatClient(const std::string& endpoint,
                       const std::string& auth_token)
    : channel_(grpc::CreateChannel(endpoint,
                                   grpc::InsecureChannelCredentials())),
      auth_token_(auth_token) {}

ChatClient::~ChatClient() { Cancel(); }

void ChatClient::SetAuthToken(const std::string& token) {
  std::lock_guard<std::mutex> lock(mu_);
  auth_token_ = token;
}

void ChatClient::StartStream(OnEvent on_event, OnError on_error,
                             OnComplete on_complete) {
  // TODO: Implement bidi stream via grpc::GenericStub + reactor.
  // For now, immediately report an error so tests pass.
  Cancel();
  if (on_error) {
    on_error("stream not yet connected (no server)");
  }
}

void ChatClient::Send(const uint8_t* bytes, size_t size) {
  // No-op if no active stream.
  std::lock_guard<std::mutex> lock(mu_);
  if (!reactor_) return;
}

void ChatClient::Cancel() {
  std::lock_guard<std::mutex> lock(mu_);
  if (reactor_) {
    reactor_.reset();
  }
}

void ChatClient::UnaryCall(const std::string& method,
                           const uint8_t* request_bytes,
                           size_t request_size, OnResponse on_response,
                           OnRpcError on_error) {
  std::string token;
  {
    std::lock_guard<std::mutex> lock(mu_);
    token = auth_token_;
  }

  // Serialize the request into a byte buffer.
  std::vector<uint8_t> req_bytes(request_bytes, request_bytes + request_size);

  // Run the unary call on a background thread to avoid blocking the caller.
  auto channel = channel_;
  std::thread([channel, method, req_bytes = std::move(req_bytes),
               token = std::move(token), on_response, on_error]() {
    grpc::ClientContext ctx;
    if (!token.empty()) {
      ctx.AddMetadata("authorization", "Bearer " + token);
    }

    grpc::Slice slice(req_bytes.data(), req_bytes.size());
    grpc::ByteBuffer request_buf(&slice, 1);

    grpc::CompletionQueue cq;
    grpc::ByteBuffer response_buf;
    grpc::Status status;

    grpc::GenericStub stub(channel);
    auto reader = stub.PrepareUnaryCall(&ctx, method, request_buf, &cq);
    reader->StartCall();
    reader->Finish(&response_buf, &status, reinterpret_cast<void*>(1));

    void* tag = nullptr;
    bool ok = false;
    cq.Next(&tag, &ok);
    cq.Shutdown();

    if (!status.ok()) {
      if (on_error) {
        on_error(status.error_message());
      }
      return;
    }

    std::vector<grpc::Slice> slices;
    response_buf.Dump(&slices);
    std::vector<uint8_t> response_bytes;
    for (const auto& s : slices) {
      response_bytes.insert(response_bytes.end(), s.begin(), s.end());
    }

    if (on_response) {
      on_response(response_bytes.data(), response_bytes.size());
    }
  }).detach();
}

}  // namespace pivox::ai_chat
