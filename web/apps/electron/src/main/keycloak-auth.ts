import { buildEndSessionUrl, decodeIdTokenClaims } from '@pivox/oidc';
import { shell } from 'electron';

import { AuthSession } from './auth-session';
import { oidcConfig, OIDC_SCOPE } from './oidc-config';
import {
  cancelLogin,
  handleAuthCallbackDeepLink as consumeDeepLink,
  runLogin,
} from './oidc-login-flow';
import { createTokenStore } from './token-store';

import type { AuthState, LoginResult } from '../preload/auth-api';

/**
 * Main-process auth orchestrator — the single owner of the signed-in session.
 *
 * Ties the OIDC config + safeStorage token store + {@link AuthSession} engine +
 * the system-browser login flow together, and exposes the surface the IPC layer
 * bridges to the renderer: login / logout / getAccessToken / getState, plus a
 * change subscription so the renderer's auth provider re-reads on every
 * transition (login, logout, boot restore, token refresh).
 */

let session: AuthSession | undefined;
function getSession(): AuthSession {
  session ??= new AuthSession({
    config: oidcConfig,
    persistence: createTokenStore(),
    decodeIdToken: decodeIdTokenClaims,
    scope: OIDC_SCOPE,
  });
  return session;
}

let restoreComplete = false;
const listeners = new Set<() => void>();

function notify(): void {
  for (const listener of listeners) listener();
}

/** Subscribe to auth-state changes. Returns an unsubscribe fn. */
export function onAuthChanged(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Restores a session on boot from the persisted refresh token, then notifies. */
export async function restoreSession(): Promise<void> {
  try {
    await getSession().restore();
  } finally {
    restoreComplete = true;
    notify();
  }
}

export function getAuthState(): AuthState {
  return { ready: restoreComplete, user: getSession().getUser() };
}

/** Runs the browser login flow; on success adopts the tokens and notifies. */
export async function login(loginHint?: string): Promise<LoginResult> {
  const result = await runLogin(oidcConfig, OIDC_SCOPE, loginHint);
  if (result.ok) {
    getSession().setTokens(result.tokens);
    notify();
    return { ok: true };
  }
  return { ok: false, error: result.error };
}

/** Cancels an in-flight login (user dismissed the sign-in UI). */
export function cancelCurrentLogin(): void {
  cancelLogin();
}

/**
 * Signs out: clears local tokens first (so the app is signed out even if the
 * network step fails), then opens Keycloak's RP-initiated end-session URL in the
 * system browser to terminate the IdP SSO session.
 */
export async function logout(): Promise<void> {
  const idTokenHint = getSession().idToken();
  getSession().clear();
  notify();
  try {
    const config = await oidcConfig();
    const url = buildEndSessionUrl(config, idTokenHint ? { idTokenHint } : {});
    await shell.openExternal(url.href);
  } catch {
    // Discovery/end-session unavailable — local sign-out already applied.
  }
}

/** A valid access token for the renderer's API calls. Throws when signed out. */
export async function getAccessToken(): Promise<string> {
  try {
    return await getSession().getAccessToken();
  } catch (err) {
    // A refresh failure clears the session inside the engine; surface that as an
    // auth transition so the renderer's provider re-reads and the gate redirects.
    if (!getSession().isAuthenticated()) notify();
    throw err;
  }
}

/** Consumes a `pivox://oidc-callback` deep link (scheme transport). */
export function handleAuthCallbackDeepLink(url: string): boolean {
  return consumeDeepLink(url);
}
