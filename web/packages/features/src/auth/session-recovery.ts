/**
 * Recovers a desynced auth state — the server session (cookie) is
 * valid but the Firebase client SDK holds no user (e.g. the auth
 * IndexedDB was evicted, or persistence fell back to in-memory and was
 * lost on reload).
 *
 * The cookie gate (`_app` `beforeLoad`) lets such a session render the
 * app, but every CSR API call goes out with no `Authorization` header
 * and 401s. This bridges server→client the same way the delegated-auth
 * flow already does in production (`internal_hooks.go` mints a Firebase
 * custom token a client redeems via `signInWithCustomToken`): verify
 * the cookie server-side, mint a custom token for its uid, and sign the
 * client back in — silently, with no user-visible re-login.
 *
 * Pure + dependency-injected so the decision logic is unit-testable
 * without Firebase or a DOM. The host (`apps/start`) supplies the real
 * Firebase, router, and server-fn implementations.
 */
export type SessionRecoveryDeps = {
  /** Current Firebase client uid, or null when the SDK has no user. */
  getCurrentUserId: () => string | null;
  /**
   * Verify the server session cookie and mint a Firebase custom token
   * for its uid. Resolves to the token, or `null` when there is no
   * valid server session (genuinely signed out — nothing to recover).
   */
  mintRecoveryToken: () => Promise<string | null>;
  /** Establish a Firebase client session from a custom token. */
  signInWithToken: (token: string) => Promise<void>;
  /** Fallback when no silent recovery is possible. */
  redirectToLogin: () => void;
};

export type SessionRecoveryOutcome =
  | 'already-authenticated'
  | 'recovered'
  | 'redirected-to-login'
  | 'recovery-failed';

/**
 * Attempt to silently re-establish the Firebase client session from a
 * still-valid server session cookie. See the module doc for the why.
 *
 * Callers MUST invoke this only after Firebase's first auth-state event
 * has settled — otherwise `getCurrentUserId()` races the SDK's async
 * restore and would trigger a spurious recovery during normal load.
 */
export async function recoverClientSession(
  deps: SessionRecoveryDeps,
): Promise<SessionRecoveryOutcome> {
  if (deps.getCurrentUserId() !== null) {
    return 'already-authenticated';
  }

  let token: string | null;
  try {
    token = await deps.mintRecoveryToken();
  } catch {
    // Server unreachable / mint failed — can't recover silently.
    deps.redirectToLogin();
    return 'recovery-failed';
  }

  if (token === null) {
    // No valid server session either — a real sign-in is required.
    deps.redirectToLogin();
    return 'redirected-to-login';
  }

  try {
    await deps.signInWithToken(token);
    return 'recovered';
  } catch {
    deps.redirectToLogin();
    return 'recovery-failed';
  }
}
