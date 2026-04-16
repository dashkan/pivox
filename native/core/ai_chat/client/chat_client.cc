#include "chat_client.h"

#include <grpcpp/impl/codegen/client_unary_call.h>

#include <utility>

namespace pivox::ai_chat {

// ── StreamReactor ───────────────────────────────────────────────────

StreamReactor::StreamReactor(std::unique_ptr<grpc::ClientContext> ctx,
                             OnEvent on_event, OnError on_error,
                             OnComplete on_complete)
    : on_event_(std::move(on_event)),
      on_error_(std::move(on_error)),
      on_complete_(std::move(on_complete)),
      ctx_(std::move(ctx)) {}

std::shared_ptr<StreamReactor> StreamReactor::Create(
    std::unique_ptr<grpc::ClientContext> ctx,
    OnEvent on_event, OnError on_error, OnComplete on_complete) {
  auto r = std::shared_ptr<StreamReactor>(
      new StreamReactor(std::move(ctx), std::move(on_event),
                        std::move(on_error), std::move(on_complete)));
  r->self_ = r;
  return r;
}

void StreamReactor::Start() {
  StartCall();
  StartRead(&read_buf_);
}

void StreamReactor::QueueWrite(const uint8_t* bytes, size_t size) {
  grpc::Slice slice(bytes, size);
  grpc::ByteBuffer buf(&slice, 1);

  std::lock_guard<std::mutex> lock(write_mu_);
  if (done_) return;
  write_queue_.push(std::move(buf));
  MaybeStartWrite();
}

void StreamReactor::Detach(std::function<void()> on_cancel) {
  std::lock_guard<std::mutex> lock(cb_mu_);
  on_event_ = nullptr;
  on_error_ = nullptr;
  // Fire on_complete (or on_cancel) so the caller can release retained refs.
  // This is the Swift StreamContext cleanup path on cancellation.
  if (on_cancel) {
    on_cancel();
  } else if (on_complete_) {
    on_complete_();
  }
  on_complete_ = nullptr;
  // Cancel the gRPC context to trigger OnDone quickly.
  if (ctx_) ctx_->TryCancel();
}

void StreamReactor::OnReadInitialMetadataDone(bool /*ok*/) {}

void StreamReactor::OnReadDone(bool ok) {
  if (!ok) return;

  std::vector<grpc::Slice> slices;
  read_buf_.Dump(&slices);

  std::vector<uint8_t> bytes;
  for (const auto& s : slices) {
    bytes.insert(bytes.end(), s.begin(), s.end());
  }

  {
    std::lock_guard<std::mutex> lock(cb_mu_);
    if (on_event_ && !bytes.empty()) {
      on_event_(bytes.data(), bytes.size());
    }
  }

  read_buf_.Clear();
  StartRead(&read_buf_);
}

void StreamReactor::OnWriteDone(bool ok) {
  std::lock_guard<std::mutex> lock(write_mu_);
  writing_ = false;
  if (!ok) return;
  if (!write_queue_.empty()) {
    MaybeStartWrite();
  }
}

void StreamReactor::OnDone(const grpc::Status& status) {
  {
    std::lock_guard<std::mutex> lock(write_mu_);
    done_ = true;
  }

  {
    std::lock_guard<std::mutex> lock(cb_mu_);
    if (status.ok() || status.error_code() == grpc::StatusCode::CANCELLED) {
      if (on_complete_) on_complete_();
    } else {
      if (on_error_) on_error_(status.error_message());
    }
    on_event_ = nullptr;
    on_error_ = nullptr;
    on_complete_ = nullptr;
  }

  // Release self-ownership. After this, the reactor may be deleted.
  self_.reset();
}

void StreamReactor::MaybeStartWrite() {
  if (writing_ || write_queue_.empty() || done_) return;
  writing_ = true;
  StartWrite(&write_queue_.front());
  write_queue_.pop();
}

// ── ChatClient ──────────────────────────────────────────────────────

ChatClient::ChatClient(const std::string& endpoint,
                       const std::string& auth_token)
    : channel_(grpc::CreateChannel(endpoint,
                                   grpc::InsecureChannelCredentials())),
      auth_token_(auth_token) {}

ChatClient::~ChatClient() { Shutdown(); }

void ChatClient::SetAuthToken(const std::string& token) {
  std::lock_guard<std::mutex> lock(mu_);
  auth_token_ = token;
}

void ChatClient::StartStream(OnEvent on_event, OnError on_error,
                             OnComplete on_complete) {
  Shutdown();

  {
    std::lock_guard<std::mutex> lock(mu_);
    shutdown_ = false;
    retry_count_ = 0;
    generation_++;
    stream_on_event_ = std::move(on_event);
    stream_on_error_ = std::move(on_error);
    stream_on_complete_ = std::move(on_complete);
  }

  OpenStream();
}

void ChatClient::OpenStream() {
  OnEvent event_cb;
  OnComplete complete_cb;
  {
    std::lock_guard<std::mutex> lock(mu_);
    event_cb = stream_on_event_;
    complete_cb = stream_on_complete_;
  }

  auto ctx = std::make_unique<grpc::ClientContext>();
  {
    std::lock_guard<std::mutex> lock(mu_);
    if (!auth_token_.empty()) {
      ctx->AddMetadata("authorization", "Bearer " + auth_token_);
    }
  }

  auto* raw_ctx = ctx.get();
  auto r = StreamReactor::Create(
      std::move(ctx),
      std::move(event_cb),
      [this](const std::string& msg) { HandleStreamError(msg); },
      std::move(complete_cb));

  grpc::GenericStub stub(channel_);
  stub.PrepareBidiStreamingCall(
      raw_ctx,
      "/pivox.ai.v1.AiChat/Stream",
      grpc::StubOptions(),
      r.get());

  r->Start();

  {
    std::lock_guard<std::mutex> lock(mu_);
    reactor_ = r;
  }
}

void ChatClient::HandleStreamError(const std::string& msg) {
  std::lock_guard<std::mutex> lock(mu_);

  if (shutdown_) return;

  auto state = channel_->GetState(false);
  bool transient = (state == GRPC_CHANNEL_TRANSIENT_FAILURE ||
                    state == GRPC_CHANNEL_CONNECTING ||
                    state == GRPC_CHANNEL_IDLE);

  if (transient && retry_count_ < kMaxRetries) {
    retry_count_++;
    int delay_ms = 500 * (1 << (retry_count_ - 1));
    uint64_t gen = ++generation_;

    if (retry_thread_.joinable()) {
      mu_.unlock();
      retry_thread_.join();
      mu_.lock();
      if (shutdown_) return;
    }

    retry_thread_ = std::thread([this, delay_ms, gen]() {
      std::unique_lock<std::mutex> lk(mu_);
      cv_.wait_for(lk, std::chrono::milliseconds(delay_ms), [this, gen]() {
        return shutdown_ || generation_ != gen;
      });

      if (!shutdown_ && generation_ == gen) {
        lk.unlock();
        OpenStream();
      }
    });
  } else {
    auto cb = stream_on_error_;
    mu_.unlock();
    if (cb) cb(msg);
    mu_.lock();
  }
}

void ChatClient::Shutdown() {
  std::shared_ptr<StreamReactor> reactor_to_detach;

  {
    std::lock_guard<std::mutex> lock(mu_);
    if (shutdown_) return;
    shutdown_ = true;
    generation_++;
    reactor_to_detach = reactor_;
    reactor_.reset();
    stream_on_event_ = nullptr;
    stream_on_error_ = nullptr;
    stream_on_complete_ = nullptr;
  }

  // Detach callbacks + cancel context + fire on_cancel for Swift cleanup.
  // The reactor owns the ClientContext — it's deleted in OnDone via self_.
  if (reactor_to_detach) {
    reactor_to_detach->Detach();
  }

  // Wake and join the retry thread.
  cv_.notify_all();
  if (retry_thread_.joinable()) {
    retry_thread_.join();
  }
}

void ChatClient::Cancel() { Shutdown(); }

void ChatClient::Send(const uint8_t* bytes, size_t size) {
  std::shared_ptr<StreamReactor> r;
  {
    std::lock_guard<std::mutex> lock(mu_);
    r = reactor_;
  }
  if (r) {
    r->QueueWrite(bytes, size);
  }
}

// ── UnaryCall ───────────────────────────────────────────────────────

void ChatClient::UnaryCall(const std::string& method,
                           const uint8_t* request_bytes,
                           size_t request_size, OnResponse on_response,
                           OnRpcError on_error) {
  std::string token;
  {
    std::lock_guard<std::mutex> lock(mu_);
    token = auth_token_;
  }

  std::vector<uint8_t> req_bytes(request_bytes, request_bytes + request_size);

  auto channel = channel_;
  std::thread([channel, method, req_bytes = std::move(req_bytes),
               token = std::move(token), on_response, on_error]() {
    grpc::ClientContext ctx;
    if (!token.empty()) {
      ctx.AddMetadata("authorization", "Bearer " + token);
    }
    ctx.set_deadline(std::chrono::system_clock::now() +
                     std::chrono::seconds(30));

    grpc::Slice req_slice(req_bytes.data(), req_bytes.size());
    grpc::ByteBuffer request_buf(&req_slice, 1);
    grpc::ByteBuffer response_buf;

    grpc::internal::RpcMethod rpc_method(
        method.c_str(), nullptr,
        grpc::internal::RpcMethod::NORMAL_RPC);

    auto status = grpc::internal::BlockingUnaryCall<
        grpc::ByteBuffer, grpc::ByteBuffer>(
        channel.get(), rpc_method, &ctx, request_buf, &response_buf);

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
