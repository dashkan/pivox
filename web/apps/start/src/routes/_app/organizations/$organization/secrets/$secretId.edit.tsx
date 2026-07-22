import { createFileRoute } from '@tanstack/react-router';

import { $api } from '@/lib/api-client';
import { ScopedSecretEdit } from '@/features/secrets/scoped-secret-form';
import { prefetchSecret } from '@/server/prefetch';

const SECRET_PATH =
  '/v1/organizations/{organization}/secrets/{secret}' as const;

/** Search for the form route: the launching route to return to (sanitized on read). */
function validateFormSearch(search: Record<string, unknown>): { from?: string } {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute(
  '/_app/organizations/$organization/secrets/$secretId/edit',
)({
  validateSearch: validateFormSearch,
  /**
   * SSR-prefetch the single (org-direct) secret record and prime it under the
   * byte-identical `$api` key the client hook (`useSecretForm`) reads — so the
   * record is in the server HTML and no XHR fires on load.
   */
  loader: async ({ context, params }) => {
    if (typeof window !== 'undefined') return;
    const secret = await prefetchSecret({
      data: { orgSlug: params.organization, secretId: params.secretId },
    });
    if (secret) {
      const { queryKey } = $api.queryOptions('get', SECRET_PATH, {
        params: {
          path: { organization: secret.orgSlug, secret: secret.secretId },
        },
      });
      context.queryClient.setQueryData(queryKey, secret.secret);
    }
  },
  component: SecretEditPage,
});

function SecretEditPage() {
  const { organization, secretId } = Route.useParams();
  const { from } = Route.useSearch();
  return (
    <ScopedSecretEdit orgSlug={organization} secretId={secretId} from={from} />
  );
}
