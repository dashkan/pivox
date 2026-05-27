import { randomUUID } from 'node:crypto';
import {
  type IncomingMessage,
  type ServerResponse,
  createServer,
} from 'node:http';

import {
  buildBrokerStartUrl,
  parseBrokerRedirect,
  type BrokerRedirectResult,
} from '@pivox/features/broker';
import { shell } from 'electron';

// The Pivox app origin — REST gateway, OAuth broker hooks, and the
// web app itself all live behind it (nginx fans them out). Same env
// var as the renderer reads, so main + renderer agree without IPC.
// Falls back to the dev ngrok tunnel when unset.
const BASE_URL = import.meta.env.VITE_BASE_URL || 'https://pivox.ngrok.app';

// How long a broker flow may stay open before it is abandoned. The
// external system browser gives no "user closed the tab" signal, so a
// timeout is the only backstop.
const FLOW_TIMEOUT_MS = 5 * 60 * 1000;

// Cap the loopback /token body. The posted value is only a callback
// URL — a few KB even with a fat id_token; anything larger is a local
// process abusing the ephemeral port, so the request is dropped.
const MAX_TOKEN_BODY_BYTES = 64 * 1024;

/**
 * How the broker's final redirect is caught:
 *   - loopback (default): an ephemeral 127.0.0.1 HTTP server. Works in
 *     electron-vite dev and packaged builds, on every platform.
 *   - scheme: the pivox:// custom scheme. Reliable only in packaged
 *     builds — electron-vite dev on macOS cannot register the scheme.
 */
type Transport = 'loopback' | 'scheme';

function selectedTransport(): Transport {
  return process.env.PIVOX_AUTH_REDIRECT === 'scheme' ? 'scheme' : 'loopback';
}

interface PendingFlow {
  resolve: (result: BrokerRedirectResult) => void;
  cleanup: () => void;
}

// Live broker flows keyed by their `es` CSRF token. A flow is removed
// the instant its redirect is caught (single-use) or it times out, so
// a loopback hit / deep link matching no live flow is simply ignored.
const pendingFlows = new Map<string, PendingFlow>();

function settleFlow(es: string, result: BrokerRedirectResult): void {
  const flow = pendingFlows.get(es);
  if (!flow) return;
  pendingFlows.delete(es);
  flow.cleanup();
  flow.resolve(result);
}

// Tiny page served at the loopback /cb route. The credential lives in
// location.hash, which never reaches a server — so this script re-posts
// the full URL to /token (same-origin: no CORS, no Private Network
// Access preflight).
const BOUNCE_PAGE = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<title>Pivox</title>
<style>
  :root { color-scheme: light dark; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    max-width: 24rem; margin: 16vh auto; padding: 0 1.5rem; text-align: center;
  }
</style>
</head>
<body>
<p id="status">Completing sign-in…</p>
<script>
  fetch('/token', { method: 'POST', body: location.href })
    .then(function () {
      document.getElementById('status').textContent =
        '✓ Signed in — you can close this tab and return to Pivox.';
    })
    .catch(function () {
      document.getElementById('status').textContent =
        'Sign-in could not be completed — you can close this tab.';
    });
</script>
</body>
</html>
`;

function handleLoopbackRequest(
  es: string,
  req: IncomingMessage,
  res: ServerResponse,
): void {
  const path = (req.url ?? '').split('?')[0] ?? '';

  if (req.method === 'GET' && path === '/cb') {
    // `Connection: close` on every loopback response keeps each request
    // one-shot — no keep-alive idle connection lingers to hold the
    // ephemeral port open after the flow settles and server.close()
    // runs. (server.close() is graceful: it waits for open connections.)
    res.writeHead(200, {
      'content-type': 'text/html; charset=utf-8',
      Connection: 'close',
    });
    res.end(BOUNCE_PAGE);
    return;
  }

  if (req.method === 'POST' && path === '/token') {
    const chunks: Buffer[] = [];
    let received = 0;
    let aborted = false;
    req.on('data', (chunk: Buffer) => {
      if (aborted) return;
      received += chunk.length;
      if (received > MAX_TOKEN_BODY_BYTES) {
        aborted = true;
        res.writeHead(413, { Connection: 'close' });
        res.end();
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => {
      if (aborted) return;
      res.writeHead(200, {
        'content-type': 'text/plain',
        Connection: 'close',
      });
      res.end('ok');
      const body = Buffer.concat(chunks).toString('utf8');
      // Cross-check the `es` the broker round-tripped before settling:
      // any local process could POST to this ephemeral port.
      let callbackEs: string | null = null;
      try {
        callbackEs = new URL(body).searchParams.get('es');
      } catch {
        return;
      }
      if (callbackEs === es) {
        settleFlow(es, parseBrokerRedirect(body));
      }
    });
    return;
  }

  res.writeHead(404, { Connection: 'close' });
  res.end();
}

/**
 * Runs the broker OAuth flow for `provider` in the system browser and
 * resolves with the parsed result. The renderer's RedirectTransport
 * calls this over IPC; sign-in vs. account-link is the renderer's
 * decision once it holds the credential.
 *
 * `flowId` is the renderer-supplied identifier for this flow. It
 * serves two purposes: (1) it's the CSRF token round-tripped through
 * the broker (echoed back in the callback URL's `es` param and
 * cross-checked here), and (2) it's the key the renderer uses to
 * target an `abortBrokerLogin(flowId)` IPC at this specific flow.
 * Renderer generates it so the renderer can call abort BEFORE
 * receiving the start result.
 *
 * `flowId` is optional for backward compat — if absent we fall
 * back to generating one internally, which keeps the loopback /
 * scheme transports working as before but loses scoped-abort
 * support. New callers should always pass a flowId.
 */
export function startBrokerLogin(input: {
  provider: string;
  loginHint?: string;
  flowId?: string;
}): Promise<BrokerRedirectResult> {
  const es = input.flowId ?? randomUUID();

  // Reject duplicate flowIds. Without this guard, a second
  // `pendingFlows.set(es, ...)` below would overwrite the first
  // entry — its resolve/cleanup would be unreachable from the map,
  // leaving its loopback server bound, its timer armed, and its IPC
  // promise unresolved forever. Renderer-controlled flowId opens
  // this window (impossible when main generated the UUID); the
  // guard closes it. Returning a structured failure lets the
  // renderer surface a real error instead of hanging.
  if (pendingFlows.has(es)) {
    return Promise.resolve({ ok: false, error: 'duplicate_flow_id' });
  }

  return new Promise<BrokerRedirectResult>((resolve) => {
    const timer = setTimeout(() => {
      settleFlow(es, { ok: false, error: 'auth_timeout' });
    }, FLOW_TIMEOUT_MS);

    const startUrl = (returnUrl: string): string =>
      buildBrokerStartUrl({
        baseUrl: BASE_URL,
        provider: input.provider,
        returnUrl,
        ...(input.loginHint ? { loginHint: input.loginHint } : {}),
      });

    if (selectedTransport() === 'scheme') {
      pendingFlows.set(es, {
        resolve,
        cleanup: () => {
          clearTimeout(timer);
        },
      });
      void shell.openExternal(startUrl(`pivox://auth-complete?es=${es}`));
      return;
    }

    const server = createServer((req, res) => {
      handleLoopbackRequest(es, req, res);
    });
    // A loopback bind failure would otherwise hang until the 5-minute
    // timeout — settle the flow immediately instead.
    server.on('error', () => {
      settleFlow(es, { ok: false, error: 'loopback_server_error' });
    });
    pendingFlows.set(es, {
      resolve,
      cleanup: () => {
        clearTimeout(timer);
        server.close();
      },
    });
    // Bind 127.0.0.1 explicitly (not 0.0.0.0) — loopback-only traffic
    // never crosses the OS firewall, so this draws no firewall prompt.
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      if (address === null || typeof address === 'string') {
        return;
      }
      void shell.openExternal(
        startUrl(`http://127.0.0.1:${address.port}/cb?es=${es}`),
      );
    });
  });
}

/**
 * Settles the broker login identified by `flowId` as user-cancelled.
 * Called from the renderer when the user clicks "Cancel sign-in"
 * while a social / SSO flow is open. Runs the flow's cleanup (closes
 * the loopback server, clears the timeout) before resolving — same
 * shape as the user dismissing the OS browser window directly.
 *
 * No-op when the flow isn't in the pending map (already settled, or
 * the renderer's abort listener fired stale). Returns true if a flow
 * was actually settled, false otherwise — tests use this; production
 * callers ignore the return.
 */
export function abortBrokerLogin(flowId: string): boolean {
  if (!pendingFlows.has(flowId)) return false;
  settleFlow(flowId, { ok: false, error: 'popup_closed' });
  return true;
}

/**
 * Settles a broker flow from a `pivox://auth-complete` deep link (the
 * `scheme` transport). Returns true if the URL was a broker callback
 * for a live flow, so the caller knows the deep link is consumed.
 */
export function handleAuthCompleteDeepLink(url: string): boolean {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return false;
  }
  if (parsed.protocol !== 'pivox:' || parsed.host !== 'auth-complete') {
    return false;
  }
  const es = parsed.searchParams.get('es');
  if (!es || !pendingFlows.has(es)) {
    return false;
  }
  settleFlow(es, parseBrokerRedirect(url));
  return true;
}
