import { createFileRoute } from '@tanstack/react-router';
import { useEffect } from 'react';

export const Route = createFileRoute('/auth/broker-callback')({
  component: BrokerCallbackPage,
});

// Landing page for the popup half of the OAuth broker flow. The broker
// finishes by redirecting the popup here with the credential material
// in the URL fragment (`#provider=…&kind=…&token=…`, or `#error=…`).
// This page forwards that fragment to the opener via postMessage; the
// opener's BrowserRedirectTransport parses it and closes the popup.
//
// The fragment (not a query string) is deliberate: the broker uses `#`
// so the token never reaches a server access log or a referrer header.
// JS on this page is the only thing that reads it.
//
// `es` is a per-flow nonce the transport puts on the return URL and the
// broker round-trips; echoing it back lets the opener reject any forged
// or stale message.
function BrokerCallbackPage() {
  useEffect(() => {
    const es = new URLSearchParams(window.location.search).get('es') ?? '';
    const opener = window.opener as Window | null;
    if (opener && !opener.closed) {
      // targetOrigin restricts delivery to a same-origin opener — a
      // cross-origin window cannot receive the credential fragment.
      opener.postMessage(
        {
          type: 'pivox:broker-callback',
          es,
          fragment: window.location.hash,
        },
        window.location.origin,
      );
    }
    // The opener closes this popup once it has the message. If there is
    // no opener (page opened directly), leave it for the user to close.
  }, []);

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <p className="text-sm text-muted-foreground">
        Signing you in… you can close this window.
      </p>
    </div>
  );
}
