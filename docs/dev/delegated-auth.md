# Delegated Authentication

> **Status (2026): legacy / reference.** This pattern is built on
> Firebase custom tokens and the Firebase SDK, tied to the Native App
> and its plugins — a legacy/reference target, not an active migration.
> Pivox auth is now **Keycloak-only**: the cloud verifies Keycloak OIDC
> access tokens via `internal/oidc`, and there is no Firebase custom-token
> minting path anymore. Treat the Firebase mechanics below as the
> abandoned design; a Keycloak-native delegated-auth flow would use OIDC
> tokens (e.g. a device/loopback PKCE grant), not Firebase custom tokens.
> See `AGENTS.md` for the current auth model.

## Overview

Delegated auth is a cross-process authentication pattern for Pivox plugins that run inside third-party host applications. Instead of authenticating directly, the plugin delegates authentication to the Pivox app and receives a Firebase custom token via the backend.

This is the standard auth mechanism for:
- **ActiveX controls** (NRCS plugins in Avid iNEWS, ENPS, etc.)
- **Adobe UXP extensions** (Premiere Pro, After Effects panels)
- **Any future plugin** that runs inside a host process it doesn't control

## Why Not Authenticate Directly?

Plugins run inside third-party host processes. Direct authentication is either impossible or unsafe:

| Concern | Direct Auth | Delegated Auth |
|---|---|---|
| **OAuth popups** | WebView2 won't initialize in XAML Islands. UXP has no native browser control. | App owns the browser — WebView2 popup works. |
| **Passwords in host memory** | Typed into a control inside iNEWS/Adobe — password lives in the host's process memory. Vulnerable to host exploits. | Password never enters the host process. User types it in the Pivox app. |
| **Protocol handlers** | Plugin is a DLL — can't register `pivox://` scheme. Host owns the process lifecycle. | App registers and handles the protocol. |
| **Firebase SDK isolation** | Shared persistence file with other Firebase instances in the same/other processes. State conflicts. | Plugin uses `signInWithCustomToken` — no OAuth flow, no persistence conflicts. |
| **New auth providers** | Must implement each provider in every plugin runtime (C++, JS, Swift). | Add a provider once in the app. Every plugin gets it for free. |

## Architecture

```
Plugin (ActiveX/UXP)              Backend                    Pivox App
       │                             │                           │
       │  POST createDelegated       │                           │
   1.  │  AuthSession {}             │                           │
       │────────────────────────────>│                           │
       │  { code, pollInterval }     │                           │
       │<────────────────────────────│                           │
       │                             │                           │
   2.  │  ShellExecute               │                           │
       │  pivox://auth/delegate/     │                           │
       │  signin?session=<code>      │                           │
       │─────────────────────────────│──────────────────────────>│
       │                             │                           │
       │                             │          User authenticates
       │                             │          (email, Google,  │
       │                             │           GitHub, SSO)    │
       │                             │                           │
       │                             │  POST completeDelegated   │
   3.  │                             │  AuthSession { code }     │
       │                             │  + Bearer <idToken>       │
       │                             │<──────────────────────────│
       │                             │  verify → mint custom     │
       │                             │  token → store            │
       │                             │  204 OK                   │
       │                             │──────────────────────────>│
       │                             │                           │
       │  POST pollDelegated         │                    App exits
   4.  │  AuthSession { code }       │                           │
       │────────────────────────────>│                           │
       │  { customToken }            │                           │
       │<────────────────────────────│  (session consumed,       │
       │                             │   deleted from DB)        │
       │                             │                           │
   5.  │  signInWithCustomToken()    │                           │
       │  AuthStateListener fires    │                           │
       │  → navigate to main UI     │                           │
```

## Backend Endpoints

Three endpoints on the Go backend (`internal/server/internal_hooks.go`):

### `POST /internal/v1/auth:createDelegatedAuthSession`

Creates a pending session. Returns a code the plugin uses to launch the app and poll for completion.

- **Auth:** None (plugin has no Firebase session yet)
- **Rate limit:** Aggressive (1 req/10s per IP, burst 3)
- **Body limit:** 1 KB
- **Request:** `{}`
- **Response:** `{ "code": "<uuid>", "pollInterval": 5 }`
- **TTL:** Configurable via `PIVOX_DELEGATED_AUTH_SESSION_TTL` (default 5 minutes)

### `POST /internal/v1/auth:completeDelegatedAuthSession`

Called by the Pivox app after the user authenticates. Verifies the Firebase ID token, mints a custom token, and stores it against the session code.

- **Auth:** Firebase ID token in `Authorization: Bearer <token>` — verified by Admin SDK
- **Rate limit:** Standard exchange tier
- **Body limit:** 4 KB
- **Request:** `{ "code": "<uuid>" }`
- **Response:** 204 No Content
- **Security:** Constant-time code comparison, custom token minted with UID only (no extra claims)

### `POST /internal/v1/auth:pollDelegatedAuthSession`

Polled by the plugin until the session is completed. Returns the custom token for `signInWithCustomToken`.

- **Auth:** None
- **Rate limit:** Poll tier (must sustain the poll interval)
- **Body limit:** 1 KB
- **Request:** `{ "code": "<uuid>" }`
- **Response:**
  - Pending: `{ "status": "pending" }`
  - Ready: `{ "customToken": "..." }` — session atomically consumed (single-use)
  - Expired/missing: 404

### Configuration

| Config | Env Var | Default | Description |
|---|---|---|---|
| `delegated_auth_session_ttl` | `PIVOX_DELEGATED_AUTH_SESSION_TTL` | 5 min | How long a session code is valid |
| `delegated_auth_poll_interval` | `PIVOX_DELEGATED_AUTH_POLL_INTERVAL` | 5 sec | Returned to clients, advisory |

Rate limiting is the responsibility of the edge proxy / load balancer in
front of pivox-cloud. Abuse defenses on this flow live in the single-use
session code, the 5-minute TTL, and the atomic
`DELETE ... RETURNING custom_token` consume — not in app-level per-IP
limits.

### Database

Table `delegated_auth_sessions`:

| Column | Type | Description |
|---|---|---|
| `code` | UUID (PK) | Session identifier, generated server-side |
| `status` | text | `pending` or `ready` |
| `custom_token` | text (nullable) | Firebase custom token, set on completion |
| `created_at` | timestamptz | Creation time |
| `expires_at` | timestamptz | Expiry time (TTL from config) |

Consumption is atomic: `DELETE ... WHERE code = $1 AND status = 'ready' RETURNING custom_token`. Expired sessions are cleaned up by a background ticker.

## Deep Links

The Pivox app handles these deep links via `pivox://` protocol:

| Deep Link | Behavior |
|---|---|
| `pivox://auth/delegate/signin?session=<code>` | Show auth UI, user signs in, complete session on backend, exit |
| `pivox://auth/delegate/profile` | Open app to user profile |
| `pivox://auth/delegate/signout` | Sign out and exit (no window) |

## Firebase Isolation

Each context uses a separate Firebase app name to get its own persistence file:

| Context | Firebase App Name | Persistence |
|---|---|---|
| Normal app launch | `__FIRAPP_DEFAULT` | Shared app state |
| ActiveX plugin | `pivox-activex` | Independent from app |
| Delegated auth instance | `pivox-delegate-<session-code>` | Ephemeral, cleaned up on exit |

This prevents:
- Cross-process state conflicts (two Firebase instances writing the same file)
- A delegated auth session affecting the main app's signed-in user
- Stale persistence from dead delegate instances (heartbeat files are deleted on exit)

## Single-Instance Behavior

The Pivox app enforces single-instance via a mutex (`Local\PivoxMutex`). Delegated auth deep links (`pivox://auth/delegate/*`) are exempt — they skip the mutex and run as an independent instance with an isolated Firebase context.

This means:
- The main app can be running while a delegated auth instance handles a plugin sign-in
- Multiple delegated instances can run concurrently (each has a unique Firebase app name)
- Normal launches still enforce single-instance

## Security Properties

Designed to pass a security audit:

| Concern | Mitigation |
|---|---|
| Session code guessability | UUID v4 from `crypto/rand` (128 bits of entropy) |
| Replay | Single-use — atomically consumed on poll (`DELETE ... RETURNING`) |
| Stale sessions | TTL with server-side expiry + background cleanup |
| Token forgery | Firebase Admin SDK verifies ID token signature (not just decoded) |
| Privilege escalation | Custom token minted with UID only, no custom claims |
| Brute force | Per-IP rate limiting on all endpoints |
| Transport | HTTPS (TLS in production) |
| Local tampering | No local state — everything is server-side |
| Password exposure | Password is never typed in the plugin — only in the Pivox app |
| Logging | Session codes and tokens are redacted in audit logs |

## Client Implementation

### ActiveX (C++ / WinUI 3 XAML Islands)

**Page:** `DelegatedLoginPage.xaml` — minimal UI with "Sign In with Pivox" button, spinner, error text.

**Flow:**
1. User clicks "Sign In with Pivox"
2. `DelegatedAuthClient::CreateSession()` → backend returns code + poll interval
3. `ShellExecuteW("pivox://auth/delegate/signin?session=<code>")`
4. Timer polls `DelegatedAuthClient::PollSession()` at the returned interval
5. On `PollResult::Ready` → `signInWithCustomTokenAsync(customToken)`
6. `AuthStateListener` fires → `PivoxControl` swaps content to `MainPage`

**Guard against overlapping polls:** A `shared_ptr<bool> polling` flag prevents firing a new poll while the previous HTTP request is still in flight.

**Sign out:** `ShellExecuteW("pivox://auth/delegate/signout")` to sign out via the app, or call `signOut()` on the local Firebase instance.

### Adobe UXP (JavaScript)

Same backend endpoints. The UXP extension would:
1. `fetch(backendUrl + "/internal/v1/auth:createDelegatedAuthSession", { method: "POST" })`
2. Open the deep link via UXP shell API
3. Poll with `setInterval`
4. `signInWithCustomToken(auth, customToken)` using the Firebase JS SDK

The extension never handles passwords or OAuth flows — just the custom token.

### Future Plugins

Any client that can make HTTP requests and call `signInWithCustomToken` works. The backend is client-agnostic. Add a new auth provider to the Pivox app once, and every plugin gets it for free.

## Related Patterns

The Electron app (deprecated) used a similar pattern: external browser + deep link + custom token. The delegated auth flow differs in that coordination is fully backend-mediated (poll instead of direct deep link callback), because plugins are DLLs inside a host — they can't receive deep links. See `docs/authn.md` "Electron Auth" section for historical reference.

## Files

| File | Description |
|---|---|
| `internal/server/internal_hooks.go` | Backend endpoint handlers |
| `internal/db/queries/delegated_auth_sessions.sql` | Database queries |
| `internal/config/config.go` | `DelegatedAuthConfig` (TTL, poll interval) |

> The plugin-side client (Windows ActiveX/UXP) lived in the deleted
> Native App codebase; only the backend half above survives.
