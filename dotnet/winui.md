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

## Firebase C++ SDK setup notes

The Firebase C++ SDK distribution for Windows lives at
https://firebase.google.com/docs/cpp/setup#windows-specific. Steps:

1. Download the SDK; extract under
   `dotnet/PivoxApp.Windows/Firebase.Native/firebase_cpp_sdk/`.
2. The Windows SDK is a set of `.lib` files plus headers — link
   against `firebase_app.lib` + `firebase_auth.lib` for the modules
   we need.
3. Provide `google-services.json` (Windows equivalent of
   `GoogleService-Info.plist`) at the project root. Get it from the
   Firebase Console — same project as the macOS app
   (`pivox-cloud`), Windows platform.
4. Initialize Firebase once at app startup via
   `firebase::App::Create(firebase::AppOptions::LoadFromJsonConfig(...), ...)`.

If `google-services.json` for Windows doesn't exist yet in the
Firebase project, register a new Windows app under the same project
in the Firebase Console first.

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
2. `dotnet/PivoxApp/Auth/MacOsAuthService.cs` — behavior reference
3. `dotnet/PivoxApp/DetailViewController.cs` — UI consumer pattern
4. `dotnet/Pivox.Client/PivoxClient.cs` — how the gRPC client wires
5. `dotnet/Pivox.Shared/Auth/IAuthService.cs` — the contract
6. Firebase C++ SDK quickstart at
   https://firebase.google.com/docs/cpp/setup#windows-specific

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
