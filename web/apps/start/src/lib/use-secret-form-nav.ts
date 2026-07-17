import {
  useResourceFormNav,
  type ResourceFormNavConfig,
} from './use-resource-form-nav';

// Secrets' wiring for the generic `useResourceFormNav` (the second consumer, the
// extraction's whole point). The two LIST families (org rollup + space-scoped)
// are invalidated so the changed row shows on return; the two single-secret
// DETAIL families are invalidated so a reopened edit form refetches fresh. Each
// pair is a [method, pathTemplate] openapi-react-query key prefix.
const SECRET_FORM_NAV: ResourceFormNavConfig = {
  listRoute: '/secrets',
  listKeys: [
    ['get', '/v1/organizations/{organization}/secrets'],
    ['get', '/v1/organizations/{organization}/spaces/{space}/secrets'],
  ],
  detailKeys: [
    ['get', '/v1/organizations/{organization}/secrets/{secret}'],
    ['get', '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}'],
  ],
  confirmMessage: 'Discard unsaved changes to this secret?',
};

/**
 * Route-owned navigation for a secret form page — a thin wrapper over the generic
 * {@link useResourceFormNav}, mirroring `useConnectorFormNav`.
 */
export function useSecretFormNav(from: string | undefined) {
  return useResourceFormNav(from, SECRET_FORM_NAV);
}
