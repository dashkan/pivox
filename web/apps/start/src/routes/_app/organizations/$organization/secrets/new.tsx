import { createFileRoute } from '@tanstack/react-router';

import { ScopedSecretNew } from '@/features/secrets/scoped-secret-form';

/** Search for the form route: the launching route to return to (sanitized on read). */
function validateFormSearch(search: Record<string, unknown>): { from?: string } {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute(
  '/_app/organizations/$organization/secrets/new',
)({
  validateSearch: validateFormSearch,
  component: SecretNewPage,
});

function SecretNewPage() {
  const { organization } = Route.useParams();
  const { from } = Route.useSearch();
  return <ScopedSecretNew orgSlug={organization} from={from} />;
}
