import {
  useResourceFormNav,
  type ResourceFormNavConfig,
} from './use-resource-form-nav';

// Connectors' wiring for the generic `useResourceFormNav`. The two LIST families
// (org rollup + space-scoped) are invalidated so the changed row shows on
// return; the two single-connector DETAIL families are invalidated so a reopened
// edit form refetches fresh instead of reading the pre-edit cache. Each pair is a
// [method, pathTemplate] openapi-react-query key prefix — a partial invalidate
// matches every filter/sort/scope/page (list) or path-param (detail) variant.
const CONNECTOR_FORM_NAV: ResourceFormNavConfig = {
  listRoute: '/connectors',
  listKeys: [
    ['get', '/v1/organizations/{organization}/connectors'],
    ['get', '/v1/organizations/{organization}/spaces/{space}/connectors'],
  ],
  detailKeys: [
    ['get', '/v1/organizations/{organization}/connectors/{connector}'],
    ['get', '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}'],
  ],
  confirmMessage: 'Discard unsaved changes to this connector?',
};

/**
 * Route-owned navigation for a connector form page — a thin wrapper over the
 * generic {@link useResourceFormNav} (shared with secrets). Behavior is
 * unchanged from the previous connector-specific implementation.
 */
export function useConnectorFormNav(from: string | undefined) {
  return useResourceFormNav(from, CONNECTOR_FORM_NAV);
}
