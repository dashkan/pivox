import { useQueryClient } from '@tanstack/react-query';
import { useBlocker, useRouter } from '@tanstack/react-router';
import { useCallback, useRef, useState } from 'react';

import { resolveConnectorReturn } from './return-to';

// The two connectors LIST query families (org rollup + space-scoped). These are
// the openapi-react-query key prefixes ([method, pathTemplate]) — a partial
// invalidate matches every filter/sort/scope/page variant. The single-record
// path (`…/connectors/{connector}`) is a DIFFERENT template, so it isn't hit.
const CONNECTORS_LIST_KEY = [
  'get',
  '/v1/organizations/{organization}/connectors',
] as const;
const SPACE_CONNECTORS_LIST_KEY = [
  'get',
  '/v1/organizations/{organization}/spaces/{space}/connectors',
] as const;

// The two single-connector DETAIL query families (org-direct + space-scoped).
// Same partial-prefix trick as the lists: a 2-element [method, pathTemplate]
// prefix matches every path-param variant, so the reopened edit form refetches
// fresh instead of reading the pre-edit cache. Harmless on create (no matching
// detail cached).
const CONNECTOR_DETAIL_KEY = [
  'get',
  '/v1/organizations/{organization}/connectors/{connector}',
] as const;
const SPACE_CONNECTOR_DETAIL_KEY = [
  'get',
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}',
] as const;

/**
 * Route-owned navigation for a connector form page. Bundles the three pieces the
 * routes must own to keep `FormPage` router-free:
 *
 *  - `returnTo` — the sanitized `?from=` (open-redirect defense), else the list.
 *  - `goBack` — a SPA navigation to `returnTo` used for cancel + submit-success.
 *  - `onDirtyChange` — fed into `FormPage.Provider.meta` so the derived `dirty`
 *    drives the SOFT in-app-navigation blocker below (the third dirty-guard
 *    mechanism from the design; it MUST live in the route because it's
 *    router-specific — TanStack's `useBlocker`).
 *
 * The blocker prompts on a stray in-app link click while the form is dirty, but
 * NOT on the intentional cancel/submit-success navigations: those go through
 * `goBack`, which sets a one-shot `bypass` so the blocker lets them through
 * (Cancel already ran FormPage's own confirm; submit-success is intentional and
 * the form is still "dirty" at push time because values differ from the seed).
 * `enableBeforeUnload: false` because `FormPage` already owns the `beforeunload`
 * hard-unload guard — we don't want two.
 */
export function useConnectorFormNav(from: string | undefined) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const returnTo = resolveConnectorReturn(from);
  const [dirty, setDirty] = useState(false);
  const bypass = useRef(false);

  const goBack = useCallback(() => {
    bypass.current = true;
    router.history.push(returnTo);
  }, [router, returnTo]);

  // Used on a mutating success (create / edit / delete): invalidate the list
  // families so the changed row shows on return, THEN navigate back. This is a
  // CSR refetch through the BFF proxy — the SSR prefetch is server-only and
  // skipped on this client transition. Cancel uses plain `goBack` (no refetch —
  // nothing changed).
  const goBackAndRefresh = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: CONNECTORS_LIST_KEY });
    void queryClient.invalidateQueries({ queryKey: SPACE_CONNECTORS_LIST_KEY });
    // Also drop the cached single-connector record so reopening the edit form
    // refetches the saved values instead of showing stale pre-edit data.
    void queryClient.invalidateQueries({ queryKey: CONNECTOR_DETAIL_KEY });
    void queryClient.invalidateQueries({ queryKey: SPACE_CONNECTOR_DETAIL_KEY });
    goBack();
  }, [queryClient, goBack]);

  useBlocker({
    shouldBlockFn: () => {
      if (bypass.current) {
        bypass.current = false;
        return false;
      }
      if (!dirty) return false;
      return !globalThis.confirm(
        'Discard unsaved changes to this connector?',
      );
    },
    enableBeforeUnload: false,
  });

  return { returnTo, goBack, goBackAndRefresh, onDirtyChange: setDirty };
}
