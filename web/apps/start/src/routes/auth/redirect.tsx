import { createFileRoute } from '@tanstack/react-router';
import { useEffect } from 'react';

// TODO: Configure Google Cloud Console with a Desktop OAuth client ID
// and set the redirect URI to https://pivox.app/auth/redirect
// TODO: Register 'pivox://' protocol in Electron via app.setAsDefaultProtocolClient('pivox')
// TODO: Implement PKCE flow in Electron main process to initiate OAuth
// TODO: Handle the pivox:// deep link in Electron main process and pass
//       the auth code to the renderer via IPC

type RedirectSearch = {
  code?: string;
  state?: string;
  error?: string;
};

function buildDeepLink(params: RedirectSearch) {
  const qs = new URLSearchParams();
  if (params.code) qs.set('code', params.code);
  if (params.state) qs.set('state', params.state);
  if (params.error) qs.set('error', params.error);
  const query = qs.toString();
  return `pivox://auth/callback${query ? `?${query}` : ''}`;
}

export const Route = createFileRoute('/auth/redirect')({
  validateSearch: (search: Record<string, unknown>): RedirectSearch => ({
    code: (search.code as string) || undefined,
    state: (search.state as string) || undefined,
    error: (search.error as string) || undefined,
  }),
  component: RedirectPage,
});

function RedirectPage() {
  const { code, state, error } = Route.useSearch();

  // Side-effecting navigation belongs in an effect, not during render.
  // Effects don't run server-side so the SSR guard becomes implicit.
  useEffect(() => {
    window.location.href = buildDeepLink({ code, state, error });
  }, [code, state, error]);

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="text-center text-sm text-muted-foreground">
        <p>Redirecting to Pivox desktop app...</p>
        <p className="mt-2">
          If the app didn&apos;t open,{' '}
          <button
            type="button"
            className="text-primary underline-offset-4 hover:underline"
            onClick={() => {
              window.location.href = buildDeepLink({ code, state, error });
            }}
          >
            click here to try again
          </button>
          .
        </p>
      </div>
    </div>
  );
}
