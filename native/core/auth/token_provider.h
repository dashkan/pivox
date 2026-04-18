#ifndef PIVOX_AUTH_TOKEN_PROVIDER_H
#define PIVOX_AUTH_TOKEN_PROVIDER_H

#include <functional>
#include <optional>
#include <string>

// Shared token-provider contract for gRPC auth.
//
// The shared C++ layer does not know how to authenticate — that lives on
// the platform (Firebase Apple SDK on macOS, Firebase / delegated auth
// client on Windows). Each platform registers a provider function at
// startup; the gRPC auth interceptor calls `FetchToken` before every RPC
// and the provider completes asynchronously with the current ID token.
//
// Why async: Firebase's `getIDToken` can trigger a network refresh when
// the cached token is near expiry. Blocking an RPC thread while that
// happens would pin gRPC workers; using a completion lets the interceptor
// defer `Proceed()` without occupying a thread.
namespace pivox::auth {

// Completion handed to the provider. Must be invoked exactly once, from
// any thread. `nullopt` signals "no token available" — the interceptor
// lets the RPC proceed unauthenticated in that case (server rejects).
using TokenCompletion = std::function<void(std::optional<std::string>)>;

// Provider function supplied by the platform.
using TokenFetchFn = std::function<void(TokenCompletion)>;

// Install a new platform provider, replacing any prior registration.
// Thread-safe. Typical call site: platform startup after FB init.
void RegisterProvider(TokenFetchFn fn);

// Remove the registered provider. Subsequent `FetchToken` calls complete
// immediately with `nullopt`. Exists primarily for tests and logout.
void ClearProvider();

// Dispatch a token fetch. `completion` fires on whichever thread the
// provider chooses (Swift `Task` → arbitrary concurrent executor; WinRT
// async → MTA worker). Callers must not assume the original thread.
// If no provider is registered, completion fires synchronously with
// `nullopt`.
void FetchToken(TokenCompletion completion);

}  // namespace pivox::auth

#endif  // PIVOX_AUTH_TOKEN_PROVIDER_H
