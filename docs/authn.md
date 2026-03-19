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

`refreshUser()` calls `user.reload()` to fetch fresh user properties from Firebase, then calls a `patchProviderData()` workaround to fix the SDK's stale `providerData`. Called automatically each time the user profile dialog opens (not on mount — the dialog stays mounted when closed, so a mount-based effect would only run once).

**Firebase SDK `providerData` bug:** The SDK's internal `mergeProviderData()` function (in `_reloadWithoutSaving`) merges old + new providers instead of replacing. Unlinked providers are absent from the server response, so they survive the merge and are never removed. The `patchProviderData()` workaround calls the `accounts:lookup` REST API directly and splices out providers that the server no longer returns. This is necessary because `reload()`, `getIdToken(true)`, and re-reading `auth.currentUser` all fail to remove unlinked providers from the cached `providerData` array. See `mergeProviderData()` in `@firebase/auth` ESM bundle (~line 1545).

**Why Firebase can't notify us of provider changes:** Firebase Auth provides only four Cloud Functions triggers: `beforeUserCreated` (blocking), `beforeUserSignedIn` (blocking), `onCreate` (v1, non-blocking), and `onDelete` (v1, non-blocking). There are no triggers for provider link/unlink, profile updates, email changes, or MFA enrollment. The Admin SDK is request-response only — no subscriptions or watches. This means there is no server-side way to reactively detect when a user's providers change.

**Planned replacement:** The Watch API (`pivox.api.v1.Watcher`) will expose user resources (with providers as a sub-message) as watchable entities with real-time change events streamed via SSE. When `linkProvider` or `unlinkProvider` succeeds on the client, it will notify the backend to emit a Watch change event. Other clients subscribed to that user's stream receive the update in real-time — no polling, no REST API workaround.

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

**Linking timeout:** When the user starts linking a provider, all link/unlink buttons are disabled until the flow completes or times out after 2 minutes. An info message is shown in the Connected Accounts section. The timeout applies to both web (`linkWithPopup`) and Electron (`startLinkProvider` deep link flow). State is tracked via `linkingProvider` in `UserProfileState`.

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
| `/auth/external-login` | Custom | Electron OAuth bridge (see below) |
| `/auth/external-link` | Custom | Electron provider linking bridge (see below) |
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
- **Prod**: Overrides `linkProvider` → sets `linkingProvider` state, gets current user's ID token, calls `window.api.startLinkProvider(providerId, idToken)` to open browser. Listens for `auth:deep-link` with `linked=true`, then clears `linkingProvider` and calls `refreshUser()` to update the UI. Times out after 2 minutes if the deep link never arrives.

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

### Isolated Firebase App on External Pages

The `external-login` and `external-link` pages each create a **named Firebase app instance** (e.g., `initializeApp(config, 'external-link')`) instead of using the default app. This isolates their IndexedDB state from other tabs. Without this, stale redirect/user data from a previous run causes the Firebase SDK to hang indefinitely on initialization, blocking the entire OAuth flow. Each run deletes and recreates the named app to start fresh.

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
| `ALLOWED_SERVICE_ACCOUNTS` | Yes (prod) | — | Comma-separated service account emails allowed to call internal endpoints |
| `AUDIENCE` | Yes (prod) | — | Expected audience in OIDC tokens (backend URL, e.g., `https://api.pivox.app`) |
| `SHARED_SECRET` | Yes (dev only) | — | Secret for internal webhook auth (only with `go build -tags dev`) |
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

## Development Workflows

There are three development modes, each with different trade-offs between fidelity and convenience.

### Mode 1: Full Stack with ngrok

Uses a real Firebase project via an ngrok tunnel, so OAuth with real Google/GitHub accounts works end-to-end. This is the closest to production. Use when you need real OAuth flows or are testing the Electron deep link path.

**What runs:**

```bash
pnpm prod          # Start app + Go server + nginx + ngrok (no Electron)
pnpm prod:electron # Same + Electron dev server
```

This launches (via `concurrently`):

| Process | Port | Purpose |
|---------|------|---------|
| Vite (start app) | 3001 | TanStack Start dev server |
| Go server (REST) | 8080 | REST gateway + internal hooks |
| Go server (gRPC) | 50051 | gRPC services |
| Go server (debug) | 9090 | Health/readiness probes |
| nginx | 8081 | Reverse proxy (single entry point) |
| ngrok | — | Tunnel `localhost:8081` → `pivox.ngrok.app` |
| Electron (if `prod:electron`) | 5173 | Electron renderer HMR server |
| PostgreSQL (docker) | 5432 | Database |

**nginx routing** (`configs/nginx.conf`):

| Path | Backend |
|------|---------|
| `/pivox.*`, `/google.*`, `/grpc.reflection.*` | gRPC → `:50051` |
| `/internal/*` | REST → `:8080` |
| `/v1/*` | REST → `:8080` |
| `/healthz`, `/readyz` | Debug → `:9090` |
| `/*` | Vite → `:3001` |

**Auth behavior:**
- Start app uses **real Firebase Auth** (`VITE_FIREBASE_AUTH_DOMAIN=pivox.ngrok.app`, emulator URL commented out)
- `/__/auth/**` Nitro proxy routes Firebase auth handler same-origin through ngrok
- Electron dev renderer connects to **Firebase emulator** on `:9099` (hardcoded `import.meta.env.DEV` check)
- Electron dev uses `signInWithPopup` (popup works because dev renderer runs on `http://localhost:5173`, a valid origin)
- Firebase Functions are deployed to GCP and call back through ngrok to `localhost:8080`
- Go server uses real Firebase credentials to verify ID tokens and mint custom tokens
- `accounts:sync` is protected by `SHARED_SECRET` (static bearer token)

**Prerequisites:**
1. `docker compose up -d` (PostgreSQL)
2. `pnpm db:migrate:up` (run migrations)
3. ngrok account with reserved domain `pivox.ngrok.app`
4. Real Firebase project credentials (service account key or ADC)
5. `SHARED_SECRET` env var set for Go server
6. Firebase Functions deployed with matching `PIVOX_SHARED_SECRET`

---

### Mode 2: Built Electron against dev backend

Tests the production Electron code paths (deep links, `pivox://` protocol handler, `signInWithCustomToken`) against the local dev stack. This is the only way to test the full Electron OAuth flow end-to-end.

**Why this matters:** Electron dev mode (`electron-vite dev`) uses `signInWithPopup` for social login and `linkWithPopup` for provider linking — it never exercises the deep link flow, the external browser bridge pages, the token deposit/consume endpoints, or the `pivox://` protocol handler. These production-only code paths need a way to be tested.

**Setup:**

```bash
# Terminal 1: Run the full stack with ngrok (provides backend + start app + real Firebase)
pnpm prod

# Terminal 2: Build and open the packaged Electron app
pnpm electron:build:unpack
open web/apps/electron/dist/mac-arm64/pivox-electron.app
```

**How it works:**

The built `.app` uses `.env.production` values baked at build time:
- `VITE_FIREBASE_API_KEY=AIzaSy...` (real API key)
- `VITE_FIREBASE_AUTH_DOMAIN=pivox-cloud.firebaseapp.com`
- `VITE_FIREBASE_PROJECT_ID=pivox-cloud`

The main process reads `PIVOX_WEB_URL` at runtime (defaults to `https://pivox.ngrok.app`). Since `pnpm prod` runs ngrok, the built Electron app's OAuth flow goes through ngrok to the local start app and Go backend.

**Flow when clicking "Sign in with Google":**
1. Built Electron main process opens `https://pivox.ngrok.app/auth/external-login?provider=google&state=<nonce>`
2. ngrok routes to nginx → Vite (local start app)
3. Start app runs `signInWithRedirect` → Google OAuth → Google redirects back to `pivox.ngrok.app/__/auth/handler`
4. Nitro proxy serves Firebase auth handler same-origin → redirect result lands on `external-login` page
5. Page calls `POST /internal/v1/auth:exchangeToken` → ngrok → nginx → Go server (`:8080`)
6. Go server verifies ID token, mints custom token
7. Page triggers `pivox://auth/callback?token=<customToken>&state=<nonce>` deep link
8. macOS routes `pivox://` to the built `.app` → main process validates state → renderer calls `signInWithCustomToken`

**To override `PIVOX_WEB_URL` at runtime:**
```bash
PIVOX_WEB_URL=https://pivox.ngrok.app open web/apps/electron/dist/mac-arm64/pivox-electron.app
```

**macOS notes:**
- `app.setAsDefaultProtocolClient('pivox')` requires a packaged `.app`. The `pivox://` protocol handler does not work with `electron-vite dev`.
- To open devtools in the built app: set `PIVOX_ENABLE_DEVTOOLS=1` environment variable, then use **Cmd+Option+I**.

**Simulate deep link (devtools console):**
```js
mainWindow.webContents.send('auth:deep-link', { token: 'test', state: 'test' })
```

**Test token exchange endpoint directly:**
```bash
curl -X POST localhost:8080/internal/v1/auth:exchangeToken \
  -H "Authorization: Bearer <firebase-id-token>"
```

---

### Mode 3: Pure localhost (planned — will become the default)

The default dev mode. No ngrok, no external network dependencies, no real Firebase project required. Everything runs locally against the Firebase Auth emulator.

**Status:** Not yet implemented. This section documents the design, trade-offs, and known limitations for when this mode is built.

#### Motivation

- Faster startup (no ngrok connection, no nginx needed for simple cases)
- Works offline / behind restrictive firewalls
- No dependency on ngrok reserved domain or Firebase project credentials
- Enables isolated testing without touching shared Firebase project state
- CI/CD friendly — no external service dependencies for integration tests

#### Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│  localhost                                                       │
│                                                                  │
│  ┌─────────────┐   ┌───────────────┐   ┌────────────────────┐  │
│  │ Electron    │   │ Start app     │   │ Firebase Emulator  │  │
│  │ (dev or     │   │ Vite :3001    │   │ Auth :9099         │  │
│  │ built .app) │   │               │   │ Functions :5001    │  │
│  └──────┬──────┘   └───────┬───────┘   └────────┬───────────┘  │
│         │                  │                     │              │
│         │   ┌──────────────┴──────────────┐      │              │
│         │   │ nginx :8081 (optional)      │      │              │
│         │   │ or direct access to :3001   │      │              │
│         │   └──────────────┬──────────────┘      │              │
│         │                  │                     │              │
│         │   ┌──────────────┴──────────────┐      │              │
│         └──►│ Go server                   │◄─────┘              │
│             │ REST :8080 / gRPC :50051    │                     │
│             └──────────────┬──────────────┘                     │
│                            │                                    │
│             ┌──────────────┴──────────────┐                     │
│             │ PostgreSQL :5432            │                     │
│             └─────────────────────────────┘                     │
└──────────────────────────────────────────────────────────────────┘
```

#### What changes from Mode 1

| Concern | Mode 1 (ngrok) | Mode 3 (localhost) |
|---------|----------------|--------------------|
| **Firebase Auth** | Real project (`pivox-cloud`) | Emulator on `:9099` |
| **Firebase Functions** | Deployed to GCP | Emulator on `:5001` |
| **OAuth providers** | Real Google/GitHub OAuth | Emulator test accounts (email/password only) |
| **ngrok** | Required | Not needed |
| **nginx** | Required (single entry point for ngrok) | Optional (direct port access works) |
| **Go server token verification** | Real Firebase Admin SDK verification | Emulator-compatible verification (skips signature check) |
| **`accounts:sync` auth** | Static shared secret (or OIDC — planned) | Static shared secret via `dev` build tag |
| **`VITE_FIREBASE_AUTH_DOMAIN`** | `pivox.ngrok.app` | `localhost` |
| **`VITE_FIREBASE_AUTH_EMULATOR_URL`** | Not set (commented out) | `http://localhost:9099` |
| **`/__/auth/**` Nitro proxy** | Proxies to `pivox-cloud.firebaseapp.com` | Not needed (emulator handles auth locally) |
| **ID token signatures** | Cryptographically verified (Google RSA keys) | Not verified (emulator mode) |

#### Dev command (planned)

```bash
pnpm dev          # Default — pure localhost
pnpm dev:electron # Same + Electron dev server
```

Would launch (without ngrok or nginx):

| Process | Port | Purpose |
|---------|------|---------|
| Vite (start app) | 3001 | TanStack Start dev server |
| Go server | 8080 / 50051 / 9090 | REST + gRPC + debug |
| Firebase emulators | 9099, 5001 | Auth + Functions |
| PostgreSQL (docker) | 5432 | Database |

#### Environment configuration (localhost mode)

**Start app** (`web/apps/start/.env` or `.env.local`):
```bash
VITE_FIREBASE_API_KEY=fake-api-key
VITE_FIREBASE_AUTH_DOMAIN=localhost
VITE_FIREBASE_PROJECT_ID=pivox-cloud
VITE_FIREBASE_AUTH_EMULATOR_URL=http://localhost:9099
```

**Electron dev** (`web/apps/electron/.env` — already configured for emulator):
```bash
VITE_FIREBASE_API_KEY=fake-api-key
VITE_FIREBASE_AUTH_DOMAIN=localhost
VITE_FIREBASE_PROJECT_ID=pivox-cloud
```
Electron's `ensureFirebase()` already calls `connectAuthEmulator` when `import.meta.env.DEV` is true.

**Go server:**
```bash
DATABASE_URL=postgres://localhost:5432/pivox?sslmode=disable
SHARED_SECRET=dev-secret          # Only used in dev build tag
FIREBASE_AUTH_EMULATOR_HOST=localhost:9099  # Tells Firebase Admin SDK to use emulator
GOOGLE_CLOUD_PROJECT_ID=pivox-cloud
```
When `FIREBASE_AUTH_EMULATOR_HOST` is set, the Firebase Admin SDK's `VerifyIDToken` skips cryptographic signature verification and talks to the emulator instead. No service account credentials needed.

**Firebase Functions emulator** (`deployments/firebase/functions/.env`):
```bash
PIVOX_API_URL=http://localhost:8080
PIVOX_SHARED_SECRET=dev-secret
```
The functions emulator reads `.env` in the functions directory and calls the Go server directly on localhost.

#### Go server: dev build tag

The Go server needs a different auth middleware for `accounts:sync` in localhost mode. In production (and in Mode 1 with ngrok), this will use Google Cloud OIDC identity tokens. In pure localhost mode, it falls back to the shared secret because the Firebase Functions emulator can't mint OIDC tokens.

This is implemented via Go build tags:

```
internal/server/
├── internal_hooks.go              # Shared: struct, Register(), handlers
├── internal_hooks_sync_auth.go    # //go:build !dev → OIDC verification
└── internal_hooks_sync_auth_dev.go # //go:build dev  → shared secret
```

Build with: `go run -tags dev ./cmd/server` (localhost) vs `go run ./cmd/server` (production).

The `dev` tag is opt-in for local work only. Production builds must never include it. CI should never pass `-tags dev`.

#### Known limitations of pure localhost mode

##### 1. No real OAuth (Google, GitHub, Apple)

The Firebase Auth emulator does not support real OAuth provider flows. `signInWithPopup` and `signInWithRedirect` against real Google/GitHub accounts will not work.

**Workaround:** Use email/password accounts created in the emulator. The emulator UI (`http://localhost:4000/auth`) lets you create test users, set `emailVerified`, and manage accounts manually.

**What this means for development:**
- ✅ Email/password sign-in, registration, password reset, email verification
- ✅ Profile management (display name, photo URL)
- ✅ Password change, account deletion
- ❌ Google/GitHub/Apple OAuth sign-in
- ❌ OAuth account linking / unlinking
- ❌ `auth/account-exists-with-different-credential` flow (link-account page)
- ❌ Trusted provider auto-linking behavior

##### 2. No Electron deep link flow (dev mode)

Electron dev mode (`electron-vite dev`) uses `signInWithPopup`, not the redirect + deep link flow. Since real OAuth doesn't work with the emulator, social login in Electron dev mode is non-functional in pure localhost.

**Workaround:** Use email/password sign-in in Electron dev, which works fully against the emulator.

##### 3. TOTP 2FA is limited in the emulator

The Firebase Auth emulator has partial MFA support. TOTP enrollment may not generate real QR codes or validate TOTP codes the same way production does.

**Workaround:** Test TOTP 2FA flows in Mode 1 (ngrok) against the real Firebase project.

##### 4. Firebase email actions don't send real emails

The emulator doesn't send verification emails, password reset emails, or email change emails. Action codes are visible in the emulator UI logs.

**Workaround:** Copy action links from the emulator console output or UI.

##### 5. `/__/auth/**` Nitro proxy is unnecessary but harmless

The `vite.config.ts` route rule proxying `/__/auth/**` to `pivox-cloud.firebaseapp.com` is not used when the emulator handles auth. It can remain in the config — the route is simply never hit.

##### 6. Account sync timing differs

In Mode 1, Firebase Functions run on GCP and call back through ngrok with real network latency. In localhost mode, the Functions emulator calls `localhost:8080` directly — sync is near-instantaneous. Edge cases around sync timeout or blocking function failure may behave differently.

#### Built Electron against localhost (planned)

This is the intersection of Mode 2 and Mode 3: testing the production Electron deep link flow without ngrok.

**The core problem:** The built Electron `.app` bakes `.env.production` values at compile time (real Firebase API key, `authDomain=pivox-cloud.firebaseapp.com`). These values cannot be changed at runtime because Vite inlines `import.meta.env.*` at build time.

**Options under consideration:**

**Option A: Separate `.env.local-prod` build target**

Create a new env file and Electron build script:

```bash
# web/apps/electron/.env.local-prod
VITE_FIREBASE_API_KEY=fake-api-key
VITE_FIREBASE_AUTH_DOMAIN=localhost
VITE_FIREBASE_PROJECT_ID=pivox-cloud
VITE_FIREBASE_AUTH_EMULATOR_URL=http://localhost:9099
```

```bash
pnpm electron:build:local   # builds with .env.local-prod
```

This produces a `.app` that:
- Uses the Firebase emulator (renderer connects to `:9099`)
- Does NOT use `import.meta.env.DEV` branches (production code paths)
- Uses deep links, `pivox://` protocol, `signInWithCustomToken`
- Sets `PIVOX_WEB_URL=http://localhost:8081` (or `:3001`) at runtime

**Limitation:** Real OAuth still doesn't work (emulator). The deep link flow can be tested with email/password by adjusting the bridge pages, or by using a custom test flow that bypasses OAuth redirect and directly mints tokens.

**Option B: Runtime config injection**

The Electron main process could fetch runtime config from a local endpoint or read a config file, then inject it into the renderer via IPC before Firebase initializes.

**Concern:** Adds complexity and a race condition (renderer must wait for config before initializing Firebase).

**Option C: Built Electron + ngrok (current Mode 2)**

Accept that testing the deep link flow requires ngrok for real OAuth. This is already documented and working.

**Recommendation:** Start with Option A for emulator-based testing of the deep link plumbing (protocol handler registration, state validation, IPC communication, `signInWithCustomToken`), and continue using Mode 2 (ngrok) for full end-to-end OAuth testing. The deep link flow has enough moving parts that testing it against the emulator catches most bugs even without real OAuth.

#### Port map (all modes)

| Service | Port | Mode 1 | Mode 3 |
|---------|------|--------|--------|
| PostgreSQL | 5432 | ✅ | ✅ |
| Vite (start app) | 3001 | ✅ | ✅ |
| Electron dev renderer | 5173 | ✅ | ✅ |
| Go REST gateway | 8080 | ✅ | ✅ |
| nginx | 8081 | ✅ | Optional |
| Firebase Auth emulator | 9099 | ❌ (real Firebase) | ✅ |
| Firebase Functions emulator | 5001 | ❌ (deployed to GCP) | ✅ |
| Firebase Emulator UI | 4000 | ❌ | ✅ |
| Go debug (health) | 9090 | ✅ | ✅ |
| Go gRPC | 50051 | ✅ | ✅ |

#### Files that require changes for localhost mode

| File | Change |
|------|--------|
| `package.json` | Rename `dev`→`prod`, `dev:electron`→`prod:electron`. New `dev` and `dev:electron` scripts (no ngrok/nginx, add Firebase emulators) |
| `internal/server/internal_hooks_sync_auth_dev.go` | New file: `//go:build dev` shared secret middleware |
| `internal/server/internal_hooks_sync_auth.go` | New file: `//go:build !dev` OIDC middleware |
| `internal/server/internal_hooks.go` | Refactor `NewInternalHooks` to use injected `syncAuth` middleware |
| `internal/config/config.go` | Split config by build tag (dev needs `SHARED_SECRET`, prod needs `ALLOWED_SERVICE_ACCOUNTS` + `AUDIENCE`) |
| `web/apps/start/.env.example` | Already correct (has emulator URL) |
| `web/apps/start/vite.config.ts` | No change needed (`/__/auth/**` proxy is harmless when unused) |
| `web/apps/electron/.env` | Already correct (fake key, `localhost` domain) |
| `deployments/firebase/functions/.env` | Ensure `PIVOX_API_URL=http://localhost:8080` and `PIVOX_SHARED_SECRET=dev-secret` |
| `firebase.json` | Already configured for emulators (auth `:9099`, functions `:5001`) |
| `.firebase-data/` | Directory for emulator state persistence (in `.gitignore`) |

#### Firebase emulator data persistence

The emulator can persist auth state across restarts:

```bash
pnpm firebase:emulators
# Equivalent to: firebase emulators:start --import=.firebase-data --export-on-exit=.firebase-data
```

This saves emulator state (users, tokens, etc.) to `.firebase-data/` on exit and restores it on next start. The directory is gitignored.

---

## Planned: OIDC Service-to-Service Auth for `accounts:sync`

Replace the static `SHARED_SECRET` on the `POST /internal/v1/accounts:sync` endpoint with Google Cloud OIDC identity tokens. This eliminates the shared secret as a credential to manage, deploy, and rotate.

### Current state

Firebase Functions authenticate to the Go backend with `Authorization: Bearer <SHARED_SECRET>`. The secret is a static string, required at startup (no default), identical on both sides.

### Target state

Firebase Functions mint a Google Cloud OIDC identity token using the Functions' default service account. The Go backend verifies the token's cryptographic signature, audience, and caller identity — no shared secret involved.

### Implementation plan

**Go server (production, `//go:build !dev`):**
- Use `google.golang.org/api/idtoken` to verify OIDC tokens
- Validate audience matches the backend's URL
- Validate `email` claim is in the configured allowlist (`ALLOWED_SERVICE_ACCOUNTS` env var)

**Go server (dev, `//go:build dev`):**
- Fall back to shared secret (`SHARED_SECRET` env var) for pure localhost mode where the Firebase Functions emulator can't mint OIDC tokens

**Firebase Functions:**
- Use `google-auth-library`'s `GoogleAuth.getIdTokenClient(targetAudience)` to mint OIDC tokens
- Remove `PIVOX_SHARED_SECRET` config parameter

**Config changes:**

| Env var | Mode | Purpose |
|---------|------|---------|
| `ALLOWED_SERVICE_ACCOUNTS` | Production | Comma-separated list of allowed service account emails |
| `AUDIENCE` | Production | Expected audience in OIDC tokens (backend URL) |
| `SHARED_SECRET` | Dev only | Shared secret for localhost mode |

The service account email (e.g., `pivox-cloud@appspot.gserviceaccount.com`) is a public identifier, not a secret. It is safe to store in env vars, config files, and documentation.

### Why not Firebase Custom Tokens

Firebase Custom Tokens (`createCustomToken()`) are designed for client-side sign-in via `signInWithCustomToken()`. They **cannot be verified server-side** — there is no `verifyCustomToken()` method in the Firebase Admin SDK. `VerifyIDToken()` only verifies ID tokens issued by `securetoken.google.com/<project-id>`, not custom tokens signed by service accounts. Using custom tokens for service-to-service auth would require a convoluted chain (mint custom token → exchange for ID token via Firebase REST API → send ID token) that adds latency, requires the web API key in Cloud Functions, and creates phantom users.

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
