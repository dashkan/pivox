import { createFileRoute } from '@tanstack/react-router';
import { useEffect } from 'react';

export const Route = createFileRoute('/auth/github-complete')({
  component: GitHubCompletePage,
});

// Landing page for the web half of the GitHub OAuth flow. The broker
// at `/api/oauth/github/callback` redirects here with the provider
// token in the URL fragment — `#token=…&kind=github_access_token&
// provider=github` on success, or `#error=…&error_description=…` on
// failure. We forward the payload to the opener via postMessage and
// close ourselves.
//
// Fragment (not query) is deliberate — the broker uses `#` so the
// token can't leak through referrer headers or server access logs on
// the way back. JS on this page reads `window.location.hash` to pick
// it up.
function GitHubCompletePage() {
  useEffect(() => {
    const frag = window.location.hash.startsWith('#')
      ? window.location.hash.slice(1)
      : window.location.hash;
    const params = new URLSearchParams(frag);

    const payload = {
      type: 'pivox:github-auth',
      access_token: params.get('token') ?? undefined,
      error: params.get('error') ?? undefined,
      error_description: params.get('error_description') ?? undefined,
    };

    const opener = window.opener as Window | null;
    if (opener && !opener.closed) {
      // targetOrigin restricts delivery to same-origin openers —
      // a malicious cross-origin opener wouldn't receive the token.
      opener.postMessage(payload, window.location.origin);
    }

    const timer = window.setTimeout(() => {
      window.close();
    }, 150);
    return () => {
      window.clearTimeout(timer);
    };
  }, []);

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <p className="text-sm text-muted-foreground">
        Signing you in… you can close this window.
      </p>
    </div>
  );
}
