'use client';

import { Navigate } from '@tanstack/react-router';

import { useOrgGate } from './use-org-gate';

import type { ApiClient } from '@pivox/client';

/**
 * Gates the authenticated app shell on the user having ≥1 organization.
 *
 *   - While Keycloak/OIDC auth is settling or the list call is in flight,
 *     renders a tiny "Loading your organizations…" splash.
 *   - When the user has zero orgs, returns a `<Navigate />` to
 *     `/auth/create-org`. The router primitive is Strict-Mode-safe
 *     (handles the double-render dedup internally) and avoids the
 *     effect-driven imperative-navigate anti-pattern.
 *   - On a list-call failure, renders an inline error with a retry.
 *   - On the unauthenticated path, passes through — the surrounding
 *     auth gate (login redirect) drives that case.
 *
 * The destination (`/auth/create-org`) is hardcoded inside the feature
 * because that's literally what this gate is for. Earlier versions
 * took an `onCreateOrgRequired` callback prop; that indirection had
 * no consumer that wanted a different destination and was the source
 * of an inline-arrow `useEffect` re-fire bug.
 */
export function OrgGateFeature({
  apiClient,
  children,
}: {
  apiClient: ApiClient;
  children: React.ReactNode;
}) {
  const { status, error, actions } = useOrgGate({ apiClient });

  if (status === 'empty') {
    return <Navigate to="/auth/create-org" replace />;
  }

  if (status === 'ready') {
    return <>{children}</>;
  }

  if (status === 'error') {
    return (
      <OrgGateError
        message={error ?? 'Something went wrong.'}
        onRetry={actions.retry}
      />
    );
  }

  return <OrgGateSplash />;
}

function OrgGateSplash() {
  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <div className="flex flex-col items-center gap-3">
        <div
          className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent"
          aria-hidden="true"
        />
        <p className="text-sm text-muted-foreground">
          Loading your organizations…
        </p>
      </div>
    </div>
  );
}

function OrgGateError({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <div className="flex max-w-sm flex-col items-center gap-4 text-center">
        <p className="text-sm text-destructive">{message}</p>
        <button
          type="button"
          onClick={onRetry}
          className="rounded-md border px-3 py-1.5 text-sm hover:bg-muted"
        >
          Try again
        </button>
      </div>
    </div>
  );
}
