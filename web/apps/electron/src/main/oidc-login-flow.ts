import {
  createServer,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from 'node:http';

import {
  buildAuthorizationRequest,
  exchangeAuthorizationCode,
  type ConfigProvider,
  type SessionTokens,
} from '@pivox/oidc';
import { app, shell } from 'electron';

import { SCHEME_LANDING_URL } from './oidc-config';

type Configuration = Awaited<ReturnType<ConfigProvider>>;

// How long a login may stay open before it's abandoned. The system browser gives
// no "user closed the tab" signal, so a timeout is the only backstop.
const FLOW_TIMEOUT_MS = 5 * 60 * 1000;

/**
 * How the authorization redirect is caught:
 *   - loopback (default): an ephemeral 127.0.0.1 HTTP server catches Keycloak's
 *     `?code&state` GET directly (OIDC returns them in the query — no hash bounce).
 *     Works in electron-vite dev + packaged.
 *   - scheme: the OIDC redirect_uri is the branded HTTPS landing page
 *     (SCHEME_LANDING_URL); that page forwards the callback params into the app
 *     via the pivox://oidc-callback custom scheme (caught by the deep-link
 *     handler below). Reliable only in packaged builds (electron-vite dev on
 *     macOS can't register the scheme). Gives a real final screen — not a
 *     stranded OAuth tab — and keeps the exchange on an https redirect_uri.
 * The loopback + landing redirect URIs are registered on the Keycloak client.
 */
type Transport = 'loopback' | 'scheme';
function selectedTransport(): Transport {
  // An explicit env override wins in either direction (testing / opt-out).
  const override = process.env.PIVOX_AUTH_REDIRECT;
  if (override === 'scheme' || override === 'loopback') return override;
  // Default: the pivox:// custom scheme in PACKAGED builds (a clean deep link,
  // registered via the app's Info.plist protocol handler), loopback in DEV —
  // electron-vite dev can't register the scheme on macOS, so it falls back to
  // the 127.0.0.1 server there.
  return app.isPackaged ? 'scheme' : 'loopback';
}

const LOOPBACK_PATH = '/oidc/callback';

export type LoginResult =
  | { ok: true; tokens: SessionTokens }
  | { ok: false; error: string };

interface PendingFlow {
  /** The CSRF state — populated once the authorize request is built. */
  state: string;
  codeVerifier: string;
  redirectUri: string;
  config: Configuration;
  /** Idempotent resolver for the whole flow (clears timer + server, resolves). */
  finish: (result: LoginResult) => void;
}

// Exactly one login runs at a time.
//   - `inProgress` is claimed SYNCHRONOUSLY on entry so two rapid auth:login
//     calls can't both pass the guard (the flow object is only assignable after
//     awaits, so guarding on it alone would race).
//   - `current` holds the live flow's resolver + params so the loopback / deep
//     link / cancel paths can settle it. It's nulled the instant a redirect is
//     consumed, so a duplicate callback can't drive a second exchange.
let inProgress = false;
let current: PendingFlow | null = null;

const PAGE = (heading: string, body: string): string => `<!doctype html>
<html lang="en"><head><meta charset="utf-8" /><title>Pivox</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    max-width: 24rem; margin: 16vh auto; padding: 0 1.5rem; text-align: center; }
  h1 { font-size: 1.1rem; } p { opacity: 0.75; }
</style></head>
<body><h1>${heading}</h1><p>${body}</p></body></html>`;

const DONE_PAGE = PAGE('✓ Signed in', 'You can close this tab and return to Pivox.');
const FAIL_PAGE = PAGE('Sign-in failed', 'You can close this tab and return to Pivox.');

/**
 * Completes the code exchange for a captured redirect and settles the flow via
 * its own `finish` (not the module `current`, which is already nulled by the
 * caller — so a duplicate callback can't re-enter here). `respond` writes the
 * browser-facing page for loopback; the scheme transport has no HTTP response.
 */
async function complete(
  flow: PendingFlow,
  currentUrl: URL,
  respond?: (page: string) => void,
): Promise<void> {
  const oauthError = currentUrl.searchParams.get('error');
  if (oauthError) {
    respond?.(FAIL_PAGE);
    flow.finish({ ok: false, error: oauthError });
    return;
  }
  try {
    const tokens = await exchangeAuthorizationCode(flow.config, {
      currentUrl,
      codeVerifier: flow.codeVerifier,
      expectedState: flow.state,
    });
    respond?.(DONE_PAGE);
    flow.finish({ ok: true, tokens });
  } catch {
    respond?.(FAIL_PAGE);
    flow.finish({ ok: false, error: 'exchange_failed' });
  }
}

function handleLoopback(req: IncomingMessage, res: ServerResponse): void {
  const reqUrl = new URL(req.url ?? '/', 'http://127.0.0.1');
  const respond = (page: string): void => {
    // Connection: close so no keep-alive idle socket holds the ephemeral port
    // open after the server closes on settle.
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8', Connection: 'close' });
    res.end(page);
  };

  if (reqUrl.pathname !== LOOPBACK_PATH) {
    res.writeHead(404, { Connection: 'close' });
    res.end();
    return;
  }

  const flow = current;
  // Cross-check the state the IdP round-tripped: any local process could hit
  // this ephemeral port. A mismatch (or no live flow) is ignored without
  // touching the flow, so a local prober can't settle someone's login.
  if (!flow || !flow.state || reqUrl.searchParams.get('state') !== flow.state) {
    res.writeHead(400, { Connection: 'close' });
    res.end();
    return;
  }

  // Consume synchronously before the async exchange so a duplicate GET (browser
  // retry) finds no live flow and can't drive a second exchange of the code.
  current = null;

  // openid-client derives the token-exchange redirect_uri from this URL, so it
  // must equal the redirect_uri sent at authorize time (the loopback URL).
  const callbackUrl = new URL(flow.redirectUri);
  callbackUrl.search = reqUrl.search;
  void complete(flow, callbackUrl, respond);
}

/**
 * Runs the Authorization Code + PKCE login in the system browser and resolves
 * with the token set (or a structured failure). Rejects a second concurrent
 * call — there is only ever one in-flight login.
 */
export async function runLogin(
  configProvider: ConfigProvider,
  scope: string,
  loginHint?: string,
): Promise<LoginResult> {
  // Claim the slot synchronously — before any await — so a concurrent call is
  // rejected rather than starting a second browser tab + loopback server.
  if (inProgress) return { ok: false, error: 'login_in_progress' };
  inProgress = true;

  let config: Configuration;
  try {
    config = await configProvider();
  } catch {
    inProgress = false;
    return { ok: false, error: 'discovery_failed' };
  }

  const transport = selectedTransport();
  const extraParams = loginHint ? { login_hint: loginHint } : undefined;

  return new Promise<LoginResult>((resolve) => {
    let settled = false;
    // Holder (not a `let`) so `finish` — defined before the loopback server
    // exists — can still close it, and so property access stays nullable.
    const loopback: { server?: Server } = {};

    // The single settle path for EVERY outcome — timeout, server error,
    // authorize/exchange failure, cancel, success. Idempotent, and reachable
    // even before `current` is assigned, which closes the "fail before the flow
    // object exists → hang forever" window.
    const finish = (result: LoginResult): void => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      loopback.server?.close();
      current = null;
      inProgress = false;
      resolve(result);
    };

    const timer = setTimeout(() => {
      finish({ ok: false, error: 'auth_timeout' });
    }, FLOW_TIMEOUT_MS);

    // Register the flow synchronously (state filled in once the authorize
    // request is built) so cancelLogin() can settle it during the brief
    // authorize-building window. An empty state matches no redirect.
    current = { state: '', codeVerifier: '', redirectUri: '', config, finish };

    const begin = async (redirectUri: string): Promise<void> => {
      try {
        const { authorizationUrl, codeVerifier, state } = await buildAuthorizationRequest(config, {
          redirectUri,
          scope,
          ...(extraParams ? { extraParams } : {}),
        });
        if (settled) return; // cancelled/timed out while building
        current = { state, codeVerifier, redirectUri, config, finish };
        // Fail fast if the browser can't be launched, rather than waiting out
        // the 5-minute timeout with no signal.
        shell.openExternal(authorizationUrl).catch(() => {
          finish({ ok: false, error: 'browser_open_failed' });
        });
      } catch {
        finish({ ok: false, error: 'authorize_failed' });
      }
    };

    if (transport === 'scheme') {
      // redirect_uri is the HTTPS landing page; it bounces the params into the
      // app via pivox://oidc-callback (see handleAuthCallbackDeepLink).
      void begin(SCHEME_LANDING_URL);
      return;
    }

    const server = createServer((req, res) => {
      handleLoopback(req, res);
    });
    loopback.server = server;
    // Bind 127.0.0.1 explicitly (not 0.0.0.0) — loopback-only traffic never
    // crosses the OS firewall, so this draws no firewall prompt.
    server.on('error', () => {
      finish({ ok: false, error: 'loopback_server_error' });
    });
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      if (address === null || typeof address === 'string') {
        finish({ ok: false, error: 'loopback_server_error' });
        return;
      }
      void begin(`http://127.0.0.1:${address.port}${LOOPBACK_PATH}`);
    });
  });
}

/** Cancels the in-flight login (user dismissed the sign-in UI). No-op if none. */
export function cancelLogin(): void {
  current?.finish({ ok: false, error: 'cancelled' });
}

/**
 * Settles the in-flight login from a `pivox://oidc-callback` deep link (the
 * `scheme` transport). Returns true when the URL was consumed as our callback.
 */
export function handleAuthCallbackDeepLink(url: string): boolean {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return false;
  }
  if (parsed.protocol !== 'pivox:' || parsed.host !== 'oidc-callback') return false;

  const flow = current;
  if (!flow || !flow.state || parsed.searchParams.get('state') !== flow.state) return false;

  // Consume synchronously so a duplicate deep link can't re-exchange.
  current = null;
  const callbackUrl = new URL(flow.redirectUri);
  callbackUrl.search = parsed.search;
  void complete(flow, callbackUrl);
  return true;
}
