# WinUI 3 Side — Implementation Brief

This is the prompt for implementing the Windows-side companion to
PivoxApp (macOS). The macOS side is fully validated: all-code AppKit
+ Firebase Cocoa SDK + Google OAuth + persistent session + gRPC
against pivox-cloud, all under NativeAOT. See `CLAUDE.md` in this
directory for the conventions and the journey that got us here.

## Goal

Build `dotnet/PivoxApp.Windows/` — a WinUI 3 + C# app that:

1. **Consumes `Pivox.Shared` and `Pivox.Client`** without modification.
   These libraries are already cross-platform and proven on macOS.
2. **Implements `IAuthService` against Firebase** for Windows. The
   Windows side has no Firebase Cocoa SDK; the auth implementation
   wraps the Firebase C++ SDK behind a C++/WinRT component.
3. **Mirrors PivoxApp's behavior** for the spike-level features:
   sign-in (email/password + Google), persistent session, gRPC call
   to `pivox-cloud → Organizations.ListOrganizations`.

End state: open the WinUI app, sign in, restart it, see the session
restored, click a button, see the list of orgs from pivox-cloud.

## What's already done — consume these as-is

Everything below is in `dotnet/`. Do not modify; reference and
extend.

### `Pivox.Shared` (cross-platform contracts)

- `Auth/IAuthService.cs` — the auth contract the Windows app
  implements:
  ```csharp
  public interface IAuthService
  {
      AuthSession? Current { get; }
      event EventHandler<AuthSession?>? CurrentChanged;
      Task<AuthSession> SignInWithEmailAsync(string email, string password, CancellationToken ct = default);
      Task<AuthSession> SignInWithGoogleAsync(CancellationToken ct = default);
      Task SignOutAsync(CancellationToken ct = default);
      Task<string> GetIdTokenAsync(CancellationToken ct = default);
  }
  ```
- `Auth/AuthSession.cs` — the POCO record the implementation returns
  (`IdToken`, `PivoxUserId`, `Email`, `ExpiresAt`).
- `Auth/JwtClaims.cs` — JWT payload decoder. Extracts `pivox_user_id`
  custom claim + `exp`. No NuGet deps. Both platforms call this
  identically.
- `CloudConfig.cs` — backend URL + TLS resolution. Reads
  `PIVOX_GRPC_HOST` and `PIVOX_GRPC_PLAINTEXT` env vars. Defaults
  to `pivox.ngrok.app:443` over TLS. **Both platforms use this** —
  don't duplicate the config.

### `Pivox.Client` (cross-platform gRPC clients)

- `PivoxClient.cs` — single entry point. Constructor takes an
  `IAuthService`; exposes `Organizations`, `Iam`, `Spaces` (and add
  more as needed). Already wires the Bearer-token interceptor.
- `Auth/AuthCallCredentials.cs` — async-native gRPC
  `CallCredentials` that calls `IAuthService.GetIdTokenAsync()` and
  attaches `Authorization: Bearer <jwt>` to every outgoing RPC. The
  Windows app gets this for free by handing `PivoxClient` your
  `WindowsAuthService` instance.
- Generated proto types — 31 service clients from
  `api/proto/pivox/**/*.proto`. Already builds clean under AOT.

### `PivoxApp/` (macOS reference implementation)

Mirror the structure, NOT the platform-specific contents:

- `PivoxApp/Auth/MacOsAuthService.cs` — macOS implementation of
  `IAuthService` wrapping FirebaseAuth Cocoa SDK +
  `ASWebAuthenticationSession` for Google. Reference for the
  **behavior** your Windows implementation must match: persistent
  session (Firebase auto-restores from Keychain), JWT extraction
  via shared `JwtClaims`, `CurrentChanged` event on every
  sign-in/refresh/sign-out.
- `PivoxApp/DetailViewController.cs` — UI consumer. Knows nothing
  about Firebase. Calls `IAuthService` + `PivoxClient`. This is the
  template for the WinUI `MainWindow.xaml.cs` shape, just rendered
  in XAML instead of AppKit.

## Deliverables

### 1. Project skeleton

```
dotnet/
  Pivox.slnx                  (existing — add PivoxApp.Windows to it)
  PivoxApp.Windows/
    PivoxApp.Windows.csproj   TargetFramework=net10.0-windows10.0.22621.0
                              (or current WinUI 3 target)
    Package.appxmanifest
    Properties/launchSettings.json
    App.xaml + App.xaml.cs
    MainWindow.xaml + MainWindow.xaml.cs
    Auth/WindowsAuthService.cs   (IAuthService implementation)
    Firebase.Native/             (C++/WinRT component — see below)
      Pivox.Firebase.Native.vcxproj
      pch.h, pch.cpp, dllmain.cpp
      FirebaseAuthBridge.idl + .h + .cpp
  Pivox.Client/                (existing)
  Pivox.Shared/                (existing)
  Firebase.Bindings/           (existing — macOS only, do not consume from Windows)
```

`PivoxApp.Windows.csproj` references `Pivox.Shared` and
`Pivox.Client`. Does NOT reference `Firebase.Bindings` (that's the
Cocoa binding — macOS only).

#### Windows App SDK version

**Pin to Windows App SDK 2.0.1** (the current stable GA, released
April 29, 2026 — the first major-version update since 1.0 in
November 2021, and the first release on the new SemVer scheme).
Release notes: https://learn.microsoft.com/en-us/windows/apps/windows-app-sdk/release-notes/windows-app-sdk-2-0

```xml
<PackageReference Include="Microsoft.WindowsAppSDK" Version="2.0.1" />
```

Under the new SemVer scheme, the NuGet version is THE version —
no separate date-based build number to track. Breaking changes are
gated on major-version bumps, so 2.0.x is a stable surface to build
against. Notable 2.0 additions you may or may not use: XAML
conditionals, modern Storage Pickers, expanded popup/anchoring APIs
in `Microsoft.UI.Content`, new package deployment + validation APIs.

If you're scaffolding from Visual Studio's "Blank App, Packaged
(WinUI 3 in Desktop)" template, update the
`Microsoft.WindowsAppSDK` PackageReference to 2.0.1 immediately —
templates may lag the latest stable.

### 2. WindowsAuthService : IAuthService

Lives in `PivoxApp.Windows/Auth/WindowsAuthService.cs`. Wraps the
Firebase C++ SDK via a C++/WinRT component. Same contract as
`MacOsAuthService` — produces `AuthSession` values, fires
`CurrentChanged`, persists session natively (Firebase C++ SDK on
Windows persists to local app data by default).

Implementation order:

1. **Build the C++/WinRT bridge first** (`Firebase.Native/`). The
   bridge exposes WinRT-projectable async methods like:
   - `SignInWithEmailAsync(hstring email, hstring password) -> Windows.Foundation.IAsyncOperation<AuthResult>`
   - `SignInWithCredentialAsync(hstring providerId, hstring idToken, hstring accessToken) -> IAsyncOperation<AuthResult>`
   - `GetIdTokenAsync(bool forceRefresh) -> IAsyncOperation<hstring>`
   - `SignOut() -> void`
   - `GetCurrentUser() -> User?`
   - `AuthStateChanged` event projected as `Windows.Foundation.TypedEventHandler<...>`

   Each method internally calls into `firebase::auth::Auth*` from
   the Firebase C++ SDK and unwraps `firebase::Future<T>` into
   IAsyncOperation. Use `winrt::resume_future` (in a coroutine
   context) to bridge `firebase::Future` to `IAsyncOperation`.

2. **C# WindowsAuthService consumes the WinRT projection**, looking
   like the macOS implementation in shape:
   - Construct the bridge once
   - Wire `AuthStateChanged` to call `SetCurrent(BuildSession(...))`
   - Use `JwtClaims.ExtractPivoxUserId` + `JwtClaims.ExtractExpiresAt`
     to build the `AuthSession` from the returned `IdToken`
   - `SignInWithGoogleAsync` — see step 3

3. **Google OAuth** — use `Windows.Security.Authentication.Web.WebAuthenticationBroker`
   (Windows-native equivalent of `ASWebAuthenticationSession`). Same
   PKCE flow as the macOS implementation:
   - Build the Google `/authorize` URL with PKCE
   - `WebAuthenticationBroker.AuthenticateAsync(...)` opens the
     system web view
   - Extract auth code from callback URI
   - POST to `oauth2.googleapis.com/token` to exchange code for
     tokens (use `System.Net.Http.HttpClient` — same as macOS)
   - Call the bridge's `SignInWithCredentialAsync("google.com", idToken, accessToken)`

   The Google OAuth client ID is the same one the macOS app uses
   (see `MacOsAuthService.GoogleClientID`). The custom URL scheme
   for the callback is also the same.

### 3. MainWindow with parity behavior

XAML layout mirroring `DetailViewController`'s contents (email
field, password field, Sign In button, Continue with Google button,
Sign Out button, Call pivox-cloud button, status text). Constructor
takes `IAuthService` + `PivoxClient`; subscribes to `CurrentChanged`
to render the restored or new session.

### 4. App.xaml.cs wiring

Single instances of `WindowsAuthService` and `PivoxClient` for the
app lifetime; passed to `MainWindow`.

## Architectural non-negotiables

- **Port, don't rewrite.** The C++ Firebase Auth integration is
  done in `native/platform/windows/WinAuthService.*` and the
  Google OAuth flow is done in
  `native/platform/windows/shared/GoogleOAuth.cpp`. The job is to
  COPY that existing C++ code into `dotnet/PivoxApp.Windows/Firebase.Native/`
  and WRAP it in a C++/WinRT component, not write new Firebase
  code. The copies in `dotnet/` are owned by the dotnet tree; the
  `native/` originals stay where they are. See the "Steal from the
  existing native/ Windows implementation" section below.

- **C++/WinRT, NOT P/Invoke.** The C++ SDK lives inside a WinRT
  component (`Pivox.Firebase.Native`). The C# side consumes it via
  WinRT projection, getting first-class C# types
  (`IAsyncOperation<T>` shows up as `Task<T>` in C#) without any
  `[DllImport]`.

- **No Firebase types cross IAuthService.** The
  `WindowsAuthService` implementation may import the WinRT projection;
  nothing else may. Inputs/outputs to/from `IAuthService` are the
  POCOs in `Pivox.Shared.Auth`.

- **No regenerating shared proto C#.** `Pivox.Client` already
  generates the gRPC C# from `api/proto/`. Windows consumes the
  same compiled DLL. Don't run protoc again in PivoxApp.Windows.

- **Bundle ID matches `app.pivox.native`.** The Firebase project
  enforces bundle-ID restriction at the API key level — the
  Package identity must produce the same effective bundle ID the
  Firebase Cocoa SDK app uses on macOS.

- **Code signing.** Use a self-signed dev cert via Visual Studio's
  packaging tool for first runs. Production signing is a separate
  task (Authenticode + MSIX bundle).

## Steal from the existing native/ Windows implementation

**Headline rule: `dotnet/` is fully self-contained. Nothing inside
`dotnet/` may reference `../native/` at build time.**

The Firebase Auth C++ SDK is already integrated and working in
`native/platform/windows/`. You COPY source files from there into
`dotnet/PivoxApp.Windows/`, then build entirely within `dotnet/`.
Reading `native/` source for guidance is fine; depending on `native/`
artifacts (link paths, generated headers, SDK extraction, build
outputs, configs) is not.

`native/` continues to exist as the current Swift/CMake/WinUI-C++
implementation. `dotnet/` is the parallel .NET cross-platform
alternative. After this work lands, the C++ Firebase glue files
exist in BOTH trees — that's the point. The `dotnet/` copies are
the new owners; they diverge from `native/` originals over time as
each tree evolves.

Source files to copy → destination:

| Source (in `native/`, read for reference, copy from) | Destination (in `dotnet/`, owned by dotnet) | What it gives you |
|---|---|---|
| `native/platform/windows/WinAuthService.{h,cpp}` | `dotnet/PivoxApp.Windows/Firebase.Native/WinAuthService.{h,cpp}` | Full Firebase C++ SDK integration: `firebase::App::Create`, `firebase::auth::Auth::SignInWithEmailAndPassword`, `SignInWithCredential` for OAuth providers, token retrieval, state listener wiring. Already handles `firebase::Future<T>` unwrapping. |
| `native/platform/windows/shared/GoogleOAuth.{h,cpp}` | `dotnet/PivoxApp.Windows/Firebase.Native/GoogleOAuth.{h,cpp}` | Google OAuth flow on Windows — equivalent of macOS's PKCE + ASWebAuthenticationSession. URL building, callback handling, code exchange. |
| `native/platform/windows/shared/OAuthPopup.{h,cpp}` | `dotnet/PivoxApp.Windows/Firebase.Native/OAuthPopup.{h,cpp}` | Windows-native popup container for the OAuth web view. Reuse instead of rewriting against WebAuthenticationBroker. |
| `native/platform/windows/firebase_config.h` | `dotnet/PivoxApp.Windows/Firebase.Native/firebase_config.h` | Firebase SDK header config (project ID, etc). |
| `google-services.json` (wherever it currently lives) | `dotnet/PivoxApp.Windows/google-services.json` | Firebase project config tied to `pivox-cloud`. Same file the existing C++ build uses; copy not symlink. |
| Firebase C++ SDK extraction (currently somewhere under `native/`) | `dotnet/PivoxApp.Windows/Firebase.Native/firebase_cpp_sdk/` or `dotnet/scripts/fetch-firebase-cpp-sdk.{ps1,sh}` that downloads it | Headers + `.lib` files the .vcxproj links against. Gitignore the extracted SDK like we do for the macOS xcframeworks; the fetch script is the reproducible source. |

After the copy, every path the WinUI build touches should be inside
`dotnet/`. The native/ tree continues to exist for the Swift+CMake
implementation and is irrelevant to the dotnet build graph.

**Strategy:**

1. **Copy the files** above into `dotnet/PivoxApp.Windows/Firebase.Native/`.
2. **Wrap them in a C++/WinRT component** — `Pivox.Firebase.Native`
   is a thin WinRT projection layer over the copied `WinAuthService`
   class. Public methods on the WinRT runtime class become thin
   forwarders to `WinAuthService` methods.
3. **The existing `GoogleOAuth` + `OAuthPopup` code likely needs
   no logic changes** — they don't depend on the surrounding
   UWP/WinUI runtime model. Just compile them into the WinRT
   component.
4. **Reuse `google-services.json`** — copy it into the WinRT
   component's resource directory. Same project as the macOS app
   (`pivox-cloud`).

**No CMake in the dotnet/ tree.** The strategic decision is pure
Visual Studio projects — `.csproj` for managed code, `.vcxproj` for
the C++/WinRT component. CMake is one of the explicit costs of the
current `native/` plan that `dotnet/` is paying to avoid; don't
reintroduce it here.

That means: the copied C++ files (WinAuthService, GoogleOAuth,
OAuthPopup) compile inside `Pivox.Firebase.Native.vcxproj`, with
Firebase SDK include paths + library references declared as
MSBuild `<AdditionalIncludeDirectories>` + `<AdditionalLibraryDirectories>`
+ `<AdditionalDependencies>` in the project's PropertyGroups/ItemGroups.

Read `native/CLAUDE.md`'s build section as REFERENCE only — to
understand which Firebase SDK extraction layout and which compile/link
flags the existing CMake setup uses. Then translate those flag-sets
into the `.vcxproj` equivalents. Don't carry CMake over.

## Firebase C++ SDK reference

Only relevant if you're starting from scratch (you're not — see
above). For reference: https://firebase.google.com/docs/cpp/setup#windows-specific

## Validation checklist

In order:

- [ ] `PivoxApp.Windows` builds via `dotnet build dotnet/Pivox.slnx`
- [ ] `Firebase.Native` C++/WinRT component builds + projects cleanly
      into C# (verify by referencing a method on the C# side and
      seeing it as `Task<T>`)
- [ ] Email/password sign-in returns an `AuthSession` with a
      non-empty `IdToken` and `PivoxUserId` from the JWT claim
- [ ] Restart the app — `Current` is populated automatically (Firebase
      C++ SDK auto-restores from Windows credential storage)
- [ ] Google sign-in: `WebAuthenticationBroker` opens, user authenticates,
      callback returns auth code, exchange completes, Firebase
      credential sign-in succeeds
- [ ] Sign Out clears `Current` and fires `CurrentChanged(null)`
- [ ] "Call pivox-cloud → ListOrganizations" returns the same orgs
      the macOS app returns for the same user

## Open questions to surface BEFORE starting

1. **Firebase Windows SDK version.** Check the current Firebase C++
   SDK Windows release; record the version in
   `PivoxApp.Windows.csproj` comment.
2. **Windows app registration in Firebase Console.** Does it exist?
   If not, this is a Firebase Console click-through.
3. **MSIX target framework.** WinUI 3 + .NET 10 — confirm the
   exact moniker (`net10.0-windows10.0.22621.0` or similar). The
   `Package.appxmanifest` target needs to align.
4. **C++/WinRT project type.** "Windows Runtime Component (C++/WinRT)"
   from the WinUI 3 template gallery is the right starting point.
   Confirm template availability in the current VS version.

## What's NOT in scope for this brief

- Full feature parity with the SwiftUI Pivox app (just sign-in +
  one gRPC call to prove the stack).
- Apple Sign In on Windows (n/a — no Apple SSO without macOS).
- MFA, anonymous auth, password reset flows.
- Production signing / store distribution.
- Firebase Crashlytics, Remote Config, etc. — only Auth.

Those come after the basic stack is proven.

## Reference reading order

1. `dotnet/CLAUDE.md` — agent conventions for this directory
2. `dotnet/Pivox.Shared/Auth/IAuthService.cs` — the contract you're
   implementing on the Windows side
3. **`native/platform/windows/WinAuthService.{h,cpp}` — the C++
   Firebase code to port over. This already works. Don't rewrite.**
4. **`native/platform/windows/shared/GoogleOAuth.{h,cpp}` and
   `OAuthPopup.{h,cpp}` — the Google OAuth flow + popup to port over.**
5. `dotnet/PivoxApp/Auth/MacOsAuthService.cs` — behavior reference
   for the C# wrapper shape (sign-in flow, JWT extraction, event
   firing)
6. `dotnet/PivoxApp/DetailViewController.cs` — UI consumer pattern
   to mirror in MainWindow.xaml.cs
7. `dotnet/Pivox.Client/PivoxClient.cs` — how the gRPC client wires
8. `native/CLAUDE.md` — for the existing Windows build toolchain
   context (CMake + Firebase SDK setup that you're porting from)
9. Firebase C++ SDK quickstart at
   https://firebase.google.com/docs/cpp/setup#windows-specific —
   reference only; existing code already integrates it

## When you're done

Open a PR with:

- The C++/WinRT bridge code + Firebase SDK setup
- `WindowsAuthService` implementation
- `MainWindow.xaml` + code-behind
- Validation screenshots: sign-in success, restored session on
  relaunch, ListOrganizations response

Commit message format: same as the macOS-side commits — explain
the WHY, not just the WHAT. End with the Co-Authored-By line if
generated with AI assistance.
