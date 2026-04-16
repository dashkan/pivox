#ifndef PIVOX_AI_CHAT_CLIENT_H
#define PIVOX_AI_CHAT_CLIENT_H

#include <cstdint>
#include <functional>
#include <memory>
#include <mutex>
#include <string>

#include <grpcpp/grpcpp.h>

namespace pivox::ai_chat {

// Callback types for stream events.
using OnEvent = std::function<void(const uint8_t* bytes, size_t size)>;
using OnError = std::function<void(const std::string& msg)>;
using OnComplete = std::function<void()>;

// Callback types for unary RPCs.
using OnResponse = std::function<void(const uint8_t* bytes, size_t size)>;
using OnRpcError = std::function<void(const std::string& msg)>;

class ChatClient {
 public:
  ChatClient(const std::string& endpoint, const std::string& auth_token);
  ~ChatClient();

  // Not copyable or movable.
  ChatClient(const ChatClient&) = delete;
  ChatClient& operator=(const ChatClient&) = delete;

  void SetAuthToken(const std::string& token);

  // Opens a bidi stream. Callbacks are dispatched on the main queue.
  void StartStream(OnEvent on_event, OnError on_error,
                   OnComplete on_complete);

  // Sends a serialized ClientEvent.
  void Send(const uint8_t* bytes, size_t size);

  // Cancels the in-flight stream.
  void Cancel();

  // Sends a unary RPC. method is the full gRPC method name
  // (e.g. "/pivox.ai.v1.AiChat/ListConversations").
  void UnaryCall(const std::string& method, const uint8_t* request_bytes,
                 size_t request_size, OnResponse on_response,
                 OnRpcError on_error);

 private:
  std::shared_ptr<grpc::Channel> channel_;
  std::mutex mu_;
  std::string auth_token_;

  // Stream state managed internally via the reactor pattern.
  class StreamReactor;
  std::shared_ptr<StreamReactor> reactor_;
};

}  // namespace pivox::ai_chat

#endif  // PIVOX_AI_CHAT_CLIENT_H
