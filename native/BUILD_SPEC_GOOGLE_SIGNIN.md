# Windows Build Spec: Google Sign-In via OAuth2Manager

## Context

macOS Google Sign-In is working via ASWebAuthenticationSession + PKCE → Firebase credential. Windows needs the same flow using Windows App SDK's OAuth2Manager (available in 1.8 which we already use).

The "Continue with Google" button exists in the UI. `LoginPage::OnGoogleSignIn` currently calls `validateGoogleSignIn()` then hits a TODO. We need to implement the actual OAuth flow.

## Architecture

The flow is:
1. User clicks "Continue with Google"
2. Generate PKCE code_verifier + code_challenge (S256)
3. Open browser to `https://accounts.google.com/o/oauth2/v2/auth` with PKCE params
4. User signs in at Google
5. Browser redirects to `pivox://oauth-callback/?code=...&state=...`
6. App receives protocol activation → extract auth code
7. Exchange auth code for tokens (POST to `https://oauth2.googleapis.com/token`)
8. Create Firebase credential: `GoogleAuthProvider::GetCredential(id_token, access_token)`
9. Sign in to Firebase: `auth->SignInWithCredential(credential)`
10. Save tokens, update auth state, navigate to main app

## Important: Do NOT use OAuth2Manager

After research, OAuth2Manager requires app packaging (MSIX) and specific activation kinds that add complexity. Instead, use the same pattern macOS uses — manual PKCE OAuth with `ShellExecuteW` to open the browser and protocol activation (`pivox://`) to receive the callback. This is simpler, proven, and works identically to the macOS flow.

## Implementation Details

### PKCE helpers (in WinAuthService)
```cpp
// Generate 32 random bytes, base64url encode → code_verifier
// SHA256(code_verifier), base64url encode → code_challenge
```
Use `BCryptGenRandom` for random bytes, `BCryptCreateHash`/`BCryptHashData` for SHA256 (all built-in Windows APIs, no extra dependencies).

### Google OAuth URL parameters
- `client_id`: from `firebase_config::kGoogleSignInClientId` (already configured: `45920224787-332q0atab40vmojtf0admuvtvm8bgfa4.apps.googleusercontent.com`)
- `redirect_uri`: `pivox://oauth-callback/` (already registered in HKCU)
- `response_type`: `code`
- `scope`: `openid email profile`
- `code_challenge`: SHA256 of code_verifier, base64url encoded
- `code_challenge_method`: `S256`
- `state`: random nonce for CSRF protection

### Token exchange
POST to `https://oauth2.googleapis.com/token`:
```
code=AUTH_CODE
client_id=CLIENT_ID
redirect_uri=pivox://oauth-callback/
grant_type=authorization_code
code_verifier=CODE_VERIFIER
```
Response contains `id_token` and `access_token`.

**Note**: Desktop OAuth clients (type "Desktop app" in Google Cloud Console) do NOT have a client_secret. The PKCE flow replaces it. Do NOT include client_secret in the token exchange.

### Protocol activation handling
The app already registers `pivox://` via `WinAppState::registerProtocolHandler()`. You need to handle the incoming URL when the browser redirects back.

Option A (preferred): Use `Microsoft::Windows::AppLifecycle::AppInstance::GetCurrent().Activated` event to receive protocol activation while the app is running.

Option B: Check command-line arguments on app activation for the URL.

Store the pending PKCE state (code_verifier, state nonce) in WinAuthService so it's available when the callback arrives.

### Firebase credential
```cpp
#if PIVOX_HAS_FIREBASE
auto credential = firebase::auth::GoogleAuthProvider::GetCredential(
    id_token.c_str(), access_token.c_str());
auto future = firebaseAuth_->SignInWithCredential(credential);
// Wait for completion, map result to AuthResult
#endif
```

### What to modify

**WinAuthService.h** — Add:
```cpp
// New async method for Google sign-in
void signInWithGoogleAsync(std::function<void(AuthResult)> callback);

// Protocol activation handler (call from App when pivox:// URL received)
void handleOAuthCallback(const std::string& callbackUrl);
```

Plus private members:
```cpp
std::string pendingCodeVerifier_;
std::string pendingStateNonce_;
std::function<void(AuthResult)> pendingOAuthCallback_;
bool isOAuthInProgress_ = false;
```

**WinAuthService.cpp** — Add:
- PKCE helper functions (generateCodeVerifier, generateCodeChallenge)
- `signInWithGoogleAsync()` — builds URL, opens browser via `ShellExecuteW`, stores PKCE state
- `handleOAuthCallback()` — validates state, extracts code, exchanges for tokens, creates Firebase credential, calls callback
- Token exchange via `WinHttpOpen`/`WinHttpConnect`/`WinHttpSendRequest` (built-in WinHTTP) or `winrt::Windows::Web::Http::HttpClient`

**App.xaml.h / App.xaml.cpp** — Add protocol activation handler:
- Register for `AppInstance::GetCurrent().Activated()` event
- When activated with protocol, call `WinAuthService::handleOAuthCallback(url)`

**LoginPage.xaml.cpp** — Update `OnGoogleSignIn`:
- Replace the TODO with actual call to `signInWithGoogleAsync`
- Show loading state while waiting
- Handle success (navigate to main) and failure (show error)

**RegisterPage.xaml.cpp** — Same for the Google button on the register page.

### Guard against concurrent clicks
Same as macOS: `isOAuthInProgress_` bool. If true, ignore subsequent clicks. Reset on completion or error.

### Error handling
- User cancels (closes browser without completing) — need a timeout or the callback simply never fires. Consider a 5-minute timeout that resets `isOAuthInProgress_`.
- Invalid state nonce — reject the callback, show error.
- Token exchange fails — show `auth_error::kUnknown`.
- Firebase credential fails — map via `mapFirebaseError`.
- Network errors — show `auth_error::kNetworkError`.

### Use shared error constants
All user-facing error messages must use `pivox::auth_error::k*` from `core/auth_state.h`.

## Testing

### Unit tests (gtest) — add to win_auth_service_tests.cpp:
- `signInWithGoogleAsync` returns error when not configured (no googleClientId)
- `handleOAuthCallback` rejects URL with wrong state nonce
- `handleOAuthCallback` rejects URL with no code parameter
- `isOAuthInProgress_` prevents concurrent sign-in attempts

### Manual test:
- Click "Continue with Google" → browser opens to Google sign-in
- Sign in → browser redirects to `pivox://oauth-callback/`
- App receives callback → shows loading → navigates to main app
- User info (email, display name) shown in sidebar profile

## Acceptance criteria
- Google Sign-In flow completes end-to-end on Windows
- User arrives at main app with correct profile data after Google sign-in
- Concurrent click protection works (rapid clicks don't open multiple browsers)
- Error states handled gracefully with shared error constants
- All existing tests still pass
- New unit tests for the OAuth flow added
- Commit and push when done
