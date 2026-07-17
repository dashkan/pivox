// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { SecretFormValues } from '@pivox/ui/resource-admin';

import { useSecretForm } from '@/secrets/use-secret-form';

// A create-mode `$api` stub: the single-record query is disabled in create, so
// it only ever needs to return an inert result.
const api = {
  useQuery: () => ({ data: undefined, isLoading: false, error: undefined }),
} as unknown as ReactQueryApi;

const values: SecretFormValues = {
  secretId: 'stripe-key',
  displayName: 'Stripe key',
  annotations: [],
  value: 's3cr3t',
  rotate: false,
  scope: 'news',
};

describe('useSecretForm mutate error surfacing', () => {
  it('surfaces a THROWN (network/aborted) create failure and clears pending', async () => {
    // openapi-fetch rejects (does not resolve `{ error }`) on a network failure
    // or an aborted request — the exact case the old uncaught async IIFE
    // swallowed, leaving the user with no error and a stuck pending state.
    const apiClient = {
      POST: vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
      PATCH: vi.fn(),
      DELETE: vi.fn(),
    } as unknown as ApiClient;
    const onDone = vi.fn();

    const { result } = renderHook(() =>
      useSecretForm({
        $api: api,
        apiClient,
        parent: 'organizations/acme',
        mode: 'create',
        onDone,
      }),
    );

    act(() => {
      result.current.mutate(values);
    });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    expect(result.current.pending).toBe(false);
    expect(onDone).not.toHaveBeenCalled();
  });

  it('surfaces an RPC error arm from a failed create and clears pending', async () => {
    const apiClient = {
      POST: vi.fn(() =>
        Promise.resolve({ error: { code: 3, message: 'bad secret id' } }),
      ),
      PATCH: vi.fn(),
      DELETE: vi.fn(),
    } as unknown as ApiClient;
    const onDone = vi.fn();

    const { result } = renderHook(() =>
      useSecretForm({
        $api: api,
        apiClient,
        parent: 'organizations/acme',
        mode: 'create',
        onDone,
      }),
    );

    act(() => {
      result.current.mutate(values);
    });

    await waitFor(() => expect(result.current.error).toBe('bad secret id'));
    expect(result.current.pending).toBe(false);
    expect(onDone).not.toHaveBeenCalled();
  });
});
