'use client';

import { useCallback, useEffect, useState } from 'react';

import { useAuth } from '@/auth/use-auth';

import type { ApiClient } from '@pivox/features/api';

export type OrgGateStatus = 'loading' | 'ready' | 'empty' | 'error';

export interface OrgGateState {
  status: OrgGateStatus;
  /** User-facing error message when `status === 'error'`. */
  error: string | null;
}

export interface OrgGateActions {
  /** Re-run the list-organizations check. Called by the error retry. */
  retry: () => void;
}

/**
 * Bootstraps the user's org membership after sign-in. Asks the cloud
 * for the user's organizations once Firebase has a user; passes
 * through unauthenticated renders so an outer auth gate can drive its
 * own redirect.
 *
 *   - status: 'loading' — still waiting on auth or the list call.
 *   - status: 'ready'   — user has ≥1 org; render the app.
 *   - status: 'empty'   — user has zero orgs; caller routes to create-org.
 *   - status: 'error'   — the list call failed; surface retry.
 *
 * Mirrors the SwiftUI native `OrgService.bootstrap()` flow at
 * `native/platform/macos/swift/Auth/OrgDirectory.swift`.
 */
export function useOrgGate(input: { apiClient: ApiClient }): OrgGateState & {
  actions: OrgGateActions;
} {
  const { apiClient } = input;
  const { user, loading: authLoading } = useAuth();
  const [status, setStatus] = useState<OrgGateStatus>('loading');
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    // Wait for auth to settle. When there's no user, the surrounding
    // auth gate (LoginFeature redirect, etc.) drives the redirect to
    // sign-in — we pass through as 'ready' so we don't block render.
    if (authLoading) {
      setStatus('loading');
      return;
    }
    if (!user) {
      setStatus('ready');
      return;
    }

    let cancelled = false;
    setStatus('loading');
    setError(null);

    void apiClient
      .GET('/v1/organizations', {})
      .then(({ data, error: respError }) => {
        if (cancelled) return;
        if (respError ?? !data) {
          setError("Couldn't load your organizations. Please try again.");
          setStatus('error');
          return;
        }
        const count = data.organizations?.length ?? 0;
        setStatus(count === 0 ? 'empty' : 'ready');
      })
      .catch(() => {
        if (cancelled) return;
        setError("Couldn't load your organizations. Please try again.");
        setStatus('error');
      });

    return () => {
      cancelled = true;
    };
  }, [apiClient, user, authLoading, attempt]);

  const retry = useCallback(() => {
    setAttempt((n) => n + 1);
  }, []);

  return { status, error, actions: { retry } };
}
