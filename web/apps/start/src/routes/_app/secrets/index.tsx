import { SecretsFeature } from '@pivox/features/secrets';
import { AdminNotice } from '@pivox/ui/resource-admin';
import { useAppShellContext } from '@pivox/ui/app-shell';
import { createFileRoute } from '@tanstack/react-router';

import { $api, apiClient } from '@/lib/api-client';

export const Route = createFileRoute('/_app/secrets/')({
  component: SecretsPage,
});

function SecretsPage() {
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;

  if (!parent) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <AdminNotice>Select an organization to manage secrets.</AdminNotice>
      </div>
    );
  }

  return <SecretsFeature $api={$api} apiClient={apiClient} parent={parent} />;
}
