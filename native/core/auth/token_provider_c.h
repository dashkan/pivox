#ifndef PIVOX_AUTH_TOKEN_PROVIDER_C_H
#define PIVOX_AUTH_TOKEN_PROVIDER_C_H

#include <stddef.h>
#include <stdint.h>

// Plain-C registration entry point for the token provider.
//
// Swift calls into C++ via Swift-C++ interop, but passing Swift-originated
// closures through std::function is not supported until Swift 6.4. So we
// expose a C-ABI seam: Swift registers a pair of C function pointers, and
// the C++ side packages them back into the std::function-based provider
// defined in token_provider.h.
//
// WinRT can use the same entry point — nothing Apple-specific here.

#ifdef __cplusplus
extern "C" {
#endif

// Completion callback the provider invokes to deliver a token. `token`
// points at a NUL-terminated C string; it may be NULL to signal "no
// token available" (auth not yet ready, logout, provider error).
//
// Lifetime: `token` is only valid until this callback returns. The C++
// side copies synchronously — do not reference `token` after invoking
// this function.
typedef void (*pivox_token_completion_fn)(
    void* completion_ctx,
    const char* token);

// Provider function supplied by the platform. The C++ side calls this
// when it needs a token, passing an opaque `completion_ctx` and a
// `completion` callback. The provider must invoke `completion` exactly
// once, on any thread, with the fetched token or NULL.
typedef void (*pivox_token_fetch_fn)(
    void* completion_ctx,
    pivox_token_completion_fn completion);

// Install `fn` as the active platform provider. Passing NULL clears
// any prior registration. Safe to call multiple times; the most recent
// registration wins.
void pivox_auth_register_provider(pivox_token_fetch_fn fn);

#ifdef __cplusplus
}
#endif

#endif  // PIVOX_AUTH_TOKEN_PROVIDER_C_H
