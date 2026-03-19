# Authentication Security Audit

**Date**: 2026-03-18
**Scope**: All authentication flows — Firebase Auth (web + Electron), server-side token verification, internal APIs, account sync, 2FA/TOTP, OAuth, password management
**Methodology**: Manual secure code review against OWASP Top 10, CWE Top 25, and Firebase Auth best practices

---

## Executive Summary

The authentication system is well-architected around Firebase Auth with proper separation of concerns. The Electron OAuth flow via custom protocol deep linking is a sound design choice. The audit identified **2 High**, **4 Medium**, and **4 Low** severity findings — **all 10 have been remediated**.

**Overall posture**: All findings addressed. Production-ready pending integration testing.

---

## Findings

### AUTHN-01 — Shared secret defaults to `dev-secret` in production

| Field | Value |
|-------|-------|
| **Severity** | High |
| **CWE** | CWE-798 (Use of Hard-Coded Credentials) |
| **Location** | `internal/config/config.go:44`, `deployments/firebase/functions/.env.example` |
| **Status** | ✅ Eliminated |

**Description**: The `SHARED_SECRET` environment variable falls back to `"dev-secret"` if unset. This static secret protects the internal `/internal/v1/accounts:sync` endpoint that can create/modify any user account. If a production deployment fails to set this variable, the endpoint is effectively unprotected — any attacker who discovers it can upsert arbitrary accounts.

The Firebase Functions side mirrors this with `defineString("PIVOX_SHARED_SECRET", { default: "dev-secret" })`.

**Impact**: An attacker could call `POST /internal/v1/accounts:sync` with `Authorization: Bearer dev-secret` to create or modify any account in the database, set `disabled: false` on locked accounts, or change email/verification status.

**Remediation**: The static shared secret has been fully replaced with Google Cloud OIDC identity tokens in production. Firebase Functions mint an OIDC token using the default service account, and the Go backend verifies it cryptographically via `google.golang.org/api/idtoken` against Google's JWKS endpoint. The backend validates both the token's audience and the caller's service account email against a configured allowlist (`ALLOWED_SERVICE_ACCOUNTS`).

The shared secret is retained only for the dev build tag (`//go:build dev`) to support pure localhost development with the Firebase emulator, which cannot mint OIDC tokens. The dev code path is excluded from production binaries at compile time.

---

### AUTHN-02 — No authentication on gRPC/REST API services

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Location** | `cmd/server/main.go:98-109` |
| **Status** | ✅ Fixed |

**Description**: The gRPC server is created with only a validation interceptor — no authentication interceptor. All registered services (`Projects`, `Organizations`, `TagKeys`, `TagValues`, `TagBindings`, `ApiKeys`) appear to be accessible without verifying a Firebase ID token or any other credential. The REST gateway inherits this — it connects to gRPC via insecure local transport with no auth layer.

```go
grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(server.FieldMaskAwareValidationInterceptor(validator)),
)
```

**Impact**: Any network-reachable client can call all API endpoints (create projects, manage organizations, manage API keys) without authentication.

**Remediation**: Add a gRPC unary/stream interceptor that extracts the Firebase ID token from the `authorization` metadata, verifies it via `firebase.VerifyIDToken()`, and injects the authenticated user into the context. Use `grpc.ChainUnaryInterceptor()` to compose it with the existing validator:

```go
grpcServer := grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        server.AuthInterceptor(authSvc),
        server.FieldMaskAwareValidationInterceptor(validator),
    ),
)
```

Exempt public endpoints (if any) via a method allowlist.

---

### AUTHN-03 — Electron `sandbox: false` weakens process isolation

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **CWE** | CWE-693 (Protection Mechanism Failure) |
| **Location** | `web/apps/electron/src/main/index.ts:102` |
| **Status** | ✅ Fixed |

**Description**: The BrowserWindow is created with `sandbox: false`:

```ts
webPreferences: {
  preload: join(__dirname, '../preload/index.js'),
  sandbox: false,
},
```

Disabling the sandbox allows the renderer process to access Node.js APIs even through the preload script's context bridge. If an XSS vulnerability is found in the renderer, the attacker gains broader capabilities than in a sandboxed renderer.

**Impact**: Increased blast radius of any XSS or content injection vulnerability in the Electron renderer.

**Remediation**: Enable the sandbox (`sandbox: true`) and ensure the preload script works within the sandboxed constraints. The current preload only uses `contextBridge` and `ipcRenderer` which are compatible with sandboxed mode.

---

### AUTHN-04 — ID token passed in URL query parameter during Electron provider linking

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **CWE** | CWE-598 (Use of GET Request Method With Sensitive Query Strings) |
| **Location** | `web/apps/electron/src/main/index.ts:86` |
| **Status** | ✅ Fixed |

**Description**: The `auth:start-link-provider` IPC handler passes the Firebase ID token as a URL query parameter:

```ts
const url = `${BASE_URL}/auth/electron-link?provider=...&state=...&token=${encodeURIComponent(idToken)}`
```

This ID token appears in:
- Browser address bar and history
- HTTP server access logs
- Any intermediate proxy/CDN logs
- Browser referrer headers on subsequent navigation

**Impact**: The Firebase ID token (valid for ~1 hour) could be leaked through browser history, server logs, or referrer headers, enabling session hijacking.

**Remediation**: Use a short-lived, single-use exchange code instead of passing the raw ID token. The flow should be:

1. Electron calls a backend endpoint to deposit the ID token and receive a single-use `code` (UUID, expires in 60s)
2. The browser URL contains only the `code`
3. The web page exchanges the `code` for the ID token via a server-side call

Alternatively, if the architecture requires it, use `POST` with the token in the request body, or use an encrypted/signed cookie.

---

### AUTHN-05 — No request body size limit on internal endpoints

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **CWE** | CWE-400 (Uncontrolled Resource Consumption) |
| **Location** | `internal/server/internal_hooks.go:54` |
| **Status** | ✅ Fixed |

**Description**: The `syncAccount` handler decodes the request body directly without limiting its size:

```go
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
```

An attacker (or misconfigured client) could send an extremely large request body to exhaust server memory.

**Impact**: Denial of service via memory exhaustion.

**Remediation**: Wrap `r.Body` with `http.MaxBytesReader`:

```go
r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
```

Apply this to all HTTP handlers that read request bodies.

---

### AUTHN-06 — `exchangeToken` endpoint has no rate limiting

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **CWE** | CWE-307 (Improper Restriction of Excessive Authentication Attempts) |
| **Location** | `internal/server/internal_hooks.go:92-118` |
| **Status** | ✅ Fixed |

**Description**: The `POST /internal/v1/auth:exchangeToken` endpoint verifies a Firebase ID token and returns a custom token. It has no rate limiting and is not protected by the shared secret (by design — it uses token-based auth). This makes it a potential target for token-oracle abuse: an attacker with a valid ID token could generate unlimited custom tokens.

While the ID token verification itself provides authentication, the lack of rate limiting means:
- A stolen ID token can be used to generate many custom tokens before it expires
- No monitoring/alerting on unusual exchange patterns

**Impact**: Amplified impact of a compromised ID token; no defense-in-depth against token abuse.

**Remediation**: Add rate limiting per UID (e.g., 10 exchanges per minute per user). Log all exchange requests with the UID for monitoring. Consider adding the endpoint behind an API gateway with built-in rate limiting.

---

### AUTHN-07 — User enumeration via differentiated error messages

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **CWE** | CWE-204 (Observable Response Discrepancy) |
| **Location** | `web/packages/features/src/shared/firebase-error.ts:9-10` |
| **Status** | ✅ Fixed |

**Description**: The error message mapping returns different messages for `auth/user-not-found` ("No account found with this email") vs. `auth/wrong-password` ("Incorrect password"). This allows an attacker to enumerate valid email addresses by observing which error is returned.

```ts
case 'auth/user-not-found':
  return 'No account found with this email';
case 'auth/wrong-password':
  return 'Incorrect password';
```

Note: Firebase's newer SDK versions often return `auth/invalid-credential` for both cases (which is already handled at line 14), but older clients or specific flows may still receive the differentiated errors.

**Impact**: Email enumeration — an attacker can determine which email addresses are registered.

**Remediation**: Return the same generic message for both cases:

```ts
case 'auth/user-not-found':
case 'auth/wrong-password':
case 'auth/invalid-credential':
  return 'Invalid email or password';
```

Also verify that Firebase project settings have **Email Enumeration Protection** enabled (Firebase Console → Authentication → Settings).

---

### AUTHN-08 — `pendingAuthState` is a single global variable (race condition)

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **CWE** | CWE-362 (Race Condition) |
| **Location** | `web/apps/electron/src/main/index.ts:10` |
| **Status** | ✅ Fixed |

**Description**: The Electron main process stores only one `pendingAuthState` at a time:

```ts
let pendingAuthState: string | null = null;
```

If a user initiates a social login flow and then immediately starts another (e.g., clicks Google then GitHub quickly), the first state is overwritten. The first callback would fail CSRF validation (correct behavior), but this creates a confusing UX and could theoretically be exploited in a timing attack.

**Impact**: Low — primarily a UX issue. The CSRF check correctly rejects stale states, so there is no bypass. However, in edge cases, a malicious deep link with an old state could theoretically match if timing aligns.

**Remediation**: Use a `Map<string, { provider: string, timestamp: number }>` to store multiple pending states with expiry. Clean up entries older than 5 minutes.

```ts
const pendingAuthStates = new Map<string, number>();

// On initiate:
pendingAuthStates.set(state, Date.now());

// On callback:
const timestamp = pendingAuthStates.get(state);
if (!timestamp || Date.now() - timestamp > 5 * 60 * 1000) {
  // reject
}
pendingAuthStates.delete(state);
```

---

### AUTHN-09 — Pending OAuth credential stored in module-level variable

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **CWE** | CWE-922 (Insecure Storage of Sensitive Information) |
| **Location** | `web/packages/features/src/shared/pending-link.ts` |
| **Status** | ✅ Fixed |

**Description**: When an OAuth login encounters `auth/account-exists-with-different-credential`, the credential is stored in a module-scoped variable:

```ts
let pendingLink: PendingLink | null = null;
```

This credential persists in memory indefinitely until consumed or the page is refreshed. It's accessible to any code running in the same JavaScript context.

**Impact**: Low — the credential is only useful for linking to an already-authenticated account, and it's constrained to the same browser tab. However, it violates the principle of minimizing credential lifetime.

**Remediation**: Add a TTL. Clear the pending link after 5 minutes or on navigation away from the link-account page:

```ts
let pendingLinkTimeout: ReturnType<typeof setTimeout> | null = null;

export function setPendingLink(link: PendingLink) {
  pendingLink = link;
  if (pendingLinkTimeout) clearTimeout(pendingLinkTimeout);
  pendingLinkTimeout = setTimeout(clearPendingLink, 5 * 60 * 1000);
}
```

---

### AUTHN-10 — DevTools accessible in production Electron builds

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **CWE** | CWE-489 (Active Debug Code) |
| **Location** | `web/apps/electron/src/main/index.ts:109-113` |
| **Status** | ✅ Fixed |

**Description**: The production Electron build allows opening DevTools via `Cmd+Option+I`:

```ts
mainWindow!.webContents.on('before-input-event', (event, input) => {
  if (input.meta && input.alt && input.key === 'i') {
    mainWindow!.webContents.toggleDevTools()
    event.preventDefault()
  }
})
```

**Impact**: A user (or malware with access to the keyboard) can open DevTools to inspect tokens in memory, modify application state, or extract the Firebase refresh token from IndexedDB.

**Remediation**: Gate this behind a development check or remove entirely for production:

```ts
if (is.dev) {
  mainWindow!.webContents.on('before-input-event', (event, input) => {
    if (input.meta && input.alt && input.key === 'i') {
      mainWindow!.webContents.toggleDevTools()
      event.preventDefault()
    }
  })
}
```

---

## Positive Findings

These aspects of the authentication system are well-implemented:

| Area | Assessment |
|------|-----------|
| **Electron OAuth via redirect + deep link** | Sound architecture — avoids `signInWithPopup` in Electron, uses `signInWithRedirect` with custom protocol. CSRF state parameter is generated with `crypto.randomUUID()` and validated on return. |
| **TOTP 2FA implementation** | Follows Firebase best practices — requires email verification before enrollment, requires reauthentication for sensitive operations, provides revert-via-email mechanism. |
| **Reauthentication for sensitive operations** | Consistent pattern across password change, email change, 2FA enrollment/unenrollment, and account deletion. Auto-reauthenticates via OAuth when possible. |
| **Account sync via blocking functions** | `beforeUserCreated` / `beforeUserSignedIn` ensure the backend database is always in sync with Firebase Auth. Sync failure blocks the auth operation — no orphaned accounts. |
| **Error handling** | Generic error messages for most cases. Firebase error codes are mapped to user-friendly messages without exposing internal details. |
| **Context isolation in Electron** | `contextBridge` is used correctly to expose a minimal API surface to the renderer. IPC channels are well-scoped. |
| **Token exchange endpoint** | Cryptographic verification of ID tokens (not shared secret) — correct design for a public-facing token exchange. |
| **Session management** | Relies on Firebase SDK's built-in token lifecycle (`onIdTokenChanged`) rather than custom session handling — less surface area for bugs. |
| **Email change flow** | Uses `verifyBeforeUpdateEmail()` which requires clicking a verification link before the change takes effect. Old email receives a recovery link. |
| **No passwords stored server-side** | All password handling is delegated to Firebase Auth — the backend only stores non-sensitive profile data. |

---

## Risk Matrix

| ID | Finding | Severity | Effort to Fix | Status |
|----|---------|----------|---------------|--------|
| AUTHN-02 | No auth on gRPC/REST API | High | Medium | ✅ Fixed |
| AUTHN-01 | Shared secret defaults to `dev-secret` | High | Low | ✅ Eliminated (OIDC) |
| AUTHN-04 | ID token in URL query parameter | Medium | Medium | ✅ Fixed |
| AUTHN-03 | Electron `sandbox: false` | Medium | Low | ✅ Fixed |
| AUTHN-06 | No rate limiting on token exchange | Medium | Medium | ✅ Fixed |
| AUTHN-05 | No request body size limit | Medium | Low | ✅ Fixed |
| AUTHN-07 | User enumeration via error messages | Low | Low | ✅ Fixed |
| AUTHN-08 | Single pending auth state variable | Low | Low | ✅ Fixed |
| AUTHN-09 | Pending credential no TTL | Low | Low | ✅ Fixed |
| AUTHN-10 | DevTools in production builds | Low | Low | ✅ Fixed |
