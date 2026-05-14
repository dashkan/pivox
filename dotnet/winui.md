# WinUI 3 Side — Implementation Record

This documents the Windows-side companion to `Pivox.MacOs` (the
macOS app). The macOS side is fully validated: all-code AppKit +
Firebase Cocoa SDK + Google OAuth + persistent session + gRPC
against pivox-cloud, all under NativeAOT. See `CLAUDE.md` in this
directory for the conventions and the journey that got us here.

## Status — validated end-to-end

The Windows stack is proven. All validation checkpoints passed:

- [x] `Pivox.WinUI` + `Pivox.Firebase.Native` build via MSBuild
- [x] C++/WinRT component projects cleanly into C# via CsWinRT
- [x] Email/password sign-in returns `AuthSession` with valid JWT
- [x] Restart → Firebase C++ SDK auto-restores session from
      Windows credential storage
- [x] Google sign-in via WebView2 popup (PKCE + token exchange in
      C#), credential hand-off to C++ bridge
- [x] Sign Out clears `Current`, fires `CurrentChanged(null)`
- [x] `ListOrganizations` returns same orgs as macOS app for same
      user

## Solution files

Two `.slnx` files exist deliberately:

- **`Pivox.slnx`** — all projects (macOS + Windows + shared).
  macOS-only projects (`Firebase.Bindings`, `Pivox.MacOs`) are
  excluded from Debug builds. Used by Rider on macOS.
- **`Pivox.WinUI.slnx`** — Windows projects only (`Pivox.Shared`,
  `Pivox.Client`, `Pivox.Firebase.Native`, `Pivox.WinUI`). Used
  by Visual Studio on Windows.

The split exists because `dotnet build` cannot handle vcxproj files
(no `VCTargetsPath`). The Windows solution must be built with VS's
MSBuild:

```sh
msbuild Pivox.WinUI.slnx -p:Configuration=Debug -p:Platform=x64
```

Or just F5 / Ctrl+B in Visual Studio.

## Source layout

```
dotnet/
  Pivox.slnx                       all projects (macOS + Windows)
  Pivox.WinUI.slnx                 Windows projects only (use this on Windows)
  Pivox.Firebase.Native/           C++/WinRT component (vcxproj)
    Pivox.Firebase.Native.vcxproj
    FirebaseAuthBridge.idl          WinRT surface definition
    FirebaseAuthBridge.h + .cpp     Implementation — wraps Firebase C++ SDK
    firebase_config.h               Firebase project constants (from native/core/)
    pch.h, pch.cpp                  Precompiled header
    Pivox.Firebase.Native.def       DLL exports
    packages.config                 NuGet (CppWinRT 2.0.250303.1)
    PropertySheet.props
    firebase_cpp_sdk/               (gitignored, fetched by script)
  Pivox.WinUI/                     WinUI 3 app (csproj)
    Pivox.WinUI.csproj
    Package.appxmanifest            Identity: app.pivox.native
    App.xaml + App.xaml.cs          Composition root
    MainWindow.xaml + .xaml.cs      Test harness (mirrors DetailViewController)
    Auth/WindowsAuthService.cs      IAuthService impl + Google OAuth
    app.manifest                    DPI awareness
    Assets/                         Placeholder MSIX assets
  Pivox.Client/                    (existing — shared gRPC clients)
  Pivox.Shared/                    (existing — shared auth contracts)
  Pivox.MacOs/                     (existing — macOS app)
  Firebase.Bindings/               (existing — macOS Cocoa bindings;
                                    NOT consumed by Windows)
  scripts/
    fetch-firebase-sdk.sh           macOS Firebase xcframeworks
    fetch-firebase-cpp-sdk.ps1      Windows Firebase C++ SDK 13.7.0
```

## Architecture — what was built

### Three-layer auth pattern

| Layer | Project | Responsibility |
|---|---|---|
| Native | `Pivox.Firebase.Native/` (vcxproj) | Thin C++/WinRT wrapper around Firebase C++ SDK. Calls `firebase::auth::Auth*`, bridges `firebase::Future<T>` to `IAsyncOperation<hstring>` via Win32 event + `resume_on_signal`. No UI code, no OAuth logic. |
| WinRT bridge | Same project, projected via CsWinRT | Translates C++ into WinRT shapes: `hstring`, `IAsyncOperation<String>`, `TypedEventHandler<FirebaseAuthBridge, Boolean>` |
| C# adapter | `Pivox.WinUI/Auth/WindowsAuthService.cs` | Implements `IAuthService`. Adapts WinRT → .NET: `IAsyncOperation` → `Task`, raw JWT → `FirebaseIdentity`/`AuthSession`. Owns the Google OAuth flow (PKCE + WebView2 popup + token exchange). |

### Key design decisions (divergences from original brief)

**Google OAuth lives in C#, not C++.** The original brief proposed
copying `GoogleOAuth.cpp` and `OAuthPopup.cpp` from `native/` into
the C++ component. In practice, the C++ layer is thinner — it wraps
only the Firebase C++ SDK (init, email sign-in, credential sign-in,
token retrieval, sign-out, state listener). Google OAuth runs
entirely in C# via a WebView2 popup window:

1. C# builds the Google `/authorize` URL with PKCE
2. Opens a WinUI 3 `Window` with a `WebView2` control
3. Intercepts navigation to the callback scheme, extracts auth code
4. POSTs to `oauth2.googleapis.com/token` to exchange for tokens
5. Calls `bridge.SignInWithCredentialAsync("google.com", idToken, accessToken)`

This keeps the C++ surface minimal and the OAuth flow debuggable
from C#.

**No `google-services.json`.** The Firebase C++ SDK on Windows reads
config from `firebase::AppOptions` set in code, not from a JSON
file. All constants live in `firebase_config.h` (copied from
`native/core/firebase_config.h`).

**No `auth_constants.h`.** The original `native/` code shared error
message constants between Swift and C++. The dotnet/ tree doesn't
share auth error messages cross-platform — Firebase SDK error
messages pass through directly.

**`FirebaseAuthBridge` instead of copied `WinAuthService`.** Rather
than copying and wrapping `WinAuthService.{h,cpp}` from `native/`,
we wrote `FirebaseAuthBridge.{idl,h,cpp}` as a clean C++/WinRT
runtime class. Same Firebase SDK calls, but designed from the start
as a WinRT projection surface. The `native/` code was read for
reference (Firebase API surface, error handling patterns) but not
copied verbatim.

**WebView2 popup instead of `WebAuthenticationBroker`.** WAB has
limitations with custom URI schemes in desktop apps. The WebView2
approach mirrors `native/`'s `OAuthPopup` pattern and gives full
control over navigation interception.

### WinRT bridge surface (FirebaseAuthBridge.idl)

```
runtimeclass FirebaseAuthBridge
{
    FirebaseAuthBridge();
    Boolean Initialize();
    IAsyncOperation<String> SignInWithEmailAsync(String email, String password);
    IAsyncOperation<String> SignInWithCredentialAsync(String providerId, String idToken, String accessToken);
    IAsyncOperation<String> GetIdTokenAsync(Boolean forceRefresh);
    void SignOut();
    Boolean IsSignedIn { get; };
    event TypedEventHandler<FirebaseAuthBridge, Boolean> AuthStateChanged;
}
```

All methods return JWT strings (not complex types). The C# adapter
constructs `FirebaseIdentity(jwt)` and `AuthSession(jwt, identity)`
— same convergence point as the macOS side.

## Build

### First-time setup

```sh
# Fetch Firebase C++ SDK (~250 MB, gitignored).
pwsh -File dotnet/scripts/fetch-firebase-cpp-sdk.ps1

# NuGet restore for the C++ project (packages.config model).
# VS does this automatically on solution open; from CLI:
nuget restore dotnet/Pivox.WinUI.slnx
```

### Build

```sh
# Must use VS MSBuild — dotnet CLI cannot build vcxproj.
msbuild dotnet/Pivox.WinUI.slnx -p:Configuration=Debug -p:Platform=x64

# Or just open Pivox.WinUI.slnx in Visual Studio and F5.
```

### Launch

Deploy + launch from Visual Studio (F5). The app is MSIX-packaged;
`Package.appxmanifest` identity is `app.pivox.native` to match the
Firebase project's bundle-ID restriction.

## Package versions

| Package | Version | Where |
|---|---|---|
| Microsoft.WindowsAppSDK | 2.0.1 | Pivox.WinUI.csproj |
| Microsoft.Windows.SDK.BuildTools | 10.0.28000.1839 | Pivox.WinUI.csproj |
| Microsoft.Windows.CsWinRT | 2.2.0 | Pivox.WinUI.csproj |
| Microsoft.Windows.CppWinRT | 2.0.250303.1 | Pivox.Firebase.Native packages.config |
| Firebase C++ SDK | 13.7.0 | fetch-firebase-cpp-sdk.ps1 |

## Target framework

- **TFM**: `net10.0-windows10.0.26100.0`
- **TargetPlatformMinVersion**: `10.0.17763.0` (Windows 10 1809)
- **Platforms**: x64 (ARM64 declared but Firebase C++ SDK doesn't
  ship ARM64 Windows libs as of 13.7.0)
- **Platform toolset**: v145 (VS 2026)
- **C++ standard**: C++20

## Linker dependencies

Firebase C++ SDK + Windows system libs:

```
firebase_app.lib
firebase_auth.lib
advapi32.lib    (registry, security)
ws2_32.lib      (Winsock — Firebase networking)
crypt32.lib     (crypto — TLS)
shell32.lib     (SHGetKnownFolderPath — Firebase app data dir)
```

Firebase's prebuilt Debug libs ship without PDBs for bundled deps
(libcurl, flatbuffers). LNK4099 is suppressed via `/ignore:4099`
in Debug configurations only.

## Dependency-direction rule

Same as the macOS side (see `CLAUDE.md`), extended:

- `Pivox.Shared` depends on nothing in this directory.
- `Pivox.Client` depends on `Pivox.Shared`.
- `Pivox.Firebase.Native` depends on nothing in this directory
  (only the Firebase C++ SDK).
- `Pivox.WinUI` depends on `Pivox.Shared` + `Pivox.Client` +
  `Pivox.Firebase.Native`.
- `Pivox.WinUI` does NOT reference `Firebase.Bindings` (macOS) or
  `Pivox.MacOs`.

No Firebase types cross `IAuthService`. The WinRT projection types
stay inside `WindowsAuthService`; everything above sees
`Pivox.Shared.Auth` POCOs.

## What's NOT in scope

- Full feature parity with the SwiftUI Pivox app (just sign-in +
  one gRPC call to prove the stack).
- Apple Sign In on Windows (n/a).
- MFA, anonymous auth, password reset flows.
- Production signing / store distribution.
- Firebase Crashlytics, Remote Config, etc. — only Auth.

Those come after the basic stack is proven.
