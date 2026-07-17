import { resourcePathParams } from '@/workflows/resource-paths';

import { buildSecretCreateBody, buildSecretUpdateBody } from './build-secret-body';

import type { ApiClient } from '@pivox/client';
import type { Secret, SecretFormValues } from '@pivox/ui/resource-admin';
import type { FormMode } from '@pivox/ui/form-page';

const SECRETS_PATH = '/v1/organizations/{organization}/secrets' as const;
const SECRET_PATH =
  '/v1/organizations/{organization}/secrets/{secret}' as const;
const SPACE_SECRETS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets' as const;
const SPACE_SECRET_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}' as const;

/**
 * Item-route params for a secret name. `space` is present when the name is
 * space-scoped (`organizations/*​/spaces/*​/secrets/*`), selecting the
 * space-scoped item path; absent selects the org-direct item path.
 */
export function secretItemParams(name: string): {
  organization: string;
  space?: string;
  secret: string;
} {
  const p = resourcePathParams(name);
  return {
    organization: p.organization ?? '',
    space: p.space,
    secret: p.secret ?? '',
  };
}

/**
 * Creates or updates a secret on the path its scope dictates. Create targets the
 * org or the selected space (`values.scope`); update derives the scope from the
 * secret's name (edit can't move scope). The create body ALWAYS carries the value
 * (required); the update body carries it only when rotating (field-mask presence)
 * — so the two modes build distinct bodies. Shared by the routed create/edit form
 * pages — `FormPage` never sees this RPC.
 */
export function saveSecret(input: {
  apiClient: ApiClient;
  mode: FormMode;
  editing: Secret | null;
  organization: string;
  values: SecretFormValues;
}) {
  const { apiClient, mode, editing, organization, values } = input;

  if (mode === 'create') {
    const body = buildSecretCreateBody(values);
    const query = { secretId: values.secretId };
    return values.scope
      ? apiClient.POST(SPACE_SECRETS_PATH, {
          params: { path: { organization, space: values.scope }, query },
          body,
        })
      : apiClient.POST(SECRETS_PATH, {
          params: { path: { organization }, query },
          body,
        });
  }

  const body = buildSecretUpdateBody({ values, etag: editing?.etag });
  const item = secretItemParams(editing?.name ?? '');
  return item.space
    ? apiClient.PATCH(SPACE_SECRET_PATH, {
        params: {
          path: {
            organization: item.organization,
            space: item.space,
            secret: item.secret,
          },
        },
        body,
      })
    : apiClient.PATCH(SECRET_PATH, {
        params: {
          path: { organization: item.organization, secret: item.secret },
        },
        body,
      });
}

/** Deletes a secret by its full name, threading the optimistic-concurrency etag. */
export function deleteSecret(input: { apiClient: ApiClient; secret: Secret }) {
  const { apiClient, secret } = input;
  const item = secretItemParams(secret.name ?? '');
  const etagQuery = secret.etag ? { etag: secret.etag } : {};
  return item.space
    ? apiClient.DELETE(SPACE_SECRET_PATH, {
        params: {
          path: {
            organization: item.organization,
            space: item.space,
            secret: item.secret,
          },
          query: etagQuery,
        },
      })
    : apiClient.DELETE(SECRET_PATH, {
        params: {
          path: { organization: item.organization, secret: item.secret },
          query: etagQuery,
        },
      });
}
