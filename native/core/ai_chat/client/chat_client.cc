#include "chat_client.h"

#include <grpcpp/impl/codegen/client_unary_call.h>

#include <utility>

#include "auth_interceptor.h"

namespace pivox::ai_chat {

// ── StreamReactor ───────────────────────────────────────────────────

StreamReactor::StreamReactor(std::unique_ptr<grpc::ClientContext> ctx,
                             const grpc::ByteBuffer& request,
                             OnEvent on_event, OnError on_error,
                             OnComplete on_complete)
    : on_event_(std::move(on_event)),
      on_error_(std::move(on_error)),
      on_complete_(std::move(on_complete)),
      ctx_(std::move(ctx)),
      request_(request) {}

std::shared_ptr<StreamReactor> StreamReactor::Create(
    std::unique_ptr<grpc::ClientContext> ctx,
    const grpc::ByteBuffer& request,
    OnEvent on_event, OnError on_error, OnComplete on_complete) {
  auto r = std::shared_ptr<StreamReactor>(
      new StreamReactor(std::move(ctx), request, std::move(on_event),
                        std::move(on_error), std::move(on_complete)));
  r->self_ = r;
  return r;
}

void StreamReactor::Start() {
  StartCall();
  StartWrite(&request_);
}

void StreamReactor::OnWriteDone(bool ok) {
  if (ok) {
    StartWritesDone();
    StartRead(&read_buf_);
  }
}

void StreamReactor::Detach() {
  std::lock_guard<std::mutex> lock(cb_mu_);
  on_event_ = nullptr;
  on_error_ = nullptr;
  if (on_complete_) {
    on_complete_();
  }
  on_complete_ = nullptr;
  if (ctx_) ctx_->TryCancel();
}

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

void StreamReactor::OnDone(const grpc::Status& status) {
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

  self_.reset();
}

// ── ChatClient ──────────────────────────────────────────────────────

SWIFT_RETURNS_RETAINED ChatClient* ChatClient::Create(const char* endpoint) {
  if (!endpoint) return nullptr;
  return new (std::nothrow) ChatClient(endpoint);
}

ChatClient::ChatClient(const std::string& endpoint)
    : channel_(pivox::auth::CreateAuthenticatedChannel(endpoint)) {}

ChatClient::~ChatClient() { Shutdown(); }

void ChatClient::Cancel() { Shutdown(); }

// ── Internal bytes-based transport (shared w/ future WinRT typed path) ──

void ChatClient::StartStreamBytes(const uint8_t* request_bytes,
                                  size_t request_size,
                                  OnEvent on_event, OnError on_error,
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
    stream_request_.assign(request_bytes, request_bytes + request_size);
  }

  OpenStream();
}

grpc::Status ChatClient::DoUnaryCall(const char* method,
                                       const uint8_t* request_bytes,
                                       size_t request_size,
                                       std::vector<uint8_t>* response_out) {
  // Authorization header is attached by the channel's auth interceptor.
  grpc::ClientContext rpc_ctx;
  rpc_ctx.set_deadline(std::chrono::system_clock::now() +
                       std::chrono::seconds(30));

  grpc::Slice req_slice(request_bytes, request_size);
  grpc::ByteBuffer request_buf(&req_slice, 1);
  grpc::ByteBuffer response_buf;

  grpc::internal::RpcMethod rpc_method(
      method, nullptr, grpc::internal::RpcMethod::NORMAL_RPC);

  auto status = grpc::internal::BlockingUnaryCall<
      grpc::ByteBuffer, grpc::ByteBuffer>(
      channel_.get(), rpc_method, &rpc_ctx, request_buf, &response_buf);

  if (status.ok() && response_out) {
    std::vector<grpc::Slice> slices;
    response_buf.Dump(&slices);
    response_out->clear();
    for (const auto& s : slices) {
      response_out->insert(response_out->end(), s.begin(), s.end());
    }
  }
  return status;
}

void ChatClient::OpenStream() {
  OnEvent event_cb;
  OnComplete complete_cb;
  std::vector<uint8_t> req;
  {
    std::lock_guard<std::mutex> lock(mu_);
    event_cb = stream_on_event_;
    complete_cb = stream_on_complete_;
    req = stream_request_;
  }

  // Authorization header is attached by the channel's auth interceptor.
  auto rpc_ctx = std::make_unique<grpc::ClientContext>();

  grpc::Slice req_slice(req.data(), req.size());
  grpc::ByteBuffer request_buf(&req_slice, 1);

  auto* raw_ctx = rpc_ctx.get();
  auto r = StreamReactor::Create(
      std::move(rpc_ctx), request_buf,
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

  if (reactor_to_detach) {
    reactor_to_detach->Detach();
  }

  cv_.notify_all();
  if (retry_thread_.joinable()) {
    retry_thread_.join();
  }
}

#if PIVOX_AI_CHAT_HAS_SWIFT_PROTOS

// ── Apple typed entry points ────────────────────────────────────────

// Copy a Swift `Array<UInt8>` into a C++ vector. Indexed access via
// `operator[]` since swift::Array doesn't expose a raw buffer pointer —
// each element crosses the Swift↔C++ FFI boundary individually (~30ns
// each, ~30μs per KB). Fine for our traffic: chat payloads are <100KB
// (<3ms), and asset-backed content is referenced by URL, not inlined.
//
// TODO(perf): revisit if a message type ever inlines large binary
// fields. Options, roughly in order of how invasive they are:
//   (A) Callback-based: Swift hands C++ a `(ptr,len) -> void` closure
//       pointing at its contiguous storage via withUnsafeBufferPointer.
//       Requires swift-protobuf serializing into a preallocated buffer,
//       which isn't its public API today.
//   (B) Bytes at the facade layer: facade serializes to Data, hands C++
//       raw bytes; bridge takes `const uint8_t*, size_t`. Skips
//       `swift::Array` entirely. Costs us the typed-param ergonomics
//       that were the whole point of this pipeline.
//   (C) Stateful wrapper pattern (size() then serialize()): blocked —
//       swift-protobuf's `serializedDataSize()` is `internal`, not
//       public. Would require forking or upstream PR.
static std::vector<uint8_t> SwiftArrayToVector(
    const swift::Array<uint8_t>& arr) {
  auto count = arr.getCount();
  std::vector<uint8_t> out;
  out.reserve(count);
  for (swift::Int i = 0; i < count; ++i) {
    out.push_back(arr[i]);
  }
  return out;
}

// Per-RPC Apple bridge method implementations are generated by
// protoc-gen-pivox-cpp-bridge. Regenerate via `make proto-generate-native`.
#include "ai_chat_bridge.cc.inc"

void ChatClient::UnaryCallBytes(
    const char* method,
    const uint8_t* request_bytes, size_t request_size,
    void* ctx,
    void (*on_response)(void* ctx, const uint8_t* bytes, size_t size),
    void (*on_error)(void* ctx, int32_t code, const char* message)) {
  if (!method) {
    if (on_error) on_error(ctx, 3 /* INVALID_ARGUMENT */, "null method");
    return;
  }

  // Copy inputs so the async thread owns them.
  std::string method_str(method);
  std::vector<uint8_t> req_bytes(request_bytes, request_bytes + request_size);

  std::thread([this, method_str = std::move(method_str),
               req_bytes = std::move(req_bytes),
               ctx, on_response, on_error]() mutable {
    std::vector<uint8_t> resp_bytes;
    auto status = DoUnaryCall(method_str.c_str(),
                              req_bytes.data(), req_bytes.size(), &resp_bytes);
    if (!status.ok()) {
      if (on_error) {
        on_error(ctx, static_cast<int32_t>(status.error_code()),
                 status.error_message().c_str());
      }
      return;
    }
    if (on_response) {
      on_response(ctx, resp_bytes.data(), resp_bytes.size());
    }
  }).detach();
}

#endif  // PIVOX_AI_CHAT_HAS_SWIFT_PROTOS

}  // namespace pivox::ai_chat
