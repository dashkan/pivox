# Authentication Architecture

## Overview

Pivox uses **Firebase Auth** as the identity provider across all clients. The auth system is organized into three layers:

1. **`@pivox/ui`** — Compound UI components (headless, context-driven)
2. **`@pivox/features`** — Business logic hooks wrapping Firebase Auth SDK
3. **App-specific wrappers** — Electron overrides for production `file://` constraints

The Go server provides token verification and custom token minting for the Electron OAuth flow.

## Providers

| Provider | Status | Sign-in method |
|----------|--------|----------------|
| Email/password | Active | `signInWithEmailAndPassword` / `createUserWithEmailAndPassword` |
| Google | Active | `signInWithPopup` (web) / `signInWithRedirect` (Electron bridge) |
| GitHub | Active | `signInWithPopup` (web) / `signInWithRedirect` (Electron bridge) |
| Apple | Planned | Not yet configured in Firebase Console |
| SSO (OIDC) | Planned | Per-tenant `OAuthProvider('oidc.pivox')` — will be added with tenant-based SSO |

Default providers for sign-in/sign-up/profile are `['google', 'github']`. Apple and SSO will be added to the defaults when implemented.

---

## Package Architecture

### UI Components (`@pivox/ui`)

All auth UI uses the compound component pattern: a `Provider` wraps children and injects context, individual components read from that context. This decouples UI from business logic — the feature layer provides the context value.

#### LoginCard

| Component | Purpose |
|-----------|---------|
| `LoginCard.Provider` | Context provider (receives `LoginContextValue`) |
| `LoginCard.Root` | `<form>` wrapper with card layout |
| `LoginCard.Header` | "Sign in" title |
| `LoginCard.EmailField` | Email input |
| `LoginCard.PasswordField` | Password input |
| `LoginCard.RememberMe` | Checkbox |
| `LoginCard.ForgotPassword` | Link (accepts `onClick`) |
| `LoginCard.SubmitButton` | Submit with error display |
| `LoginCard.Separator` | "or" divider |
| `LoginCard.SocialButtons` | OAuth buttons (defaults to `['google', 'github']`, override with `providers` prop) |
| `LoginCard.SSOButton` | Enterprise SSO button |
| `LoginCard.Footer` | "Don't have an account?" link |

**Types:**
- `LoginState`: `{ email, password, error }`
- `LoginActions`: `{ updateEmail, updatePassword, formAction, socialLogin(provider), ssoLogin() }`
- `LoginMeta`: `{ emailRef }`

#### RegistrationCard

| Component | Purpose |
|-----------|---------|
| `RegistrationCard.Provider` | Context provider |
| `RegistrationCard.Root` | Form wrapper |
| `RegistrationCard.Header` | "Create account" title |
| `RegistrationCard.EmailField` | Email input |
| `RegistrationCard.DisplayNameField` | Display name input |
| `RegistrationCard.PasswordField` | Password input |
| `RegistrationCard.ConfirmPasswordField` | Confirm password |
| `RegistrationCard.SubmitButton` | Submit with error display |
| `RegistrationCard.Separator` | Divider |
| `RegistrationCard.SocialButtons` | OAuth buttons (defaults to `['google', 'github']`) |
| `RegistrationCard.Footer` | "Already have an account?" link |

#### UserProfileCard

| Component | Purpose |
|-----------|---------|
| `UserProfileCard.Provider` | Context provider |
| `UserProfileCard.Root` | Dialog with sidebar |
| `UserProfileCard.Sidebar` | Account / Security page nav |
| `UserProfileCard.AccountPage` | Profile, email, connected accounts, delete account |
| `UserProfileCard.SecurityPage` | Password, TOTP 2FA |

**Account page subsections:** Profile editor (avatar, display name), email (with verification badge, change form), connected accounts (link/unlink OAuth providers), danger zone (delete account).

**Security page subsections:** Password (change or set if OAuth-only account), MFA (TOTP enrollment with QR code, verification, unenroll).

**Types:**
- `UserProfileState`: `{ displayName, email, photoURL, emailVerified, providers[], availableProviders[], activePage, error, success, mfaEnrolled, totpEnrollment }`
- `UserProfileActions`: `updateDisplayName`, `updatePhoto`, `setPhotoURL`, `removePhoto`, `setPassword`, `changePassword`, `sendVerificationEmail`, `changeEmail`, `linkProvider`, `unlinkProvider`, `startTotpEnrollment`, `verifyTotpEnrollment`, `cancelTotpEnrollment`, `unenrollTotp`, `deleteAccount`, `signOut`, `setActivePage`, `clearStatus`

#### Other Cards

| Card | Purpose | Key Firebase methods |
|------|---------|---------------------|
| `ForgotPasswordCard` | Request password reset email | `sendPasswordResetEmail` |
| `ResetPasswordCard` | Set new password from email link | `verifyPasswordResetCode`, `confirmPasswordReset` |
| `VerifyEmailCard` | Prompt to check email, resend | `sendEmailVerification` |
| `LinkAccountCard` | Link accounts with conflicting credentials | `signInWithEmailAndPassword`, `linkWithCredential` |

#### AppLayout

`AppLayout.HeaderAvatar` — shows skeleton when loading, "Sign in" button when unauthenticated, or avatar dropdown (with "Manage account" and "Sign out") when authenticated.

---

### Feature Hooks (`@pivox/features`)

Each feature exports a hook and a wrapper component. The hook calls Firebase Auth SDK methods and returns a context value that the UI compound components consume.

#### `auth` — Core auth state

| Export | Purpose |
|--------|---------|
| `AuthProvider` | Listens to `onIdTokenChanged`, provides user state |
| `useAuth()` | Returns `{ user, loading, signOut, refreshUser }` |

`refreshUser()` calls `user.reload()` then forces a re-render so consumers pick up updated `providerData`, `emailVerified`, etc.

#### `login` — Sign-in logic

| Export | Firebase methods |
|--------|-----------------|
| `useLogin(onSuccess?, onLinkRequired?)` | `signInWithEmailAndPassword`, `signInWithPopup` |
| `LoginFeature` | Wraps children with `LoginCard.Provider` |

Handles `auth/account-exists-with-different-credential` by storing the pending credential via `setPendingLink()` and calling `onLinkRequired(email)`.

Google provider configured with `prompt: 'select_account'` for explicit account selection.

#### `registration` — Account creation

| Export | Firebase methods |
|--------|-----------------|
| `useRegistration(onSuccess?, onLinkRequired?)` | `createUserWithEmailAndPassword`, `updateProfile`, `sendEmailVerification`, `signInWithPopup` |
| `RegistrationFeature` | Wraps children with `RegistrationCard.Provider` |

#### `user-profile` — Profile management

| Export | Firebase methods |
|--------|-----------------|
| `useUserProfile(onClose?)` | `updateProfile`, `verifyBeforeUpdateEmail`, `updatePassword`, `linkWithPopup`, `unlink`, `deleteUser`, `multiFactor`, `TotpMultiFactorGenerator`, `reauthenticateWithCredential`, `reauthenticateWithPopup` |
| `UserProfileFeature` | Wraps children with `UserProfileCard.Provider` |

Reauthentication helper: tries OAuth providers first (`reauthenticateWithPopup`), then falls back to email/password prompt (`reauthenticateWithCredential`). Required for: password change, email change, account deletion, MFA changes.

#### Other features

| Feature | Firebase methods | Purpose |
|---------|-----------------|---------|
| `forgot-password` | `sendPasswordResetEmail` | Request password reset |
| `reset-password` | `verifyPasswordResetCode`, `confirmPasswordReset` | Set new password |
| `verify-email` | `sendEmailVerification` | Resend verification |
| `link-account` | `signInWithEmailAndPassword`, `linkWithCredential` | Resolve credential conflicts |
| `app-layout` | Uses `useAuth()` | Manages avatar dropdown, profile dialog open state |

#### Shared utilities

| Export | Purpose |
|--------|---------|
| `firebaseErrorMessage(e)` | Maps 20+ Firebase error codes to user-friendly messages |
| `setPendingLink(link)` / `getPendingLink()` / `clearPendingLink()` | In-memory storage for account linking across navigation |

---

## Start App Auth Routes

All routes in `web/apps/start/src/routes/auth/`:

| Route | Component | Purpose |
|-------|-----------|---------|
| `/auth/login` | `LoginFeature` + `LoginCard` | Email/password + social sign-in |
| `/auth/register` | `RegistrationFeature` + `RegistrationCard` | New account creation |
| `/auth/forgot-password` | `ForgotPasswordFeature` + `ForgotPasswordCard` | Request password reset |
| `/auth/reset-password` | `ResetPasswordFeature` + `ResetPasswordCard` | Set new password (from email `oobCode`) |
| `/auth/verify-email` | `VerifyEmailFeature` + `VerifyEmailCard` | Email verification prompt |
| `/auth/link-account` | `LinkAccountFeature` + `LinkAccountCard` | Resolve credential conflicts |
| `/auth/action` | Custom handler | Firebase email actions (verify email, change email, recover email, revert 2FA) |
| `/auth/electron-login` | Custom | Electron OAuth bridge (see below) |
| `/auth/electron-link` | Custom | Electron provider linking bridge (see below) |
| `/auth/done` | Static | "You can close this browser tab" |
| `/auth/redirect` | Custom | Deep link forwarder (placeholder for future PKCE flow) |

---

## Electron Auth

### Problem

Production Electron builds use the `file://` protocol. Firebase Auth rejects `file://` as an invalid origin for OAuth popups and redirects. The solution opens the user's default browser for OAuth and communicates back via a `pivox://` custom protocol deep link.

### Dev vs Production

| | Dev | Production |
|---|-----|------------|
| **Origin** | `http://localhost:5173` (Vite dev server) | `file://` |
| **Social login** | `signInWithPopup` (standard) | External browser + deep link + `signInWithCustomToken` |
| **Provider linking** | `linkWithPopup` (standard) | External browser + deep link + `refreshUser` |
| **Protocol handler** | Not registered (macOS requires packaged `.app`) | Registered via `app.setAsDefaultProtocolClient('pivox')` |

### Electron-Specific Components

Located in `web/apps/electron/src/renderer/src/components/`:

#### `ElectronLoginFeature`

Wraps `useLogin` but overrides behavior in production:
- **Dev**: Delegates to standard `useLogin` (popup flow)
- **Prod**: Overrides `socialLogin` → calls `window.api.startSocialLogin(provider)` to open browser. Listens for `auth:deep-link` IPC callback, then calls `signInWithCustomToken(auth, token)`.

#### `ElectronUserProfileFeature`

Wraps `useUserProfile` but overrides `linkProvider` in production:
- **Dev**: Delegates to standard `useUserProfile` (popup flow)
- **Prod**: Overrides `linkProvider` → gets current user's ID token, calls `window.api.startLinkProvider(providerId, idToken)` to open browser. Listens for `auth:deep-link` with `linked=true`, then calls `refreshUser()` to update the UI.

### Electron Main Process

**File:** `web/apps/electron/src/main/index.ts`

**Protocol registration** (before `app.whenReady()`):
```
app.setAsDefaultProtocolClient('pivox')
```

**Deep link handlers:**
- macOS: `app.on('open-url')` — fires when the OS opens a `pivox://` URL
- Windows/Linux: `app.on('second-instance')` — finds `pivox://` in argv

**`handleDeepLink(url)`**: Parses `pivox://auth/callback?token=...&state=...&linked=...`, validates state nonce matches `pendingAuthState`, sends data to renderer via `mainWindow.webContents.send('auth:deep-link', data)`.

**IPC handlers:**
- `auth:start-social-login(provider)` — generates state nonce, opens `${BASE_URL}/auth/electron-login?provider=...&state=...`
- `auth:start-link-provider(provider, idToken)` — generates state nonce, opens `${BASE_URL}/auth/electron-link?provider=...&state=...&token=...`

`BASE_URL` comes from `PIVOX_WEB_URL` env var, defaults to `https://pivox.ngrok.app`.

### Electron Preload IPC Bridge

**File:** `web/apps/electron/src/preload/index.ts`

Exposes `window.api`:
- `startSocialLogin(provider: string): Promise<string>` — returns state nonce
- `startLinkProvider(provider: string, idToken: string): Promise<string>` — returns state nonce
- `onAuthDeepLink(callback): () => void` — listens for `auth:deep-link` events, returns cleanup function

### Social Login Flow (Production)

```
Electron                      Browser (Safari)                Go Server
───────                      ────────────────                ─────────
1. Click "Sign in with Google"
2. Generate state nonce
3. shell.openExternal() ──→  /auth/electron-login
                              ?provider=google&state=<nonce>
                          4. signOut (clean slate)
                          5. signInWithRedirect(GoogleProvider)
                              ──→ accounts.google.com
                          6. User picks account
                          7. Google redirects back
                          8. getRedirectResult() → user
                          9. user.getIdToken()
                         10. POST /internal/v1/auth:exchangeToken ──→
                                                              11. VerifyIDToken(idToken)
                                                              12. CreateCustomToken(uid)
                                                              ←── { custom_token }
                         13. Redirect to pivox://auth/callback
                              ?token=<customToken>&state=<nonce>
                         14. Navigate to /auth/done
15. open-url event
16. Validate state nonce
17. IPC → renderer
18. signInWithCustomToken(token)
19. User is signed in
```

### Provider Linking Flow (Production)

```
Electron                      Browser (Safari)                Go Server
───────                      ────────────────                ─────────
1. Click "Connect GitHub"
2. Get current user's ID token
3. shell.openExternal() ──→  /auth/electron-link
                              ?provider=github.com
                              &state=<nonce>
                              &token=<idToken>
                          4. POST /internal/v1/auth:exchangeToken ──→
                                                              5. VerifyIDToken → uid
                                                              6. CreateCustomToken(uid)
                                                              ←── { custom_token }
                          7. signInWithCustomToken(customToken)
                              (now signed in as the Electron user)
                          8. linkWithRedirect(user, GithubProvider)
                              ──→ github.com OAuth
                          9. User authorizes
                         10. GitHub redirects back
                         11. getRedirectResult() → linked
                         12. Redirect to pivox://auth/callback
                              ?state=<nonce>&linked=true
                         13. Navigate to /auth/done
14. open-url event
15. Validate state nonce
16. IPC → renderer
17. refreshUser()
18. Provider list updates
```

### Self-Hosted Firebase Auth Handler

Firebase's `signInWithRedirect` uses an intermediary page at `/__/auth/handler` on the `authDomain`. By default this is `pivox-cloud.firebaseapp.com`, but Safari's ITP blocks cross-origin cookies needed to pass the redirect result back.

**Solution:** Set `authDomain` to our own domain (`pivox.ngrok.app`) and proxy `/__/auth/**` to Firebase via Nitro route rules:

```ts
// vite.config.ts (start app)
nitro({
  routeRules: {
    '/__/auth/**': { proxy: 'https://pivox-cloud.firebaseapp.com/__/auth/**' },
  },
})
```

This means the Firebase auth handler runs same-origin. No cross-origin cookie issues.

**OAuth provider callback URLs** must be registered with the self-hosted domain:
- **Google**: Google Cloud Console → Credentials → OAuth Client → add `https://pivox.ngrok.app/__/auth/handler`
- **GitHub**: GitHub → Settings → Developer Settings → OAuth App → set callback to `https://pivox.ngrok.app/__/auth/handler`
- **Apple**: (when implemented) Apple Developer → Service IDs → add return URL

### Custom Protocol Registration

**electron-builder.yml:**
```yaml
protocols:
  - name: Pivox
    schemes:
      - pivox
```

**Platform behavior:**
- **macOS**: Requires packaged `.app` bundle. Does not work in dev mode.
- **Windows**: Works in dev mode via `process.defaultApp` workaround.
- **Linux**: Requires packaged build with `.desktop` file.

### CSRF Protection

Each OAuth flow generates a `crypto.randomUUID()` nonce stored as `pendingAuthState`. The deep link callback validates that the returned `state` matches. Mismatched state sends an error to the renderer.

---

## Go Server

### AuthService (`internal/firebase/auth.go`)

Wraps the Firebase Admin SDK for server-side auth operations.

| Method | Purpose |
|--------|---------|
| `NewAuthService(ctx, GoogleCloudConfig)` | Initialize with credentials |
| `VerifyIDToken(ctx, idToken)` | Verify Firebase ID token (crypto signature check) |
| `CreateCustomToken(ctx, uid)` | Mint custom token for `signInWithCustomToken` |
| `CreateTenant(ctx, displayName)` | Create Firebase Auth tenant |
| `UpdateTenantDisplayName(ctx, id, name)` | Update tenant name |
| `DeleteTenant(ctx, id)` | Delete tenant |

**Credential resolution order:**
1. `ServiceAccountKey` — inline JSON (for containers, CI)
2. `ServiceAccountFile` — path to JSON key file
3. `GOOGLE_APPLICATION_CREDENTIALS` env var — standard ADC
4. Application Default Credentials — metadata server, gcloud CLI, workload identity

### Token Exchange Endpoint

`POST /internal/v1/auth:exchangeToken`

Used by the Electron OAuth bridge pages (`electron-login`, `electron-link`). Authenticates via the Firebase ID token itself — no shared secret needed.

**Request:** `Authorization: Bearer <firebase-id-token>`
**Response:** `{ "custom_token": "<token>" }`

The handler calls `VerifyIDToken` (validates Google's cryptographic signature), then `CreateCustomToken(uid)` to mint a token that the Electron client can use with `signInWithCustomToken`.

### Account Sync Webhook

`POST /internal/v1/accounts:sync`

Called by Firebase Functions (`syncAccountOnCreate`, `syncAccountOnSignIn`). Upserts user records into the `accounts` database table. Protected by shared secret (`Authorization: Bearer <SHARED_SECRET>`).

---

## Environment Variables

### Go Server

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `GOOGLE_CLOUD_PROJECT_ID` | Yes (if using ADC) | — | Firebase project ID for token verification |
| `GOOGLE_CLOUD_SERVICE_ACCOUNT_KEY` | No | — | Inline JSON service account key |
| `GOOGLE_CLOUD_SERVICE_ACCOUNT_FILE` | No | — | Path to service account JSON key file |
| `SHARED_SECRET` | Yes | `dev-secret` | Secret for internal webhook auth |
| `DATABASE_URL` | Yes | `postgres://localhost:5432/pivox?sslmode=disable` | PostgreSQL connection |
| `GRPC_PORT` | No | `:50051` | gRPC server port |
| `REST_PORT` | No | `:8080` | REST gateway port |
| `DEBUG_PORT` | No | `:9090` | Health/readiness port |

### Start App (`web/apps/start/.env`)

| Variable | Required | Example | Purpose |
|----------|----------|---------|---------|
| `VITE_FIREBASE_API_KEY` | Yes | `AIzaSy...` | Firebase web API key |
| `VITE_FIREBASE_AUTH_DOMAIN` | Yes | `pivox.ngrok.app` | Must match the domain serving `/__/auth/handler` |
| `VITE_FIREBASE_PROJECT_ID` | Yes | `pivox-cloud` | Firebase project |
| `VITE_FIREBASE_STORAGE_BUCKET` | No | `pivox-cloud.firebasestorage.app` | Storage bucket |
| `VITE_FIREBASE_MESSAGING_SENDER_ID` | No | `45920224787` | FCM sender |
| `VITE_FIREBASE_APP_ID` | No | `1:459...` | Firebase app ID |
| `VITE_FIREBASE_AUTH_EMULATOR_URL` | No | `http://localhost:9099` | Connect to emulator in dev |

**Important:** `VITE_FIREBASE_AUTH_DOMAIN` must be set to the domain that serves the app (e.g., `pivox.ngrok.app`), NOT `pivox-cloud.firebaseapp.com`. The Nitro proxy at `/__/auth/**` handles the Firebase auth handler same-origin.

### Electron App

**Dev** (`web/apps/electron/.env`):

| Variable | Value | Notes |
|----------|-------|-------|
| `VITE_FIREBASE_API_KEY` | `fake-api-key` | Emulator doesn't validate |
| `VITE_FIREBASE_AUTH_DOMAIN` | `localhost` | Dev server |
| `VITE_FIREBASE_PROJECT_ID` | `pivox-cloud` | Must match |

**Production** (`web/apps/electron/.env.production`):

| Variable | Value | Notes |
|----------|-------|-------|
| `VITE_FIREBASE_API_KEY` | `AIzaSy...` | Real API key (safe to embed — it's public) |
| `VITE_FIREBASE_AUTH_DOMAIN` | `pivox-cloud.firebaseapp.com` | Standard Firebase domain (Electron uses `signInWithCustomToken`, not redirects) |
| `VITE_FIREBASE_PROJECT_ID` | `pivox-cloud` | Must match |

**Runtime** (main process):

| Variable | Default | Purpose |
|----------|---------|---------|
| `PIVOX_WEB_URL` | `https://pivox.ngrok.app` | Base URL for external browser OAuth pages |

---

## Trusted Provider Auto-Linking

Google is a "trusted/authoritative" provider for `@gmail.com` domains. When a user signs in with **any** Google account whose email matches an existing Firebase user's email, Firebase automatically links it — even if it's a different Google account (e.g., Workspace vs personal). This:

- Cannot be disabled via Firebase settings
- Applies to both web and Electron flows
- Can result in two `google.com` entries in `providerData`

**Mitigation options** (not currently implemented):
- `beforeSignIn` blocking function to validate Google `sub`
- Switch to "Create multiple accounts per identity provider" (complicates data model)

---

## Firebase Functions

- **`syncAccountOnCreate`** — Blocking function on user creation, POSTs to `accounts:sync`
- **`syncAccountOnSignIn`** — Blocking function on sign-in, POSTs to `accounts:sync`

---

## Building and Testing

### Start app (dev)

```bash
cd web && pnpm --filter start run dev
```

### Electron (dev — popup auth works)

```bash
cd web && pnpm --filter electron run dev
```

### Electron (production build — tests deep link flow)

```bash
cd web && pnpm --filter electron run build:unpack
open web/apps/electron/dist/mac-arm64/pivox-electron.app
```

**macOS note:** `app.setAsDefaultProtocolClient` requires a packaged `.app`. The protocol handler does not work with `electron-vite dev`.

To open devtools in production: **Cmd+Option+I**

### Simulate deep link (devtools console)

```js
mainWindow.webContents.send('auth:deep-link', { token: 'test', state: 'test' })
```

### Test token exchange endpoint

```bash
curl -X POST localhost:8080/internal/v1/auth:exchangeToken \
  -H "Authorization: Bearer <firebase-id-token>"
```

---

## Planned: Apple Sign-In

Apple provider is defined in the codebase (`OAuthProvider('apple.com')`) but not yet configured in Firebase Console or Apple Developer portal. When implementing:

1. Configure in Firebase Console → Authentication → Sign-in method → Apple
2. Configure in Apple Developer → Certificates, Identifiers & Profiles → Service IDs
3. Add `https://pivox.ngrok.app/__/auth/handler` as a return URL
4. Add `'apple'` to the default providers in `SocialButtons` components and `oauthProviders` in `use-user-profile.ts`

## Planned: Tenant-Based SSO

Multi-tenant SSO using Firebase Auth tenants and custom OIDC providers. The `ssoLogin` action in `useLogin` already calls `signInWithPopup(auth, new OAuthProvider('oidc.pivox'))`. When implementing:

1. Use `AuthService.CreateTenant` to create per-organization tenants
2. Configure OIDC providers per tenant in Firebase Console
3. Route SSO sign-in through the tenant's auth instance
4. Document the full SSO flow here

---

## Firebase Auth SDK Methods Reference

### Authentication
| Method | Used in | Purpose |
|--------|---------|---------|
| `signInWithEmailAndPassword` | `useLogin`, `useLinkAccount` | Email/password sign-in |
| `createUserWithEmailAndPassword` | `useRegistration` | Account creation |
| `signInWithPopup` | `useLogin`, `useRegistration` | OAuth sign-in (web/dev) |
| `signInWithRedirect` | `electron-login`, `electron-link` | OAuth sign-in (Electron bridge) |
| `getRedirectResult` | `electron-login`, `electron-link` | Retrieve redirect result |
| `signInWithCustomToken` | `ElectronLoginFeature`, `electron-link` | Sign in with server-minted token |
| `signOut` | `AuthProvider`, `useUserProfile`, `electron-login` | Sign out |

### User Management
| Method | Used in | Purpose |
|--------|---------|---------|
| `updateProfile` | `useRegistration`, `useUserProfile` | Set display name, photo URL |
| `verifyBeforeUpdateEmail` | `useUserProfile` | Change email with verification |
| `sendEmailVerification` | `useRegistration`, `useVerifyEmail`, `useUserProfile` | Send verification email |
| `deleteUser` | `useUserProfile` | Delete account |
| `user.reload()` | `AuthProvider.refreshUser` | Refresh user properties |
| `user.getIdToken()` | `ElectronLoginFeature`, `ElectronUserProfileFeature`, `electron-login` | Get ID token for server calls |

### Password
| Method | Used in | Purpose |
|--------|---------|---------|
| `updatePassword` | `useUserProfile` | Change password |
| `sendPasswordResetEmail` | `useForgotPassword` | Request reset email |
| `verifyPasswordResetCode` | `useResetPassword` | Validate reset code |
| `confirmPasswordReset` | `useResetPassword` | Set new password |

### Account Linking
| Method | Used in | Purpose |
|--------|---------|---------|
| `linkWithCredential` | `useLinkAccount`, `useUserProfile` | Link email/password |
| `linkWithPopup` | `useUserProfile` | Link OAuth provider (web/dev) |
| `linkWithRedirect` | `electron-link` | Link OAuth provider (Electron bridge) |
| `unlink` | `useUserProfile` | Remove provider |
| `OAuthProvider.credentialFromError` | `useLogin`, `useRegistration` | Extract credential from linking error |

### Reauthentication
| Method | Used in | Purpose |
|--------|---------|---------|
| `reauthenticateWithCredential` | `useUserProfile` | Reauth with email/password |
| `reauthenticateWithPopup` | `useUserProfile` | Reauth with OAuth |

### Multi-Factor Auth
| Method | Used in | Purpose |
|--------|---------|---------|
| `multiFactor(user).getSession()` | `useUserProfile` | Start MFA enrollment |
| `TotpMultiFactorGenerator.generateSecret` | `useUserProfile` | Generate TOTP secret + QR |
| `TotpMultiFactorGenerator.assertionForEnrollment` | `useUserProfile` | Verify TOTP code |
| `multiFactor(user).enroll` | `useUserProfile` | Finalize MFA enrollment |
| `multiFactor(user).unenroll` | `useUserProfile` | Remove MFA factor |

### Email Actions
| Method | Used in | Purpose |
|--------|---------|---------|
| `applyActionCode` | `action.tsx` | Apply email action (verify, change, recover) |
| `checkActionCode` | `action.tsx` | Validate action code before applying |

### Auth State
| Method | Used in | Purpose |
|--------|---------|---------|
| `onIdTokenChanged` | `AuthProvider` | Listen for auth state + token changes |
| `getAuth` | Everywhere | Get Firebase Auth instance |
