import { createFileRoute } from '@tanstack/react-router';

import { ScopedSecretNew } from '@/features/secrets/scoped-secret-form';

function validateFormSearch(search: Record<string, unknown>): { from?: string } {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute(
  '/_app/organizations/$organization/spaces/$space/secrets/new',
)({
  validateSearch: validateFormSearch,
  component: SpaceSecretNewPage,
});

function SpaceSecretNewPage() {
  const { organization, space } = Route.useParams();
  const { from } = Route.useSearch();
  return <ScopedSecretNew orgSlug={organization} spaceSlug={space} from={from} />;
}
