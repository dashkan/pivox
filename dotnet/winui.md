# WinUI 3 Side — Implementation Brief

This is the prompt for implementing the Windows-side companion to
`Pivox.MacOs` (the macOS app). The macOS side is fully validated: all-code AppKit
+ Firebase Cocoa SDK + Google OAuth + persistent session + gRPC
against pivox-cloud, all under NativeAOT. See `CLAUDE.md` in this
directory for the conventions and the journey that got us here.

## Goal

Build `dotnet/Pivox.WinUI/` — a WinUI 3 + C# app that:

1. **Consumes `Pivox.Shared` and `Pivox.Client`** without modification.
   These libraries are already cross-platform and proven on macOS.
2. **Implements `IAuthService` against Firebase** for Windows. The
   Windows side has no Firebase Cocoa SDK; the auth implementation
   wraps the Firebase C++ SDK behind a C++/WinRT component.
3. **Mirrors `Pivox.MacOs`'s behavior** for the spike-level features:
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
- `Auth/AuthSession.cs` — the record the implementation returns. Two
  fields: `IdToken` (the raw JWT for outbound Bearer auth) and
  `Identity` (a `FirebaseIdentity`). Convenience accessors forward
  to the identity (`PivoxUserId`, `DisplayName`, `Email`,
  `PictureUrl`, `ExpiresAt`) plus `Principal` for `ClaimsPrincipal`-
  shaped consumers.
- `Auth/FirebaseIdentity.cs` — `ClaimsIdentity` subclass constructed
  directly from a JWT string. Uses `Microsoft.IdentityModel.JsonWebTokens`
  (AOT-clean) to parse the token; exposes typed accessors for the
  claims we read (`PivoxUserId`, `FirebaseUid`, `DisplayName`, `Email`,
  `PictureUrl`, `IdToken`, `ExpiresAt`). **Both platforms construct
  the same `FirebaseIdentity` from their respective JWT strings** —
  this is where the platform-specific code paths converge.
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

### `Pivox.MacOs/` (macOS reference implementation)

Mirror the structure, NOT the platform-specific contents:

- `Pivox.MacOs/Auth/MacOsAuthService.cs` — macOS implementation of
  `IAuthService` wrapping FirebaseAuth Cocoa SDK +
  `ASWebAuthenticationSession` for Google. Reference for the
  **behavior** your Windows implementation must match: persistent
  session (Firebase auto-restores from Keychain), `AuthSession`
  construction via `new FirebaseIdentity(jwt)`, `CurrentChanged`
  event on every sign-in/refresh/sign-out. Notice how
  `BuildSession` is a one-liner — that's the convergence point both
  platforms target.
- `Pivox.MacOs/DetailViewController.cs` — UI consumer. Knows nothing
  about Firebase. Calls `IAuthService` + `PivoxClient`. This is the
  template for the WinUI `MainWindow.xaml.cs` shape, just rendered
  in XAML instead of AppKit.

## Deliverables

### 1. Project skeleton

```
dotnet/
  Pivox.slnx                       (existing — add the two new projects)
  Pivox.Firebase.Native/           NEW: C++/WinRT component (vcxproj)
    Pivox.Firebase.Native.vcxproj
    pch.h, pch.cpp, dllmain.cpp
    FirebaseAuthBridge.idl + .h + .cpp
    GoogleOAuth.{h,cpp}            (ported from native/)
    OAuthPopup.{h,cpp}             (ported from native/)
    WinAuthService.{h,cpp}         (ported from native/)
    firebase_config.h
    firebase_cpp_sdk/              (gitignored, fetched by script)
  Pivox.WinUI/                     NEW: WinUI 3 app (csproj)
    Pivox.WinUI.csproj
    Package.appxmanifest
    App.xaml + App.xaml.cs
    MainWindow.xaml + MainWindow.xaml.cs
    Auth/WindowsAuthService.cs     (C# class : IAuthService — adapts
                                    the Pivox.Firebase.Native WinRT
                                    projection into the idiomatic
                                    .NET surface)
    google-services.json           (Firebase config, copy of native's)
  Pivox.Client/                    (existing)
  Pivox.Shared/                    (existing)
  Pivox.MacOs/                     (existing macOS app)
  Firebase.Bindings/               (existing — macOS Cocoa bindings;
                                    do not consume from Windows)
```

Two **new** top-level projects, sibling to the existing ones:

- `Pivox.Firebase.Native` — C++/WinRT component. Owns the native
  Firebase SDK glue. Pure WinRT projection (no managed code).
- `Pivox.WinUI` — the WinUI 3 app. C# only. References `Pivox.Shared`,
  `Pivox.Client`, AND `Pivox.Firebase.Native` (the WinRT component
  projects to C# automatically).

`Pivox.WinUI.csproj` does NOT reference `Firebase.Bindings` (Cocoa
binding, macOS-only). It also does not reference `Pivox.MacOs`.

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

Lives in `Pivox.WinUI/Auth/WindowsAuthService.cs`. Wraps the
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
   - Wire `AuthStateChanged` to call `SetCurrent(BuildSession(jwt))`
   - `BuildSession(jwt)` is a one-liner mirroring the macOS side:
     `new AuthSession(jwt, new FirebaseIdentity(jwt))`. The
     `FirebaseIdentity` constructor pulls every claim we care about
     from the JWT — no per-claim extraction code.
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

- **Three-layer pattern.** Auth on Windows is three distinct layers
  with single responsibilities. Don't try to collapse them — each
  is doing real work, not boilerplate.

  | Layer | Project | Responsibility |
  |---|---|---|
  | Native | `Pivox.Firebase.Native/` (vcxproj) — copied-in C++ from `native/platform/windows/` plus the Firebase C++ SDK | Calls into `firebase::auth::Auth*`, manages `firebase::Future<T>`, runs the Google OAuth flow via `OAuthPopup` |
  | WinRT bridge | `Pivox.Firebase.Native/` exposed as a C++/WinRT component, projects to C# automatically | Translates the C++ API into WinRT shapes (`hstring`, `IAsyncOperation<T>`, `TypedEventHandler<...>`); presents a clean async surface |
  | C# adapter | `Pivox.WinUI/Auth/WindowsAuthService.cs` — C# class implementing `IAuthService` | Adapts WinRT primitives into idiomatic .NET shapes: `IAsyncOperation<hstring>` → `Task<string>`, `TypedEventHandler` → `event EventHandler<AuthSession?>`, raw JWT strings → `FirebaseIdentity`/`AuthSession`. This is the layer that gives the WinUI app the same shape `IAuthService` it would get on any other platform. |

  The C# adapter is small (~50–80 LOC) but mandatory: `AuthSession`
  carries a `FirebaseIdentity : ClaimsIdentity`, which is not
  WinRT-projectable. The adapter is where the JWT becomes a
  ClaimsPrincipal. Mirrors how `Pivox.MacOs/Auth/MacOsAuthService.cs`
  is a C# class wrapping Cocoa types — same responsibility,
  different underlying primitives.

- **Port, don't rewrite.** The C++ Firebase Auth integration is
  done in `native/platform/windows/WinAuthService.*` and the
  Google OAuth flow is done in
  `native/platform/windows/shared/GoogleOAuth.cpp`. The job is to
  COPY that existing C++ code into `dotnet/Pivox.Firebase.Native/`
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
  `WindowsAuthService` C# adapter imports the WinRT projection;
  nothing else may. Inputs/outputs to/from `IAuthService` are the
  POCOs in `Pivox.Shared.Auth` — `AuthSession` carrying
  `FirebaseIdentity`, never the WinRT projection types directly.

- **No regenerating shared proto C#.** `Pivox.Client` already
  generates the gRPC C# from `api/proto/`. Windows consumes the
  same compiled DLL. Don't run protoc again in Pivox.WinUI.

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
`dotnet/Pivox.WinUI/`, then build entirely within `dotnet/`.
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
| `native/platform/windows/WinAuthService.{h,cpp}` | `dotnet/Pivox.Firebase.Native/WinAuthService.{h,cpp}` | Full Firebase C++ SDK integration: `firebase::App::Create`, `firebase::auth::Auth::SignInWithEmailAndPassword`, `SignInWithCredential` for OAuth providers, token retrieval, state listener wiring. Already handles `firebase::Future<T>` unwrapping. |
| `native/platform/windows/shared/GoogleOAuth.{h,cpp}` | `dotnet/Pivox.Firebase.Native/GoogleOAuth.{h,cpp}` | Google OAuth flow on Windows — equivalent of macOS's PKCE + ASWebAuthenticationSession. URL building, callback handling, code exchange. |
| `native/platform/windows/shared/OAuthPopup.{h,cpp}` | `dotnet/Pivox.Firebase.Native/OAuthPopup.{h,cpp}` | Windows-native popup container for the OAuth web view. Reuse instead of rewriting against WebAuthenticationBroker. |
| `native/platform/windows/firebase_config.h` | `dotnet/Pivox.Firebase.Native/firebase_config.h` | Firebase SDK header config (project ID, etc). |
| `google-services.json` (wherever it currently lives) | `dotnet/Pivox.WinUI/google-services.json` | Firebase project config tied to `pivox-cloud`. Same file the existing C++ build uses; copy not symlink. |
| Firebase C++ SDK extraction (currently somewhere under `native/`) | `dotnet/Pivox.Firebase.Native/firebase_cpp_sdk/` or `dotnet/scripts/fetch-firebase-cpp-sdk.{ps1,sh}` that downloads it | Headers + `.lib` files the .vcxproj links against. Gitignore the extracted SDK like we do for the macOS xcframeworks; the fetch script is the reproducible source. |

After the copy, every path the WinUI build touches should be inside
`dotnet/`. The native/ tree continues to exist for the Swift+CMake
implementation and is irrelevant to the dotnet build graph.

**Strategy:**

1. **Copy the files** above into `dotnet/Pivox.Firebase.Native/`.
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

- [ ] `Pivox.WinUI` builds via `dotnet build dotnet/Pivox.slnx`
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
   `Pivox.WinUI.csproj` comment.
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
5. `dotnet/Pivox.MacOs/Auth/MacOsAuthService.cs` — behavior reference
   for the C# wrapper shape (sign-in flow, JWT extraction, event
   firing)
6. `dotnet/Pivox.MacOs/DetailViewController.cs` — UI consumer pattern
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
