#include "token_provider.h"

#include <mutex>
#include <utility>

namespace pivox::auth {
namespace {

// Process-global provider storage. Mutex guards both install and read —
// install is rare (once at platform startup), read happens per RPC.
std::mutex& Mu() {
  static std::mutex m;
  return m;
}

TokenFetchFn& ProviderSlot() {
  static TokenFetchFn f;
  return f;
}

}  // namespace

void RegisterProvider(TokenFetchFn fn) {
  std::lock_guard<std::mutex> lock(Mu());
  ProviderSlot() = std::move(fn);
}

void ClearProvider() {
  std::lock_guard<std::mutex> lock(Mu());
  ProviderSlot() = nullptr;
}

void FetchToken(TokenCompletion completion) {
  TokenFetchFn fn;
  {
    // Copy under the lock so the provider can run (and the completion
    // can fire) without holding the mutex. Required for async providers
    // that re-enter FetchToken or touch the platform's main thread.
    std::lock_guard<std::mutex> lock(Mu());
    fn = ProviderSlot();
  }

  if (!fn) {
    if (completion) completion(std::nullopt);
    return;
  }
  fn(std::move(completion));
}

}  // namespace pivox::auth
