# ActiveX Authentication Strategy

## Problem

The Pivox WinUI 3 app uses `OAuth2Manager::RequestAuthWithParamsAsync` for social sign-in (Google, GitHub). This API launches the system browser and uses a `pivox://` protocol handler to receive the OAuth callback. A second app instance captures the redirect and forwards the auth code to the first instance via `CompleteAuthRequest`.

In the ActiveX control scenario, this flow doesn't work:
- We can't register a protocol handler — we're a DLL, not the app
- We can't launch a second instance of ourselves
- The host process (e.g., iNEWS) owns the process lifecycle
- iNEWS runs with `uiAccess=true` which restricts cross-process interaction

## Solution: WebView2 OAuth Popup

Replace the "launch browser + protocol redirect" portion of the OAuth flow with a WebView2 popup window. This mimics `ASWebAuthenticationSession` on macOS — a controlled browser window that intercepts the OAuth redirect.

### Flow

1. User clicks "Sign In" in the XAML Island
2. Build the OAuth authorization URL with PKCE:
   - Use `AuthRequestParams::CreateForAuthorizationCodeRequest` to generate code_verifier/code_challenge
   - Construct the full authorization URL manually (Google, GitHub, or SAML IdP)
   - Set redirect URI to `http://localhost` (never actually navigates — we intercept it)
3. Open a WinUI `Window` with a `WebView2` control
4. Navigate to the authorization URL
5. Hook `WebView2.NavigationStarting`:
   - Watch for navigation to `http://localhost?code=...`
   - Extract the authorization code from the URL query params
   - Cancel the navigation (don't actually go to localhost)
6. Exchange the code for tokens:
   - Use `OAuth2Manager::RequestTokenAsync` with the intercepted code
   - Or manually POST to the token endpoint with the PKCE code_verifier
7. Sign in to Firebase with the tokens:
   - `GoogleAuthProvider::GetCredential(id_token, access_token)`
   - `firebaseAuth->SignInWithCredential(credential)`
8. Close the popup window
9. Update the XAML Island UI to reflect signed-in state

### Architecture

```
┌─────────────────────────────────────────────┐
│ Host Process (iNEWS / test container)       │
│                                             │
│  ┌─────────────────────────────────────┐    │
│  │ ActiveX Control (XAML Island)       │    │
│  │  ┌─────────────┐                   │    │
│  │  │ Sign In btn  │──── click ───┐   │    │
│  │  └─────────────┘               │   │    │
│  └────────────────────────────────│───┘    │
│                                   │         │
│  ┌────────────────────────────────▼───┐     │
│  │ WinUI Window (popup)              │     │
│  │  ┌──────────────────────────────┐ │     │
│  │  │ WebView2                     │ │     │
│  │  │  ┌────────────────────────┐  │ │     │
│  │  │  │ Google/GitHub/SAML     │  │ │     │
│  │  │  │ sign-in page           │  │ │     │
│  │  │  └────────────────────────┘  │ │     │
│  │  └──────────────────────────────┘ │     │
│  │  NavigationStarting intercept     │     │
│  │  → extract auth code             │     │
│  │  → exchange for tokens            │     │
│  │  → Firebase SignInWithCredential  │     │
│  │  → close popup                    │     │
│  └───────────────────────────────────┘     │
│                                             │
└─────────────────────────────────────────────┘
```

### Supported Providers

| Provider | Auth URL | Notes |
|----------|----------|-------|
| Google | `accounts.google.com/o/oauth2/v2/auth` | PKCE required, same client_id as app |
| GitHub | `github.com/login/oauth/authorize` | Needs client_id registration |
| SAML/OIDC | Customer IdP URL | Enterprise SSO, redirect to our localhost |

### Redirect URI

Use `http://localhost` as the redirect URI for all providers:
- Register it as an allowed redirect URI in the OAuth app configuration (Google Console, GitHub app settings)
- The WebView2 intercepts the navigation before it actually reaches localhost
- No server needs to be running
- Works inside `uiAccess=true` processes (no cross-process communication)

### PKCE Handling

`AuthRequestParams::CreateForAuthorizationCodeRequest` generates the PKCE challenge automatically. We extract the parameters to build the URL manually:

```cpp
auto authParams = AuthRequestParams::CreateForAuthorizationCodeRequest(
    clientId, redirectUri);
authParams.Scope(L"openid email profile");

// Build URL from params — don't call RequestAuthWithParamsAsync
// (that launches the system browser which we can't use)
std::wstring authUrl = BuildAuthorizationUrl(authEndpoint, authParams);
```

If `AuthRequestParams` doesn't expose the code_challenge/code_verifier for manual URL construction, generate PKCE ourselves:
- code_verifier: 43-128 character random string (A-Z, a-z, 0-9, `-._~`)
- code_challenge: Base64URL(SHA256(code_verifier))

### Token Exchange

After intercepting the auth code, exchange it for tokens:

```cpp
// Option A: Use OAuth2Manager for token exchange
auto tokenParams = TokenRequestParams::CreateForAuthorizationCodeRequest(authCode);
auto tokenResult = co_await OAuth2Manager::RequestTokenAsync(tokenEndpoint, tokenParams);

// Option B: Manual HTTP POST (if OAuth2Manager doesn't accept manually-intercepted codes)
// POST to https://oauth2.googleapis.com/token
// Body: code, client_id, redirect_uri, code_verifier, grant_type=authorization_code
```

### Shared Auth State

The ActiveX control uses the same `WinAuthService` and `WinAppState` as the full app:
- Firebase C++ SDK manages session persistence
- Tokens stored in Windows PasswordVault via `WinAppState::saveSecure`
- `hasValidSession()` checks Firebase's persisted auth state on startup
- Both the app and ActiveX control can share the same Firebase auth state since they use the same PasswordVault entries

If the user signs in via the app, the ActiveX control picks it up on next load (and vice versa). No explicit state sharing mechanism needed — Firebase handles it.

### Email/Password Auth

No changes needed — the existing `signInWithEmailAsync` / `createAccountAsync` work directly from the XAML Island. These use Firebase C++ SDK which doesn't need a browser or protocol handler.

### WebView2 Dependency

WebView2 is already in the output directory (from the WindowsAppSDK NuGet). The `Microsoft.Web.WebView2.Core.dll` is copied by the project reference. No additional installation needed — WebView2 runtime is pre-installed on Windows 10 1903+ and all Windows 11.

## TODOs

- [ ] Verify `AuthRequestParams` exposes PKCE params for manual URL construction
- [ ] If not, implement PKCE generation (SHA256 + Base64URL) in C++
- [ ] Create `OAuthPopup` class in PivoxShared or PivoxActiveX
- [ ] WebView2 integration in WinUI Window (popup)
- [ ] NavigationStarting handler to intercept redirect
- [ ] Token exchange (try OAuth2Manager first, fallback to manual HTTP)
- [ ] Firebase SignInWithCredential with tokens
- [ ] Register `http://localhost` as redirect URI in Google/GitHub OAuth app configs
- [ ] Test inside iNEWS with `uiAccess=true`
- [ ] Handle auth cancellation (user closes popup)
- [ ] Handle auth errors (invalid credentials, network failure)
- [ ] Session persistence — verify Firebase auth state is shared between app and ActiveX
