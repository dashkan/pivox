'use client';

import { useEffect } from 'react';

import { useOrgGate } from './use-org-gate';

import type { ApiClient } from '@pivox/client';

/**
 * Gates the authenticated app shell on the user having ≥1 organization.
 *
 *   - While Firebase auth is settling or the list call is in flight,
 *     renders a tiny "Loading your organizations…" splash.
 *   - When the user has zero orgs, calls `onCreateOrgRequired` once so
 *     the parent route can navigate to the create-org screen; renders
 *     the splash in the meantime so the user isn't staring at empty UI.
 *   - On a list-call failure, renders an inline error with a retry.
 *   - On the unauthenticated path, passes through — the surrounding
 *     auth gate (login redirect) drives that case.
 */
export function OrgGateFeature({
  apiClient,
  onCreateOrgRequired,
  children,
}: {
  apiClient: ApiClient;
  onCreateOrgRequired: () => void;
  children: React.ReactNode;
}) {
  const { status, error, actions } = useOrgGate({ apiClient });

  // Fire the redirect callback from an effect so it doesn't run during
  // render (and so React 18 strict-mode double-mount sees the same
  // empty state and dispatches a single navigate).
  useEffect(() => {
    if (status === 'empty') {
      onCreateOrgRequired();
    }
  }, [status, onCreateOrgRequired]);

  if (status === 'ready') {
    return <>{children}</>;
  }

  if (status === 'error') {
    return <OrgGateError message={error ?? 'Something went wrong.'} onRetry={actions.retry} />;
  }

  // 'loading' and 'empty' both show the splash. 'empty' transitions
  // away as soon as the parent's redirect lands.
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
