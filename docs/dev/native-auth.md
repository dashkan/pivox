# Native App Authentication Architecture

> **Status (2026): legacy / reference.** This entire architecture is
> built on Firebase Auth (Firebase Apple/C++/Android SDKs, Firebase
> Identity Platform federation). The Native App is now a legacy/reference
> target, and Firebase is **no longer the Pivox auth system** — the cloud
> is Keycloak-only and verifies Keycloak OIDC access tokens via
> `internal/oidc`, so tokens minted by these Firebase SDKs do not
> authenticate against the current Cloud Controller. Preserved as a record
> of the abandoned native-auth design. For the current model (Keycloak BFF
> for `web/apps/start`, Keycloak public PKCE client for Electron, shared
> `@pivox/oidc`), see `AGENTS.md` (§ Components).

## Principle

Use the best native SDK for each platform. No browser hacks, no workarounds. Every platform has production-grade auth SDKs.

## Provider Support Matrix

| Provider | macOS | iOS/iPadOS | Windows | Future: Android | Future: Linux |
|---|---|---|---|---|---|
| Email/Password | Firebase Apple SDK | Firebase Apple SDK | Firebase C++ SDK | Firebase Android SDK | Firebase C++ SDK |
| Google | GoogleSignIn-iOS SDK | GoogleSignIn-iOS SDK | OAuth2Manager + Firebase C++ | Google Sign-In Android | OAuth2Manager pattern |
| GitHub | ASWebAuthenticationSession | ASWebAuthenticationSession | OAuth2Manager + Firebase C++ | Chrome Custom Tab | Browser flow |
| Apple | AuthenticationServices | AuthenticationServices | OAuth2Manager + Firebase C++ | N/A | N/A |
| Microsoft | ASWebAuthenticationSession | ASWebAuthenticationSession | MSAL + Firebase C++ | MSAL Android | MSAL |
| SAML/OIDC (enterprise) | ASWebAuthenticationSession | ASWebAuthenticationSession | OAuth2Manager | AppAuth Android | AppAuth |

## SDK Summary

### Apple Platforms (macOS, iOS, iPadOS)

**Firebase Apple SDK** (`firebase-ios-sdk` via SPM)
- Email/password: `Auth.auth().signIn(withEmail:password:)`
- Token management: automatic refresh, Keychain storage
- Works on macOS (native, not Catalyst) + iOS + iPadOS

**GoogleSignIn-iOS** (`google/GoogleSignIn-iOS` via SPM)
- Supports both iOS and macOS
- macOS: opens default browser, returns credential
- iOS: in-app browser sheet or One Tap
- Returns `GIDGoogleUser` with ID token → create Firebase credential

**ASWebAuthenticationSession** (AuthenticationServices framework, built-in)
- System browser sheet — stays in-app, shares Safari cookies
- Works for any OAuth 2.0 / OIDC / SAML provider
- Used for GitHub, Microsoft, Apple, enterprise SSO
- macOS 10.15+, iOS 12+

**AuthenticationServices** (Sign in with Apple, built-in)
- `ASAuthorizationAppleIDProvider` — native, no browser
- Returns Apple ID credential → create Firebase credential

### Windows

**Firebase C++ SDK** (beta, functional for desktop)
- Email/password: `auth->SignInWithEmailAndPassword(email, password)`
- Credential-based: `auth->SignInWithCredential(credential)` — you bring the OAuth token
- Token storage: manual via AppState (Credential Manager)

**OAuth2Manager** (Windows App SDK, built-in)
- Opens user's default browser for OAuth
- Handles PKCE automatically
- Handles redirect via protocol activation (`pivox:/oauth-callback/`)
- Exchanges auth code for tokens
- Works with ANY OAuth 2.0 provider (Google, GitHub, Apple, SAML, OIDC)
- Available in Windows App SDK 1.7+

**Windows.Security.Credentials.PasswordVault** (built-in)
- Secure credential storage (equivalent of Keychain)
- Used via AppState.saveSecure/loadSecure

## Auth Flows

### Email/Password (all platforms)

```
User enters email + password
    → Firebase SDK signInWithEmailAndPassword
    → Firebase returns ID token + refresh token
    → Store tokens via AppState.saveSecure
    → Auth state → logged in
```

Same SDK call on all platforms. Firebase handles token refresh automatically on Apple. On Windows, manual refresh via C++ SDK.

### Google Sign-In (macOS/iOS)

```
User clicks "Continue with Google"
    → GoogleSignIn-iOS SDK presents browser/sheet
    → User signs in at accounts.google.com
    → SDK returns GIDGoogleUser with ID token
    → GoogleAuthProvider.credential(withIDToken:)
    → Auth.auth().signIn(with: credential)
    → Store via AppState.saveSecure
```

### Google Sign-In (Windows)

```
User clicks "Continue with Google"
    → OAuth2Manager.RequestAuthWithParamsAsync(
        windowId,
        "https://accounts.google.com/o/oauth2/v2/auth",
        params)  // client_id, scopes, PKCE auto-handled
    → Default browser opens, user signs in
    → Browser redirects to pivox:/oauth-callback/
    → AppInstance.Activated fires
    → OAuth2Manager.CompleteAuthRequest(uri)
    → OAuth2Manager.RequestTokenAsync → access_token + id_token
    → GoogleAuthProvider::GetCredential(id_token, nullptr)
    → auth->SignInWithCredential(credential)
    → Store via AppState.saveSecure
```

### GitHub (macOS/iOS)

```
User clicks "Continue with GitHub"
    → ASWebAuthenticationSession(
        url: "https://github.com/login/oauth/authorize?client_id=...",
        callbackURLScheme: "pivox")
    → In-app browser sheet opens
    → User signs in at github.com
    → GitHub redirects to pivox://callback?code=...
    → Session returns URL with auth code
    → Exchange code for access token (POST github.com/login/oauth/access_token)
    → GithubAuthProvider.credential(withToken: accessToken)
    → Auth.auth().signIn(with: credential)
```

### GitHub (Windows)

```
Same as Google on Windows but with GitHub's authorize URL.
OAuth2Manager handles the browser + redirect + PKCE.
```

### Enterprise SAML/OIDC (all platforms)

Firebase Identity Platform handles federation. The native app doesn't need per-customer IdP configuration.

```
User clicks "Sign in with SSO"
    → App presents browser (ASWebAuthenticationSession / OAuth2Manager)
    → URL points to Firebase Auth's SAML/OIDC handler
    → Firebase redirects to customer's IdP login page
    → User authenticates at IdP
    → IdP redirects back to Firebase
    → Firebase issues ID token
    → App receives token via callback
    → Auth state → logged in
```

## Shared C++ Interface

```cpp
// core/auth.h — shared auth result types
namespace pivox {

struct AuthUser {
    std::string uid;
    std::string email;
    std::string displayName;
    std::string photoURL;
    bool emailVerified;
};

enum class AuthError {
    None,
    InvalidEmail,
    WrongPassword,
    UserNotFound,
    EmailAlreadyInUse,
    WeakPassword,
    NetworkError,
    Unknown,
};

} // namespace pivox
```

Platform-specific auth services implement the actual sign-in logic using native SDKs. The shared types are used for passing results to the UI layer.

## Token Storage

All platforms use `AppState` (already built):

| Method | macOS/iOS | Windows |
|---|---|---|
| `saveSecure(key, value)` | Keychain | PasswordVault |
| `loadSecure(key)` | Keychain | PasswordVault |
| `deleteSecure(key)` | Keychain | PasswordVault |
| `saveBool("rememberMe", true)` | NSUserDefaults | LocalSettings |

## Delegated Auth (Plugins)

Plugins running inside third-party host applications (ActiveX in iNEWS, UXP in Adobe, etc.) cannot authenticate directly — no browser control, no protocol handlers, and passwords should never be typed into a host process's memory space.

Instead, plugins use **delegated auth**: the plugin creates a session on the backend, launches the Pivox app via deep link, the user authenticates in the app, and the plugin polls for a custom token.

See [delegated-auth.md](delegated-auth.md) for the full architecture, security properties, and implementation details.

Key points:
- Plugin only calls `signInWithCustomToken` — no OAuth flows, no password handling
- Backend is the coordination point — no local state, no file-based signaling
- Each plugin gets its own Firebase app name (`pivox-activex`) for persistence isolation
- Add a new auth provider to the app once, every plugin gets it for free

## What the Native App Does NOT Use

The Go backend has auth endpoints built for the web app. The native app does NOT use:
- `POST /internal/v1/auth:exchangeToken` — native SDKs handle token exchange
- `POST /internal/v1/auth:depositToken` — no need to deposit tokens server-side
- `POST /internal/v1/auth:consumeToken` — no opaque code exchange

These endpoints remain for the web app (Start app).

The native app DOES use the **delegated auth** endpoints when launched via deep link to authenticate on behalf of a plugin:
- `POST /internal/v1/auth:completeDelegatedAuthSession` — called by the app after user signs in

## Dependencies

### Apple Platforms (SPM)
- `firebase-ios-sdk` — https://github.com/firebase/firebase-ios-sdk
- `GoogleSignIn-iOS` — https://github.com/google/GoogleSignIn-iOS

### Windows (vcpkg / manual)
- Firebase C++ SDK — https://github.com/firebase/firebase-cpp-sdk
- Windows App SDK (already included) — OAuth2Manager

### Configuration
- Firebase project config: `GoogleService-Info.plist` (Apple), `google-services.json` (Windows)
- Google OAuth client ID: from Google Cloud Console (separate for iOS, macOS, Windows)
- GitHub OAuth app: from GitHub Settings → Developer settings
- Apple Sign-In: from Apple Developer portal (App ID capability)

## URL Scheme Registration

All platforms register `pivox://` for OAuth callbacks:
- macOS: `Info.plist` → `CFBundleURLTypes`
- iOS: `Info.plist` → `CFBundleURLTypes`
- Windows: Protocol activation in app manifest
