'use client';

import { CreateOrgCard } from '@pivox/ui/create-org-card';

import { useCreateOrg } from './use-create-org';

import type { ApiClient } from '@pivox/client';

export function CreateOrgFeature({
  apiClient,
  onSuccess,
  onSignOut,
  children,
}: {
  apiClient: ApiClient;
  onSuccess?: () => void;
  onSignOut?: () => void;
  children: React.ReactNode;
}) {
  const value = useCreateOrg({ apiClient, onSuccess, onSignOut });

  return (
    <CreateOrgCard.Provider value={value}>{children}</CreateOrgCard.Provider>
  );
}
