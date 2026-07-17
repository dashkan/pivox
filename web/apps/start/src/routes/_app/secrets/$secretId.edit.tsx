import { SecretEditFeature } from '@pivox/features/secrets';
import { useAppShellContext } from '@pivox/ui/app-shell';
import { AdminNotice } from '@pivox/ui/resource-admin';
import { createFileRoute } from '@tanstack/react-router';

import { $api, apiClient } from '@/lib/api-client';
import { useSecretFormNav } from '@/lib/use-secret-form-nav';
import { prefetchSecret } from '@/server/prefetch';

const SECRET_PATH =
  '/v1/organizations/{organization}/secrets/{secret}' as const;
const SPACE_SECRET_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}' as const;

/** Search: the launching route (`from`) + the secret's space slug (org-direct = absent). */
interface SecretEditSearch {
  from?: string;
  space?: string;
}

function validateEditSearch(search: Record<string, unknown>): SecretEditSearch {
  const out: SecretEditSearch = {};
  if (typeof search.from === 'string' && search.from) out.from = search.from;
  if (typeof search.space === 'string' && search.space) out.space = search.space;
  return out;
}

export const Route = createFileRoute('/_app/secrets/$secretId/edit')({
  validateSearch: validateEditSearch,
  // Re-run the loader when the target space changes (org-direct vs space-scoped).
  loaderDeps: ({ search }) => ({ space: search.space }),
  /**
   * SSR-prefetch the single secret record and prime it under the byte-identical
   * `$api` key the client hook (`useSecretForm`) reads — so the record is in the
   * server HTML and no XHR fires on load. Client navigations skip this;
   * `useSecretForm`'s `keepPreviousData` avoids a flash.
   */
  loader: async ({ params, deps, context }) => {
    if (typeof window !== 'undefined') return;
    const secret = await prefetchSecret({
      data: { secretId: params.secretId, space: deps.space },
    });
    if (secret) {
      const { queryKey } = secret.space
        ? $api.queryOptions('get', SPACE_SECRET_PATH, {
            params: {
              path: {
                organization: secret.orgSlug,
                space: secret.space,
                secret: secret.secretId,
              },
            },
          })
        : $api.queryOptions('get', SECRET_PATH, {
            params: {
              path: {
                organization: secret.orgSlug,
                secret: secret.secretId,
              },
            },
          });
      context.queryClient.setQueryData(queryKey, secret.secret);
    }
  },
  component: SecretEditPage,
});

function SecretEditPage() {
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;
  const { secretId } = Route.useParams();
  const search = Route.useSearch();

  const { returnTo, goBack, goBackAndRefresh, onDirtyChange } = useSecretFormNav(
    search.from,
  );

  if (!parent) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <AdminNotice>Select an organization to edit this secret.</AdminNotice>
      </div>
    );
  }

  return (
    <SecretEditFeature
      $api={$api}
      apiClient={apiClient}
      parent={parent}
      secretId={secretId}
      space={search.space}
      back={
        <a
          href={returnTo}
          className="hover:underline"
          onClick={(e) => {
            e.preventDefault();
            goBack();
          }}
        >
          ← Secrets
        </a>
      }
      onCancel={goBack}
      onSubmitSuccess={goBackAndRefresh}
      onDirtyChange={onDirtyChange}
    />
  );
}
