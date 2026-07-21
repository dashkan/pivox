'use client';

import { secretsListView } from '@pivox/ui/resource-admin';

import { buildSecretsListRequest } from '@/secrets/build-secrets-request';
import { deleteSecret, saveSecret } from '@/secrets/save-secret';

import type {
  FormDescriptor,
  ListDescriptor,
  ResourceAdmin,
} from '@/resource-admin';
import type { components } from '@pivox/client/types';
import type {
  Secret,
  SecretFormValues,
  SecretListExtras,
  SpaceOption,
} from '@pivox/ui/resource-admin';

type ListSecretsResponse = components['schemas']['v1ListSecretsResponse'];

const SECRETS_PATH = '/v1/organizations/{organization}/secrets' as const;
const SPACE_SECRETS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets' as const;
const SECRET_PATH =
  '/v1/organizations/{organization}/secrets/{secret}' as const;
const SPACE_SECRET_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}' as const;

/**
 * react-query `placeholderData` that keeps the previous page rendered while a new
 * query key (filter / scope / page change) loads — no empty flash, no `isLoading`
 * flip. Equivalent to `keepPreviousData`, inlined to avoid a direct dep on it.
 */
function keepPrevious<T>(previous: T): T {
  return previous;
}

/** The route-owned slice of the list extras: SSR-prefetched spaces to scope by. */
export interface SecretListInjected {
  spaceOptions: SpaceOption[];
}

/**
 * The secrets LIST descriptor (data side), the secret twin of
 * `connectorsListDescriptor`. The scope switches the list PARENT path; both
 * queries are declared so the hook count stays stable and only the one matching
 * the scope is enabled. Its react-query key is byte-identical to the SSR loader's
 * prime (same literal path + shared `buildSecretsListRequest`), so primed rows
 * hydrate instead of refetching. Secrets carry no per-response facet (no agent
 * equivalent), so `extrasOf` just passes the injected spaces through.
 */
export const secretsListDescriptor: ListDescriptor<
  Secret,
  SecretListExtras,
  SecretListInjected,
  ListSecretsResponse
> = {
  key: 'secrets',
  useList({ $api, organization, state }) {
    // The shared builder produces the exact query the SSR loader keys on.
    const { query } = buildSecretsListRequest(organization, state);
    const scope = state.scope;

    const orgListQuery = $api.useQuery(
      'get',
      SECRETS_PATH,
      { params: { path: { organization }, query } },
      { enabled: scope === '', placeholderData: keepPrevious },
    );
    const spaceListQuery = $api.useQuery(
      'get',
      SPACE_SECRETS_PATH,
      { params: { path: { organization, space: scope }, query } },
      { enabled: scope !== '', placeholderData: keepPrevious },
    );
    const listQuery = scope === '' ? orgListQuery : spaceListQuery;
    return {
      data: listQuery.data,
      isLoading: listQuery.isLoading,
      // react-query uses `null` for "no error"; the ApiError arm is `undefined`.
      error: listQuery.error ?? undefined,
      refetch: listQuery.refetch,
    };
  },
  rowsOf: (data) => data?.secrets ?? [],
  nextPageTokenOf: (data) => data?.nextPageToken,
  rowId: (secret) => secret.name ?? '',
  extrasOf: (_data, injected) => ({ spaceOptions: injected.spaceOptions }),
  remove: (apiClient, secret) => deleteSecret({ apiClient, secret }),
  loadErrorFallback: "Couldn't load secrets.",
  view: secretsListView,
};

/**
 * The secrets FORM descriptor (data side), the secret twin of
 * `connectorsFormDescriptor`. The single-record detail query is keyed identically
 * to the SSR loader's `setQueryData` (same `$api` params) so the prefetched
 * record hydrates with no XHR on load. The write-only value invariant lives in
 * `saveSecret` (create always writes it; update writes it only when rotating).
 */
export const secretsFormDescriptor: FormDescriptor<Secret, SecretFormValues> = {
  useRecord({ $api, organization, id, space, enabled }) {
    const recordQuery = $api.useQuery(
      'get',
      space ? SPACE_SECRET_PATH : SECRET_PATH,
      {
        params: {
          path: space
            ? { organization, space, secret: id ?? '' }
            : { organization, secret: id ?? '' },
        },
      },
      { enabled, placeholderData: keepPrevious },
    );
    return {
      data: recordQuery.data,
      isLoading: recordQuery.isLoading,
      // react-query uses `null` for "no error"; the ApiError arm is `undefined`.
      error: recordQuery.error ?? undefined,
    };
  },
  save: (input) => saveSecret(input),
  remove: (apiClient, secret) => deleteSecret({ apiClient, secret }),
  loadErrorFallback: "Couldn't load this secret.",
  saveErrorFallback: "Couldn't save the secret.",
};

/** The secrets admin — List + default form create/edit (no custom view). */
export const secretsResourceAdmin: ResourceAdmin<
  Secret,
  SecretFormValues,
  SecretListExtras,
  SecretListInjected,
  ListSecretsResponse
> = {
  list: secretsListDescriptor,
  form: secretsFormDescriptor,
};
