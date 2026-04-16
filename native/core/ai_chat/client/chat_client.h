#ifndef PIVOX_AI_CHAT_CLIENT_H
#define PIVOX_AI_CHAT_CLIENT_H

#include <atomic>
#include <cstdint>
#include <condition_variable>
#include <functional>
#include <memory>
#include <mutex>
#include <queue>
#include <string>
#include <thread>

#include <grpcpp/grpcpp.h>
#include <grpcpp/generic/generic_stub.h>

namespace pivox::ai_chat {

using OnEvent = std::function<void(const uint8_t* bytes, size_t size)>;
using OnError = std::function<void(const std::string& msg)>;
using OnComplete = std::function<void()>;

using OnResponse = std::function<void(const uint8_t* bytes, size_t size)>;
using OnRpcError = std::function<void(const std::string& msg)>;

// Self-owning bidi stream reactor. Holds a shared_ptr to itself which is
// released in OnDone — guarantees the reactor stays alive until gRPC is
// done with it. Callbacks can be detached safely via Detach().
class StreamReactor
    : public grpc::ClientBidiReactor<grpc::ByteBuffer, grpc::ByteBuffer> {
 public:
  // Use Create() instead of constructing directly — sets up self-ownership.
  static std::shared_ptr<StreamReactor> Create(
      std::unique_ptr<grpc::ClientContext> ctx,
      OnEvent on_event, OnError on_error, OnComplete on_complete);

  void Start();
  void QueueWrite(const uint8_t* bytes, size_t size);

  // Clears all callbacks under lock and cancels the gRPC context.
  // After Detach(), OnDone will not call into the ChatClient.
  // on_cancel is invoked exactly once so Swift can release retained refs.
  void Detach(std::function<void()> on_cancel = nullptr);

  void OnReadInitialMetadataDone(bool ok) override;
  void OnReadDone(bool ok) override;
  void OnWriteDone(bool ok) override;
  void OnDone(const grpc::Status& status) override;

 private:
  StreamReactor(std::unique_ptr<grpc::ClientContext> ctx,
                OnEvent on_event, OnError on_error, OnComplete on_complete);
  void MaybeStartWrite();

  std::mutex cb_mu_;
  OnEvent on_event_;
  OnError on_error_;
  OnComplete on_complete_;

  // Owns the gRPC context — deleted when reactor self-destructs in OnDone.
  std::unique_ptr<grpc::ClientContext> ctx_;

  // Self-ownership: released in OnDone after all callbacks are done.
  std::shared_ptr<StreamReactor> self_;

  grpc::ByteBuffer read_buf_;

  std::mutex write_mu_;
  std::queue<grpc::ByteBuffer> write_queue_;
  bool writing_ = false;
  bool done_ = false;
};

class ChatClient {
 public:
  ChatClient(const std::string& endpoint, const std::string& auth_token);
  ~ChatClient();

  ChatClient(const ChatClient&) = delete;
  ChatClient& operator=(const ChatClient&) = delete;

  void SetAuthToken(const std::string& token);

  void StartStream(OnEvent on_event, OnError on_error,
                   OnComplete on_complete);
  void Send(const uint8_t* bytes, size_t size);
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

  // Stored callbacks for retry.
  OnEvent stream_on_event_;
  OnError stream_on_error_;
  OnComplete stream_on_complete_;

  // Retry state.
  std::thread retry_thread_;
  int retry_count_ = 0;
  uint64_t generation_ = 0;
  bool shutdown_ = false;

  static constexpr int kMaxRetries = 3;
};

}  // namespace pivox::ai_chat

#endif  // PIVOX_AI_CHAT_CLIENT_H
