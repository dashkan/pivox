#ifndef PIVOX_AI_CHAT_CLIENT_H
#define PIVOX_AI_CHAT_CLIENT_H

#include <atomic>
#include <cstdint>
#include <condition_variable>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

#include <grpcpp/grpcpp.h>
#include <grpcpp/generic/generic_stub.h>

namespace pivox::ai_chat {

using OnEvent = std::function<void(const uint8_t* bytes, size_t size)>;
using OnError = std::function<void(const std::string& msg)>;
using OnComplete = std::function<void()>;

using OnResponse = std::function<void(const uint8_t* bytes, size_t size)>;
using OnRpcError = std::function<void(const std::string& msg)>;

// Server-stream reactor using bidi reactor with immediate half-close.
// Self-owning via shared_ptr released in OnDone.
class StreamReactor
    : public grpc::ClientBidiReactor<grpc::ByteBuffer, grpc::ByteBuffer> {
 public:
  static std::shared_ptr<StreamReactor> Create(
      std::unique_ptr<grpc::ClientContext> ctx,
      const grpc::ByteBuffer& request,
      OnEvent on_event, OnError on_error, OnComplete on_complete);

  void Start();

  void Detach(std::function<void()> on_cancel = nullptr);

  void OnWriteDone(bool ok) override;
  void OnReadDone(bool ok) override;
  void OnDone(const grpc::Status& status) override;

 private:
  StreamReactor(std::unique_ptr<grpc::ClientContext> ctx,
                const grpc::ByteBuffer& request,
                OnEvent on_event, OnError on_error, OnComplete on_complete);

  std::mutex cb_mu_;
  OnEvent on_event_;
  OnError on_error_;
  OnComplete on_complete_;

  std::unique_ptr<grpc::ClientContext> ctx_;
  std::shared_ptr<StreamReactor> self_;

  grpc::ByteBuffer request_;
  grpc::ByteBuffer read_buf_;
};

class ChatClient {
 public:
  ChatClient(const std::string& endpoint, const std::string& auth_token);
  ~ChatClient();

  ChatClient(const ChatClient&) = delete;
  ChatClient& operator=(const ChatClient&) = delete;

  void SetAuthToken(const std::string& token);

  // Opens a server-streaming call. request_bytes is the serialized
  // ClientEvent. Responses stream back via on_event.
  void StartStream(const uint8_t* request_bytes, size_t request_size,
                   OnEvent on_event, OnError on_error,
                   OnComplete on_complete);
  void Cancel();

  void UnaryCall(const std::string& method, const uint8_t* request_bytes,
                 size_t request_size, OnResponse on_response,
                 OnRpcError on_error);

 private:
  void OpenStream();
  void HandleStreamError(const std::string& msg);
  void Shutdown();

  std::shared_ptr<grpc::Channel> channel_;

  std::mutex mu_;
  std::condition_variable cv_;
  std::string auth_token_;
  std::shared_ptr<StreamReactor> reactor_;

  OnEvent stream_on_event_;
  OnError stream_on_error_;
  OnComplete stream_on_complete_;
  std::vector<uint8_t> stream_request_;

  std::thread retry_thread_;
  int retry_count_ = 0;
  uint64_t generation_ = 0;
  bool shutdown_ = false;

  static constexpr int kMaxRetries = 3;
};

}  // namespace pivox::ai_chat

#endif  // PIVOX_AI_CHAT_CLIENT_H
