#include "token_provider_c.h"

#include <optional>
#include <string>
#include <utility>

#include "token_provider.h"

namespace {

// Heap-allocated bridge between the C++ TokenCompletion (a std::function
// the interceptor gave us) and the C function-pointer trampoline the
// platform provider will invoke. The platform provider owns a pointer
// to `Shim` for as long as it takes to complete the fetch.
//
// Lifetime: created in the forwarding lambda, destroyed in the
// trampoline after `cpp_completion` has been invoked.
struct Shim {
  pivox::auth::TokenCompletion cpp_completion;
};

// C-ABI callback the platform provider calls when it has the token.
// Copies the C string synchronously into a std::optional<std::string>
// and invokes the waiting C++ completion.
extern "C" void pivox_auth_completion_trampoline(
    void* ctx, const char* token) {
  auto* shim = static_cast<Shim*>(ctx);
  std::optional<std::string> value;
  if (token != nullptr) {
    value = std::string(token);
  }
  if (shim->cpp_completion) {
    shim->cpp_completion(std::move(value));
  }
  delete shim;
}

}  // namespace

extern "C" void pivox_auth_register_provider(pivox_token_fetch_fn fn) {
  if (fn == nullptr) {
    pivox::auth::ClearProvider();
    return;
  }

  // Wrap the C function pointer in a std::function that the interceptor
  // can call. Each fetch allocates a Shim that owns the C++ completion
  // until the trampoline disposes of it.
  pivox::auth::RegisterProvider(
      [fn](pivox::auth::TokenCompletion cpp_completion) {
        auto* shim = new Shim{std::move(cpp_completion)};
        fn(shim, &pivox_auth_completion_trampoline);
      });
}
