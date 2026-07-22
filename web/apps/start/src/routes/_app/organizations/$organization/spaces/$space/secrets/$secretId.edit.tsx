import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { ScopedSecretEdit } from '@/features/secrets/scoped-secret-form';
import { prefetchSecret } from '@/server/prefetch';

const SPACE_SECRET_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}' as const;

function validateFormSearch(search: Record<string, unknown>): { from?: string } {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute(
  '/_app/organizations/$organization/spaces/$space/secrets/$secretId/edit',
)({
  validateSearch: validateFormSearch,
  /** SSR-prefetch the space-scoped secret record and prime it under the client key. */
  loader: async ({ context, params }) => {
    if (typeof window !== 'undefined') return;
    const secret = await prefetchSecret({
      data: {
        orgSlug: params.organization,
        space: params.space,
        secretId: params.secretId,
      },
    });
    if (secret && secret.space) {
      const { queryKey } = $api.queryOptions('get', SPACE_SECRET_PATH, {
        params: {
          path: {
            organization: secret.orgSlug,
            space: secret.space,
            secret: secret.secretId,
          },
        },
      });
      context.queryClient.setQueryData(queryKey, secret.secret);
    }
  },
  component: SpaceSecretEditPage,
});

function SpaceSecretEditPage() {
  const { organization, space, secretId } = Route.useParams();
  const { from } = Route.useSearch();
  return (
    <ScopedSecretEdit
      orgSlug={organization}
      spaceSlug={space}
      secretId={secretId}
      from={from}
    />
  );
}
