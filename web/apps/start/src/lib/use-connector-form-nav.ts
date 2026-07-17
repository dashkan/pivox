import { useBlocker, useRouter } from '@tanstack/react-router';
import { useCallback, useRef, useState } from 'react';

import { resolveConnectorReturn } from './return-to';

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
  const returnTo = resolveConnectorReturn(from);
  const [dirty, setDirty] = useState(false);
  const bypass = useRef(false);

  const goBack = useCallback(() => {
    bypass.current = true;
    router.history.push(returnTo);
  }, [router, returnTo]);

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

  return { returnTo, goBack, onDirtyChange: setDirty };
}
