import { createFileRoute } from '@tanstack/react-router'

// TODO: Configure Google Cloud Console with a Desktop OAuth client ID
// and set the redirect URI to https://pivox.app/auth/redirect
// TODO: Register 'pivox://' protocol in Electron via app.setAsDefaultProtocolClient('pivox')
// TODO: Implement PKCE flow in Electron main process to initiate OAuth
// TODO: Handle the pivox:// deep link in Electron main process and pass
//       the auth code to the renderer via IPC

type RedirectSearch = {
  code?: string
  state?: string
  error?: string
}

export const Route = createFileRoute('/auth/redirect')({
  validateSearch: (search: Record<string, unknown>): RedirectSearch => ({
    code: (search.code as string) || undefined,
    state: (search.state as string) || undefined,
    error: (search.error as string) || undefined,
  }),
  component: RedirectPage,
})

function RedirectPage() {
  const { code, state, error } = Route.useSearch()

  // When accessed from the web (non-Electron), show a message
  // When the OAuth provider redirects here, forward to the Electron custom protocol
  if (typeof window !== 'undefined') {
    const params = new URLSearchParams()
    if (code) params.set('code', code)
    if (state) params.set('state', state)
    if (error) params.set('error', error)
    const query = params.toString()
    const deepLink = `pivox://auth/callback${query ? `?${query}` : ''}`

    // Attempt redirect to Electron app
    window.location.href = deepLink
  }

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
              const params = new URLSearchParams()
              if (code) params.set('code', code)
              if (state) params.set('state', state)
              if (error) params.set('error', error)
              const query = params.toString()
              window.location.href = `pivox://auth/callback${query ? `?${query}` : ''}`
            }}
          >
            click here to try again
          </button>
          .
        </p>
      </div>
    </div>
  )
}
