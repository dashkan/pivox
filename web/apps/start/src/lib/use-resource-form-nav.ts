import { useQueryClient } from '@tanstack/react-query';
import { useBlocker, useRouter } from '@tanstack/react-router';
import { useCallback, useRef, useState } from 'react';

import { resolveReturnTo } from './return-to';

/**
 * An openapi-react-query key PREFIX: the `[method, pathTemplate]` pair a partial
 * `invalidateQueries` matches. A 2-element prefix matches every path-param /
 * filter / sort / scope / page variant of that template, so one entry invalidates
 * a whole query family (the org-rollup list, or every single-record detail).
 */
export type ResourceQueryKeyPrefix = readonly [method: string, pathTemplate: string];

/**
 * The per-resource wiring the generic form-nav hook needs. Everything that
 * varies between connectors and secrets lives here; the machinery below
 * (returnTo sanitizing, goBack, cache invalidation, the dirty guard) is shared.
 */
export interface ResourceFormNavConfig {
  /** Default return target + the list route the `?from=` sanitizer falls back to. */
  listRoute: string;
  /**
   * The resource's LIST query families (org rollup + space-scoped). Invalidated
   * on a mutating success so the changed row shows on return.
   */
  listKeys: readonly ResourceQueryKeyPrefix[];
  /**
   * The resource's single-record DETAIL families (org-direct + space-scoped).
   * Invalidated too, so reopening the edit form refetches fresh instead of
   * reading the pre-edit cache. Harmless on create (no matching detail cached).
   */
  detailKeys: readonly ResourceQueryKeyPrefix[];
  /** Copy for the soft in-app navigation blocker's discard confirm. */
  confirmMessage: string;
}

/**
 * Route-owned navigation for a routed resource form page, shared by connectors
 * and secrets (the two real consumers — deliberately not abstracted further).
 * Bundles the three pieces the routes must own to keep `FormPage` router-free:
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
export function useResourceFormNav(
  from: string | undefined,
  config: ResourceFormNavConfig,
) {
  const { listRoute, listKeys, detailKeys, confirmMessage } = config;
  const router = useRouter();
  const queryClient = useQueryClient();
  const returnTo = resolveReturnTo(from, listRoute);
  const [dirty, setDirty] = useState(false);
  const bypass = useRef(false);

  const goBack = useCallback(() => {
    bypass.current = true;
    router.history.push(returnTo);
  }, [router, returnTo]);

  // Used on a mutating success (create / edit / delete): mark the list families
  // stale so the changed row shows on return, and the detail families so a
  // reopened edit form refetches, THEN navigate back.
  //
  // `refetchType: 'none'` is load-bearing. The default ('active') EAGERLY
  // refetches every matching observed query the instant we invalidate. For the
  // list that races the destination list route, which fires its own fetch on
  // mount (staleTime 0) — react-query then cancels one in favour of the other,
  // and the Network tab shows a doubled request (`…/connectors` canceled, then
  // 200). 'none' invalidates WITHOUT an eager refetch: the query is left stale,
  // so the list refetches exactly ONCE, on mount, with fresh post-save data.
  // The detail families are inactive on the list (and, on edit, about to unmount
  // — an 'active' refetch there would be started only to be canceled), so 'none'
  // is right for them too: they refetch on the next edit-open, not now.
  const goBackAndRefresh = useCallback(() => {
    for (const queryKey of listKeys) {
      void queryClient.invalidateQueries({ queryKey, refetchType: 'none' });
    }
    for (const queryKey of detailKeys) {
      void queryClient.invalidateQueries({ queryKey, refetchType: 'none' });
    }
    goBack();
  }, [queryClient, listKeys, detailKeys, goBack]);

  useBlocker({
    shouldBlockFn: () => {
      if (bypass.current) {
        bypass.current = false;
        return false;
      }
      if (!dirty) return false;
      return !globalThis.confirm(confirmMessage);
    },
    enableBeforeUnload: false,
  });

  return { returnTo, goBack, goBackAndRefresh, onDirtyChange: setDirty };
}
