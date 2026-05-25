'use client';

import { useActionState, useRef, useState } from 'react';

import type { ApiClient } from '@pivox/client';
import type {
  CreateOrgActions,
  CreateOrgContextValue,
  CreateOrgMeta,
  CreateOrgState,
} from '@pivox/ui/create-org-card';

import { useAuth } from '@/auth/use-auth';
import { isValidSlug, slugify } from '@/create-org/slug';


/**
 * Drives the create-org screen. Owns the slug auto-derive UX (mirrors
 * SwiftUI native `CreateOrgView`), the form submit through
 * `apiClient.POST('/v1/organizations', ...)`, and the synchronously-
 * completed LRO unpacking the server returns.
 *
 * `onSuccess` fires once the operation reports `done: true` without
 * an error. The created org is not threaded through — callers route
 * to `/` and let `OrgGateFeature` re-fetch the membership list, which
 * picks up the new org. (This also keeps the `protobufAny` response
 * unpack inside the feature instead of leaking to the route.)
 */
export function useCreateOrg(input: {
  apiClient: ApiClient;
  onSuccess?: () => void;
  onSignOut?: () => void;
}): CreateOrgContextValue {
  const { apiClient, onSuccess, onSignOut } = input;
  const { signOut } = useAuth();
  const displayNameRef = useRef<HTMLInputElement | null>(null);
  const [displayName, setDisplayName] = useState('');
  const [organizationId, setOrganizationId] = useState('');
  const [slugTouched, setSlugTouched] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [, formAction] = useActionState(async () => {
    setError(null);
    const trimmedName = displayName.trim();
    if (!trimmedName) {
      setError('Organization name is required.');
      return;
    }
    if (!isValidSlug(organizationId)) {
      setError('Short name must be 4–20 characters and match the rules below.');
      return;
    }

    try {
      const resp = await apiClient.POST('/v1/organizations', {
        params: { query: { organizationId } },
        body: { displayName: trimmedName },
      });
      // openapi-fetch returns a discriminated union: `{ data, error? }`
      // for success vs `{ data?, error }` for failure. Branch on
      // `resp.error` first so the type narrowing on `resp.data` /
      // `resp.error` is preserved — destructuring up front flattens
      // the union and trips eslint's no-unnecessary-condition rule.
      if (resp.error) {
        // grpc-gateway returns rpcStatus on error; surface its
        // message if present.
        setError(
          resp.error.message ??
            "Couldn't create your organization. Please try again.",
        );
        return;
      }
      // CreateOrganization is a synchronously-completed LRO: `done`
      // should always be true and `response` carries the org. Mirror
      // the SwiftUI client's defensive unpack (see
      // native/platform/macos/swift/Auth/OrgsClient.swift).
      const op = resp.data;
      if (op.error) {
        setError(op.error.message ?? "Couldn't create your organization.");
        return;
      }
      if (!op.done) {
        setError(
          'Organization creation is taking longer than expected. Please refresh.',
        );
        return;
      }
      onSuccess?.();
    } catch {
      setError("Couldn't create your organization. Please try again.");
    }
  }, null);

  /**
   * Sync slug from displayName while the user hasn't manually edited
   * the slug. Once the user types in the slug field, auto-derive
   * stops — matching the Slack workspace / Notion team behavior the
   * SwiftUI native screen uses.
   */
  const updateDisplayName = (next: string): void => {
    setDisplayName(next);
    if (!slugTouched) {
      setOrganizationId(slugify(next));
    }
  };

  /**
   * Manual edits flip `slugTouched`. Clearing the slug resets the
   * flag so the next character typed in displayName resumes the
   * auto-derive — same affordance as the SwiftUI screen.
   */
  const updateOrganizationId = (next: string): void => {
    setOrganizationId(next);
    setSlugTouched(next.length > 0);
  };

  const state: CreateOrgState = { displayName, organizationId, error };
  const actions: CreateOrgActions = {
    updateDisplayName,
    updateOrganizationId,
    formAction,
    signOut: () => {
      void signOut();
      onSignOut?.();
    },
  };
  const meta: CreateOrgMeta = { displayNameRef, slugTouched };

  return { state, actions, meta };
}
