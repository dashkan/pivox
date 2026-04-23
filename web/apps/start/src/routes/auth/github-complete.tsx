import { createFileRoute } from '@tanstack/react-router';
import { useEffect } from 'react';

export const Route = createFileRoute('/auth/github-complete')({
  component: GitHubCompletePage,
});

// Landing page for the web half of the GitHub OAuth flow. The Cloud
// Function redirects here with ?access_token=…&state=… after
// exchanging the auth code. We forward the payload to the opener via
// postMessage and close ourselves. If the opener is gone (user
// navigated away), we show a friendly hint.
function GitHubCompletePage() {
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const payload = {
      type: 'pivox:github-auth',
      state: params.get('state') ?? '',
      access_token: params.get('access_token') ?? undefined,
      error: params.get('error') ?? undefined,
      error_description: params.get('error_description') ?? undefined,
    };

    const opener = window.opener as Window | null;
    if (opener && !opener.closed) {
      // targetOrigin restricts delivery to same-origin openers only —
      // a malicious cross-origin opener wouldn't receive the token.
      opener.postMessage(payload, window.location.origin);
    }

    const timer = window.setTimeout(() => {
      window.close();
    }, 150);
    return () => window.clearTimeout(timer);
  }, []);

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <p className="text-sm text-muted-foreground">
        Signing you in… you can close this window.
      </p>
    </div>
  );
}
