'use client';

import { useCallback, useEffect, useState } from 'react';

import type { ApiClient } from '@pivox/client';

import { useAuth } from '@/auth/use-auth';

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
 * for the user's active organizations once Firebase has a user; passes
 * through unauthenticated renders so an outer auth gate can drive its
 * own redirect.
 *
 *   - status: 'loading' — still waiting on auth or the list call.
 *   - status: 'ready'   — user has ≥1 org (or unauthenticated pass-through).
 *   - status: 'empty'   — user has zero orgs; caller routes to create-org.
 *   - status: 'error'   — the list call failed; surface retry.
 *
 * Uses `Iam.ListAccountOrganizations` (`GET /v1/accounts/me/organizations`)
 * — the slim, caller-scoped, ACTIVE-only view purpose-built for the
 * bootstrap. `Organizations.ListOrganizations` is intentionally NOT
 * used here: it includes soft-deleted orgs (for the undelete UX), so
 * a user whose only membership is on a soft-deleted org would skip
 * the create-org gate and land in the app shell with no usable org.
 *
 * The status is derived from (authLoading, user, fetchOutcome), and
 * fetchOutcome is only written from the fetch's async `.then()` /
 * `.catch()` — never synchronously in the effect body. That keeps the
 * react-hooks/set-state-in-effect rule satisfied without losing the
 * effect-driven state machine.
 *
 * Mirrors the SwiftUI native `OrgService.bootstrap()` flow at
 * `native/platform/macos/swift/Auth/OrgDirectory.swift`.
 */
export function useOrgGate(input: { apiClient: ApiClient }): OrgGateState & {
  actions: OrgGateActions;
} {
  const { apiClient } = input;
  const { user, loading: authLoading } = useAuth();
  // `null` means "no fetch outcome yet" — status falls back to the
  // derived loading/pass-through value. Reset to null on retry so the
  // UI shows the loading splash again while the new fetch is in
  // flight.
  const [fetchOutcome, setFetchOutcome] = useState<
    'ready' | 'empty' | 'error' | null
  >(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    // Wait for auth to settle; the derived status reads 'loading'
    // while we do.
    if (authLoading) return;
    // No user — the surrounding auth gate (LoginFeature redirect,
    // etc.) drives the redirect to sign-in. The derived status reads
    // 'ready' so we don't block render.
    if (!user) return;

    let cancelled = false;
    void apiClient
      // The path template is a literal (`accounts/me` is baked into
      // the URL by the proto's `{parent=accounts/me}` binding), but
      // openapi-typescript still types `parent` as a required path
      // param so we pass the literal value here. openapi-fetch
      // doesn't substitute it (there's no placeholder) — typing
      // ceremony only.
      .GET('/v1/accounts/me/organizations', {
        params: { path: { parent: 'accounts/me' } },
      })
      .then((resp) => {
        if (cancelled) return;
        // openapi-fetch returns a discriminated union; branch on
        // `resp.error` (not destructured) so TS narrowing across
        // both branches stays intact.
        if (resp.error) {
          setError("Couldn't load your organizations. Please try again.");
          setFetchOutcome('error');
          return;
        }
        const count = resp.data.accountOrganizations?.length ?? 0;
        setError(null);
        setFetchOutcome(count === 0 ? 'empty' : 'ready');
      })
      .catch(() => {
        if (cancelled) return;
        setError("Couldn't load your organizations. Please try again.");
        setFetchOutcome('error');
      });

    return () => {
      cancelled = true;
    };
  }, [apiClient, user, authLoading, attempt]);

  // Derived status. The fetch outcome wins once present; otherwise we
  // either wait on auth ('loading') or pass through ('ready') if
  // there's no user for the auth gate to redirect.
  const status: OrgGateStatus =
    fetchOutcome ?? (authLoading ? 'loading' : !user ? 'ready' : 'loading');

  const retry = useCallback(() => {
    setFetchOutcome(null);
    setError(null);
    setAttempt((n) => n + 1);
  }, []);

  return { status, error, actions: { retry } };
}
