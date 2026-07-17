import { describe, expect, it, vi } from 'vitest';

import { deleteSecret, saveSecret } from '@/secrets/save-secret';

import type { ApiClient } from '@pivox/client';
import type { Secret, SecretFormValues } from '@pivox/ui/resource-admin';

const ORG_LIST = '/v1/organizations/{organization}/secrets';
const SPACE_LIST = '/v1/organizations/{organization}/spaces/{space}/secrets';
const ORG_ITEM = '/v1/organizations/{organization}/secrets/{secret}';
const SPACE_ITEM =
  '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}';

interface Call {
  path: string;
  init: {
    params: { path: Record<string, string>; query?: Record<string, unknown> };
    body?: Record<string, unknown>;
  };
}

function makeClient() {
  const calls: Record<'POST' | 'PATCH' | 'DELETE', Call[]> = {
    POST: [],
    PATCH: [],
    DELETE: [],
  };
  const record =
    (kind: 'POST' | 'PATCH' | 'DELETE') =>
    (path: string, init: Call['init']) => {
      calls[kind].push({ path, init });
      return Promise.resolve({ error: undefined });
    };
  return {
    apiClient: {
      POST: vi.fn(record('POST')),
      PATCH: vi.fn(record('PATCH')),
      DELETE: vi.fn(record('DELETE')),
    } as unknown as ApiClient,
    calls,
  };
}

const values: SecretFormValues = {
  secretId: 'stripe-key',
  displayName: 'Stripe key',
  annotations: [],
  value: 's3cr3t',
  rotate: true,
  scope: '',
};

describe('saveSecret — create scope', () => {
  it('POSTs the org rollup path with the value body when scope is empty', () => {
    const { apiClient, calls } = makeClient();
    void saveSecret({
      apiClient,
      mode: 'create',
      editing: null,
      organization: 'acme',
      values: { ...values, scope: '' },
    });
    expect(calls.POST).toHaveLength(1);
    expect(calls.POST[0]?.path).toBe(ORG_LIST);
    expect(calls.POST[0]?.init.params.path.space).toBeUndefined();
    expect(calls.POST[0]?.init.params.query?.secretId).toBe('stripe-key');
    // Create always carries the (base64) value.
    expect(calls.POST[0]?.init.body?.value).toBeDefined();
  });

  it('POSTs the space path when a space scope is selected', () => {
    const { apiClient, calls } = makeClient();
    void saveSecret({
      apiClient,
      mode: 'create',
      editing: null,
      organization: 'acme',
      values: { ...values, scope: 'main' },
    });
    expect(calls.POST).toHaveLength(1);
    expect(calls.POST[0]?.path).toBe(SPACE_LIST);
    expect(calls.POST[0]?.init.params.path.space).toBe('main');
  });
});

describe('saveSecret — update derives scope from the name', () => {
  it('PATCHes the org-direct item path, threading the etag, omitting value when not rotating', () => {
    const { apiClient, calls } = makeClient();
    const editing: Secret = {
      name: 'organizations/acme/secrets/stripe-key',
      etag: 'e1',
    };
    void saveSecret({
      apiClient,
      mode: 'edit',
      editing,
      organization: 'acme',
      values: { ...values, rotate: false },
    });
    expect(calls.PATCH).toHaveLength(1);
    expect(calls.PATCH[0]?.path).toBe(ORG_ITEM);
    expect(calls.PATCH[0]?.init.params.path.space).toBeUndefined();
    expect(calls.PATCH[0]?.init.body?.etag).toBe('e1');
    // Metadata-only edit omits the value (field-mask presence).
    expect(calls.PATCH[0]?.init.body).not.toHaveProperty('value');
  });

  it('PATCHes the space item path for a space-scoped secret', () => {
    const { apiClient, calls } = makeClient();
    const editing: Secret = {
      name: 'organizations/acme/spaces/main/secrets/vizrt-key',
    };
    void saveSecret({
      apiClient,
      mode: 'edit',
      editing,
      organization: 'acme',
      values,
    });
    expect(calls.PATCH).toHaveLength(1);
    expect(calls.PATCH[0]?.path).toBe(SPACE_ITEM);
    expect(calls.PATCH[0]?.init.params.path.space).toBe('main');
    // Rotating → value included.
    expect(calls.PATCH[0]?.init.body?.value).toBeDefined();
  });
});

describe('deleteSecret — path + etag', () => {
  it('DELETEs the org-direct path, threading the etag', () => {
    const { apiClient, calls } = makeClient();
    void deleteSecret({
      apiClient,
      secret: { name: 'organizations/acme/secrets/stripe-key', etag: 'v1' },
    });
    expect(calls.DELETE).toHaveLength(1);
    expect(calls.DELETE[0]?.path).toBe(ORG_ITEM);
    expect(calls.DELETE[0]?.init.params.query?.etag).toBe('v1');
  });

  it('DELETEs the space path for a space-scoped secret', () => {
    const { apiClient, calls } = makeClient();
    void deleteSecret({
      apiClient,
      secret: { name: 'organizations/acme/spaces/main/secrets/vizrt-key' },
    });
    expect(calls.DELETE).toHaveLength(1);
    expect(calls.DELETE[0]?.path).toBe(SPACE_ITEM);
    expect(calls.DELETE[0]?.init.params.path.space).toBe('main');
  });
});
