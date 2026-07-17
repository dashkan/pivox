import { SecretCreateFeature } from '@pivox/features/secrets';
import { useAppShellContext } from '@pivox/ui/app-shell';
import { AdminNotice } from '@pivox/ui/resource-admin';
import { createFileRoute } from '@tanstack/react-router';

import { $api, apiClient } from '@/lib/api-client';
import { useSecretFormNav } from '@/lib/use-secret-form-nav';

/** Search for the form routes: the launching route to return to (sanitized on read). */
interface SecretFormSearch {
  from?: string;
}

function validateFormSearch(search: Record<string, unknown>): SecretFormSearch {
  return typeof search.from === 'string' && search.from
    ? { from: search.from }
    : {};
}

export const Route = createFileRoute('/_app/secrets/new')({
  validateSearch: validateFormSearch,
  component: SecretNewPage,
});

function SecretNewPage() {
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;
  const search = Route.useSearch();

  // The route (not FormPage) owns the return target + the soft-navigation dirty
  // guard. FormPage stays router-free — we inject navigate + onDirtyChange.
  const { returnTo, goBack, goBackAndRefresh, onDirtyChange } = useSecretFormNav(
    search.from,
  );

  if (!parent) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <AdminNotice>Select an organization to create a secret.</AdminNotice>
      </div>
    );
  }

  return (
    <SecretCreateFeature
      $api={$api}
      apiClient={apiClient}
      parent={parent}
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
