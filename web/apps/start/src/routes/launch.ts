import { createFileRoute } from '@tanstack/react-router'

/**
 * Desktop OAuth landing page.
 *
 * The Electron app's `scheme` transport uses this HTTPS URL as its OIDC
 * redirect_uri, so Keycloak returns the browser to a real, branded page instead
 * of stranding it on the last OAuth screen when redirecting straight to a
 * pivox:// custom scheme. This page then hands the callback params off to the
 * desktop app via `pivox://oidc-callback` + the same query string.
 *
 * It's a dumb forwarder: the app validates `state` (CSRF) and completes the
 * exchange with PKCE, so a forged hit here (someone opening /launch with a bogus
 * code) is inert — it can't match any in-flight sign-in. Served as static HTML
 * with no auth and no app bundle.
 */
const LAUNCH_PAGE = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>Signing in to Pivox…</title>
<style>
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    min-height: 100vh; margin: 0; display: grid; place-items: center;
    background: #fafafa; color: #18181b;
  }
  @media (prefers-color-scheme: dark) { body { background: #09090b; color: #fafafa; } }
  main {
    max-width: 22rem; padding: 2.5rem 1.5rem; text-align: center;
  }
  .mark {
    width: 3rem; height: 3rem; margin: 0 auto 1.25rem; border-radius: 0.85rem;
    display: grid; place-items: center; font-weight: 700; font-size: 1.35rem;
    background: #6366f1; color: #fff;
  }
  h1 { font-size: 1.15rem; margin: 0 0 0.4rem; }
  p { margin: 0; opacity: 0.7; font-size: 0.9rem; line-height: 1.5; }
  a.btn {
    display: inline-block; margin-top: 1.25rem; padding: 0.55rem 1.1rem;
    border-radius: 0.6rem; background: #6366f1; color: #fff; text-decoration: none;
    font-size: 0.9rem; font-weight: 500;
  }
  [hidden] { display: none; }
</style>
</head>
<body>
<main>
  <div class="mark">P</div>
  <h1 id="title">Signed in to Pivox</h1>
  <p id="msg">Returning you to the app…</p>
  <a id="open" class="btn" href="#" hidden>Open Pivox</a>
</main>
<script>
  (function () {
    var search = location.search;
    var params = new URLSearchParams(search);
    // Only hand off a real callback (code or an OAuth error); an incidental
    // visit just shows a neutral message and never triggers the scheme.
    if (!params.has('code') && !params.has('error')) {
      document.getElementById('title').textContent = 'Pivox';
      document.getElementById('msg').textContent = 'You can close this tab.';
      return;
    }
    var target = 'pivox://oidc-callback' + search;
    var open = document.getElementById('open');
    open.href = target;
    open.hidden = false;
    // Auto hand-off; the button is the fallback if the browser blocks the
    // programmatic scheme navigation (some require a user gesture).
    location.replace(target);
  })();
</script>
</body>
</html>`

export const Route = createFileRoute('/launch')({
  server: {
    handlers: {
      GET: () =>
        new Response(LAUNCH_PAGE, {
          headers: { 'content-type': 'text/html; charset=utf-8' },
        }),
    },
  },
})
