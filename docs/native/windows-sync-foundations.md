# Windows Sync — Foundational Plumbing

## Context

The macOS side just landed foundational plumbing for Swift↔C++ interop,
shared gRPC auth, and broadened proto codegen. This prompt gets the Windows
side aligned on the cross-platform pieces.

## What's already shared (don't duplicate)

The following landed in `native/core/` and is platform-agnostic C++. Windows
consumes it as-is — do not rewrite WinRT-specific variants.

### `native/core/auth/` — gRPC auth plumbing
- `token_provider.{h,cc}` — `RegisterProvider` / `FetchToken` / `ClearProvider`
- `token_provider_c.{h,cc}` — C-ABI seam: `pivox_auth_register_provider(fn)`
- `auth_interceptor.{h,cc}` — gRPC client interceptor that attaches
  `Authorization: Bearer <token>` to every outbound RPC. Fetches the token
  async via the registered provider before each call.
- `CreateAuthenticatedChannel(endpoint)` — one-liner for any service. Uses
  insecure credentials by default; overloads for custom creds/args.

**Unit tests (`pivox_auth_tests`) pass on macOS; should also build+pass
on Windows once the shared C++ layer is wired in.**

### `native/core/generated/cpp/` — full proto generation
Previously only `pivox/ai/v1` was generated. Now ALL pivox protos plus
`google.api`, `google.rpc`, `google.type`, `google.longrunning`, and
`buf.validate` are generated to C++ via `buf.gen.native.cpp.yaml`. Windows
gets every proto type for free by linking `pivox_protos_cpp`.

### `native/core/auth/auth_constants.h` — shared auth policy
Contains canonical user-facing error messages AND a policy list for which
Firebase error conditions trigger forced sign-out vs transient retry. The
`auth_reauth` namespace doc comment is the cross-platform contract — both
platforms' classifier functions must match it.

## Windows-side tasks

### 1. Stand up vcpkg (or whatever C++ dependency solution Windows picks)

Windows doesn't have vcpkg set up yet. Adopting it would be the lowest-
friction path since the shared C++ libs (`pivox_auth`, `pivox_protos_cpp`)
depend on:
- **gRPC** — interceptor API in `grpc::experimental`
- **Protobuf** — must match the plugin pin in `buf.gen.native.cpp.yaml`
  (currently `v29.5`). Latest plugin emits code using newer runtime APIs
  (`PROTOBUF_NONNULL`, new `ParseNamedEnum` overloads) that older
  libprotobuf doesn't provide. Bump vcpkg protobuf and the plugin pin
  together, never independently.
- **gtest** — for `pivox_auth_tests` and any future shared C++ tests

Alternative: NuGet packages, hand-built libs, or whatever matches Windows
build conventions — but whichever path you pick needs these specific
versions (or versions compatible with the pinned plugin).

### 2. Firebase SDK upgrade to 13.6.0
Windows is on an older SDK. Upgrade to 13.6.0 by whatever mechanism
Windows currently uses for Firebase (NuGet, static libs, vendored source).
Review API changes in auth / IdToken retrieval — the async shape wraps
into the token provider (task 3).

### 3. Register the token provider from Windows startup
**Not a new interceptor — just registration.** The interceptor is shared
C++ code that already handles the auth header insertion. Windows only
needs to provide a platform-specific token fetcher.

Call once during app startup, after Firebase is initialized:

```cpp
pivox_auth_register_provider(&FetchFirebaseIdToken);
```

Where `FetchFirebaseIdToken` has this signature:

```cpp
void FetchFirebaseIdToken(
    void* completion_ctx,
    pivox_token_completion_fn completion);
```

Contract (from `token_provider_c.h`):
- Invoke `completion` exactly once, on any thread
- Pass a NUL-terminated C string for the token, or `nullptr` on failure
- The string's lifetime only needs to extend through the `completion` call;
  C++ copies synchronously into a `std::string`
- Implementation: kick off Firebase's async getIdToken, stash the
  completion + ctx, fire back when the token arrives

### 4. Re-auth-required error classification
Firebase does NOT auto-sign-out when the refresh token is revoked server-
side. The macOS bridge detects this and forces a local sign-out so the UI
can route to the login screen. Windows needs equivalent logic.

**The canonical list of conditions is in `native/core/auth/auth_constants.h`
(`pivox::auth_reauth` doc comment).** Firebase SDK error codes DIFFER
across platforms — you can't reuse the same numeric values. What you must
reuse is the decision list: which logical conditions trigger sign-out.

On Windows, implement an `IsReAuthRequired(firebase::auth::Error)`
classifier that maps the Firebase C++ SDK's error enum onto the list in
`auth_reauth`. Example conditions: `kAuthErrorUserTokenExpired`,
`kAuthErrorInvalidUserToken`, `kAuthErrorUserDisabled`,
`kAuthErrorUserNotFound`, etc.

On a match, call the Windows equivalent of sign-out + clear local session
state. For transient errors (network, timeout), just return `nullptr` to
the completion and let the user retry.

If the policy list in `auth_reauth` ever changes, both platforms' classifiers
must update together.

### 5. `pivox::auth::CreateAuthenticatedChannel` — the pattern for future gRPC clients
Windows has no gRPC clients yet. When it starts using them, every client
must create its channel via:

```cpp
auto channel = pivox::auth::CreateAuthenticatedChannel(endpoint);
```

**Never** `grpc::CreateChannel(endpoint, creds)` directly, and **never**
manually add `Authorization` metadata to `ClientContext` — the interceptor
handles it. Double-headers would result.

No immediate task; this is the discipline to follow when the first Windows
gRPC consumer lands.

### 6. CMake — link `pivox_auth` when the Windows app needs it
The shared auth lib is created by `native/core/auth/CMakeLists.txt` and
pulled into the build via `native/core/CMakeLists.txt`'s
`add_subdirectory(auth)`. When a Windows target needs to register the token
provider, add `pivox_auth` to its link dependencies.

Verify `pivox_auth_tests` builds and passes on Windows once the C++
toolchain is in place:
```
cmake --build . --target pivox_auth_tests
ctest -R PivoxAuthTests
```

## New work — WinRT observable VM codegen plugin

Windows-only. macOS has no counterpart because SwiftUI's `@Observable` +
swift-protobuf structs gives reactive binding for free. WinRT has no
equivalent — XAML `x:Bind` requires types declared in IDL that implement
`INotifyPropertyChanged` and `IObservableVector<T>`.

### What to build
A new protoc plugin `protoc-gen-pivox-winrt-observable` that emits, per
proto message, a WinRT runtime class projecting the underlying proto-cpp
message:
- `.idl` file declaring a WinRT runtime class
- C++ implementation that PROJECTS (doesn't copy) the underlying proto —
  wrapper holds a pointer/reference to the proto, getters forward
- Scalar properties with `INotifyPropertyChanged` notifications
- Repeated fields wrapped as `IObservableVector<T>` with keyed diff-apply
  (naive clear+refill blows away ListView selection/scroll; generator
  must emit key-based LCS-style diffs per repeated field)
- `oneof` → kind enum + nullable per-case property
- Map fields → `IObservableMap<K,V>`
- All notifications marshaled to `DispatcherQueue` (wrapper captures
  dispatcher at construction)
- Activation factory + WinRT runtime class registration

### Scope: generate for ALL messages by default
Match the swift-protobuf pattern on the macOS side: generate a binding
wrapper for every proto message, don't annotate anything. Most will be
unused — that's fine. Binary-size cost estimate: ~5–15MB for the full
proto surface (50–100 messages × ~200–300 LOC each after optimization).
Trivial for a desktop app; we don't care.

Opt-out escape hatch only if a specific message causes problems (e.g.,
recursive or deeply nested types that stress MIDL):
```proto
message Foo {
  option (pivox.winrt.skip_vm) = true;
}
```
Default is on. Don't ask proto authors to opt in.

### Single mutation entry point per wrapper
`Replace(next_proto)` diffs against prior state and fires granular events.
Services ALWAYS mutate through this — never touch the underlying proto
directly. Matches the macOS `@Observable` pattern where all mutations flow
through the observable container.

### Plugin source location
Follow the existing pattern: `tools/cmd/protoc-gen-pivox-winrt-observable/`
in the `tools` Go module. Register in a `buf.gen.native.winrt.yaml` with
`local: bin/protoc-gen-pivox-winrt-observable`.

### When to start
Recommend hand-writing one or two WinRT VMs first (for any proto-shaped
type you're about to bind in XAML) to nail the per-message shape, then
codegen once the pattern is stable.

## Out of scope

- **Shared UI state proto / shared services** — explicitly rejected. Each
  platform owns its own state model; only the wire protocol (proto schema)
  + gRPC client stubs are shared.
