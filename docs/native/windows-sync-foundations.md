# Windows Sync — Foundational Plumbing

## Context

This doc previously described how the Windows side would consume a shared C++ core (`pivox_auth`, `pivox_protos_cpp`) along with a Swift↔C++ interop bridge running on macOS. **That entire shared layer has been retired.** macOS now talks to the cloud via pure-Swift `grpc-swift-2`; the C-ABI token-provider seam and the C++ gRPC interceptor are gone.

**What this means for Windows**: there is no longer a shared C++ chat or auth library to consume. Windows will write its own native gRPC client, its own Firebase token-fetch + auth interceptor, and its own re-auth classifier. The wire protocol (proto schema) is the only cross-platform contract.

## What's actually shared (don't duplicate)

Only the proto schema — `api/proto/pivox/**/*.proto`. Everything else is per-platform.

- macOS Swift surface: `native/platform/macos/swift/AIChat/Client/ChatClient.swift` (grpc-swift-2 native, Firebase ID token attached per-RPC inside a `ClientInterceptor`).
- Windows surface: TBD per the tasks below.

Generated outputs:
- Go BE (cloud): `internal/pkg/gen/...` via `buf.gen.yaml`.
- macOS: `native/platform/macos/swift-packages/PivoxModels/Sources/PivoxModels/Generated/` via `buf.gen.native.swift.yaml` (apple/swift-protobuf + `protoc-gen-grpc-swift-2`).
- Windows: TBD — see Task 2 below.

## Windows-side tasks

### 1. C++ dependency story

Windows needs:
- **gRPC C++** (with the experimental `ClientInterceptor` API or whatever async API WinRT favors)
- **protobuf** at a version compatible with the codegen plugin you pick
- **Firebase C++ SDK 13.6.0+** for auth (or whatever Microsoft-blessed Firebase wrapper exists by the time this work starts)
- **gtest** for unit tests

vcpkg is the lowest-friction option since macOS already uses it. NuGet, hand-built libs, or vendored sources all work — choose what matches Windows build conventions. Pin the `protobuf` version to whatever your protoc plugin emits against; don't bump them independently.

### 2. C++ proto codegen for Windows

The macOS codegen path emits Swift only. Windows needs C++ proto types + gRPC stubs of its own. Add a `buf.gen.native.cpp.yaml` template (modeled on `buf.gen.native.swift.yaml`) that runs:
- `buf.build/protocolbuffers/cpp` (or local `protoc-gen-cpp`) for message types
- `protoc-gen-grpc-cpp` (or grpc's BSR equivalent) for gRPC client stubs

Output goes into a Windows-specific dir (e.g. `native/platform/windows/generated/`), built into a `pivox_protos_cpp` static lib that Windows targets link. The macOS app does NOT consume this — the two platforms generate their bindings independently.

### 3. Firebase SDK + token fetch

Set up Firebase Auth on Windows (whatever the current platform mechanism is — NuGet, static libs, vendored). Wire `User::GetIdToken(forceRefresh: false)` so it returns a fresh JWT cached by the SDK.

### 4. gRPC auth interceptor — Windows-native

Implement on Windows what `FirebaseAuthInterceptor` does in `ChatClient.swift`. Shape:

```cpp
class FirebaseAuthInterceptor : public grpc::experimental::Interceptor {
  void Intercept(grpc::experimental::InterceptorBatchMethods* methods) override {
    if (methods->QueryInterceptionHookPoint(
        grpc::experimental::InterceptionHookPoints::PRE_SEND_INITIAL_METADATA)) {
      // Async getIdToken from Firebase, then:
      //   methods->GetSendInitialMetadata()->insert(
      //       {"authorization", "Bearer " + token});
      //   methods->Proceed();
    } else {
      methods->Proceed();
    }
  }
};
```

Register the interceptor factory on every gRPC channel the Windows app creates. There is no shared `CreateAuthenticatedChannel` helper — it lived in the deleted `pivox_auth` lib. If the same pattern recurs on Windows, build a Windows-local helper; do not try to revive the C-ABI seam.

### 5. Re-auth-required error classification

Firebase does NOT auto-sign-out when the refresh token is revoked server-side. Each platform needs to detect this and route the user back to login.

The canonical list of *logical* conditions used to live in `native/core/auth/auth_constants.h::pivox::auth_reauth`. That header is gone. The conditions still apply — the conceptual list is:

- token expired
- invalid token
- user disabled
- user not found
- credential revoked / changed

Implement an `IsReAuthRequired(firebase::auth::Error)` classifier on Windows that maps the Firebase C++ SDK error enum onto these conditions. Trigger the Windows equivalent of sign-out + local session clear on a match. Transient errors (network, timeout) are not re-auth — let the user retry.

If we ever centralize this list again (e.g. in proto docs or a shared markdown file), both platform classifiers must track it.

## Out of scope

- **Sharing C++ chat or auth libraries with macOS** — explicitly rejected. macOS is pure Swift; nothing in the macOS chat path is reusable on Windows.
- **Sharing UI state, view models, or services** — each platform owns its own state model. Only the wire protocol is shared.
- **Reviving the C-ABI token-provider seam (`token_provider_c.h`)** — the bridge architecture it served is gone.
- **Reviving the custom protoc plugins (`protoc-gen-pivox-{cpp-bridge,swift-facade,swift-protobridge}`)** — also gone. Use stock plugins.

## New work — WinRT observable VM codegen plugin (still relevant)

Windows-only. macOS has no counterpart because SwiftUI's `@Observable` + swift-protobuf structs gives reactive binding for free. WinRT has no equivalent — XAML `x:Bind` requires types declared in IDL that implement `INotifyPropertyChanged` and `IObservableVector<T>`.

### What to build

A `protoc-gen-pivox-winrt-observable` plugin emitting per-message:
- `.idl` declaring a WinRT runtime class
- C++ implementation that PROJECTS (doesn't copy) the underlying proto — wrapper holds a pointer/reference to the proto, getters forward
- Scalar properties with `INotifyPropertyChanged` notifications
- Repeated fields wrapped as `IObservableVector<T>` with keyed diff-apply (naive clear+refill blows away ListView selection/scroll — emit key-based LCS-style diffs per repeated field)
- `oneof` → kind enum + nullable per-case property
- Map fields → `IObservableMap<K,V>`
- All notifications marshaled to `DispatcherQueue` (wrapper captures dispatcher at construction)
- Activation factory + WinRT runtime class registration

### Scope: generate for ALL messages by default

Match the swift-protobuf pattern (now done by `apple/swift-protobuf` for macOS): generate a binding wrapper for every proto message, don't annotate anything. Most will be unused — that's fine. Binary-size cost estimate: ~5–15MB for the full proto surface (50–100 messages × 200–300 LOC each after optimization). Trivial for a desktop app.

Opt-out escape hatch only if a specific message causes problems:

```proto
message Foo {
  option (pivox.winrt.skip_vm) = true;
}
```

Default is on.

### Single mutation entry point per wrapper

`Replace(next_proto)` diffs against prior state and fires granular events. Services ALWAYS mutate through this — never touch the underlying proto directly. Matches what `@Observable` does in Swift on the macOS side: all mutations flow through the observable container.

### Plugin source location

Follow the pattern the now-deleted plugins used: `tools/cmd/protoc-gen-pivox-winrt-observable/` in the `tools` Go module. Register in `buf.gen.native.winrt.yaml` with `local: bin/protoc-gen-pivox-winrt-observable`. (Note: the macOS-specific plugins under `tools/cmd/` were retired — Windows resurrecting that directory layout is fine, but it'll be the only occupant.)

### When to start

Hand-write one or two WinRT VMs first (for any proto-shaped type you're about to bind in XAML) to nail the per-message shape. Codegen once the pattern is stable.
