/**
 * The auth IPC contract between the main process and the renderer — the single
 * source of truth for the shapes that cross the contextBridge.
 *
 * Deliberately dependency-free (no electron, no @pivox/*): the main process
 * implements it, the preload bridge re-exposes it, the preload `.d.ts` types
 * `window.api` from it, and the renderer derives its types off `window.api`.
 * Keeping it import-free means pulling it into the renderer's type graph brings
 * no main-process or Node types along.
 */

/** Platform-neutral identity (mirrors @pivox/features/auth's AuthUser). */
export interface AuthUser {
  id: string;
  email?: string;
  displayName?: string;
  photoURL?: string;
}

export interface AuthState {
  /** False until boot restore finished — the renderer shows a splash. */
  ready: boolean;
  user: AuthUser | null;
}

export interface LoginResult {
  ok: boolean;
  error?: string;
}

/** The surface exposed on `window.api` by the preload bridge. */
export interface PivoxAPI {
  /** Runs the system-browser login; resolves once caught + exchanged. */
  login: (input?: { loginHint?: string }) => Promise<LoginResult>;
  /** Cancels an in-flight login (user dismissed the sign-in UI). */
  cancelLogin: () => Promise<void>;
  /** RP-initiated logout: clears local tokens + ends the Keycloak session. */
  logout: () => Promise<void>;
  /** Current auth state; read on mount and after each onAuthChanged. */
  getAuthState: () => Promise<AuthState>;
  /** A valid access token; rejects when signed out. */
  getAccessToken: () => Promise<string>;
  /** Subscribe to auth-state changes. Returns an unsubscribe fn. */
  onAuthChanged: (callback: () => void) => () => void;
}
