'use client';

import { SidebarProvider as ShadcnSidebarProvider } from '@pivox/primitives/sidebar';
import { SIDEBAR_OPEN, storage, subscribeToChanges } from '@pivox/storage';
import { useEffect, useState, type ComponentProps } from 'react';

// Shadcn doesn't export a props type for SidebarProvider — derive
// it via ComponentProps so additions in upstream stay covered.
type ShadcnSidebarProviderProps = ComponentProps<typeof ShadcnSidebarProvider>;

/**
 * Pivox-app wrapper around shadcn's `<SidebarProvider>`. Routes
 * sidebar open/closed state through `@pivox/storage` so persistence
 * is unified with the rest of the app's preferences (theme,
 * active-org, remember-email):
 *
 *   - On the start app (cookie backend), the value is written to a
 *     `pivox.sidebar-state` cookie that SSR can read via h3's
 *     `getCookie`, threaded into `initialOpen` for the first paint.
 *   - On electron (localStorage backend), the value persists across
 *     launches via localStorage — shadcn's own `document.cookie`
 *     write at line 83 of `sidebar.tsx` does NOT persist on file://,
 *     so without this wrapper electron's sidebar would reset to
 *     `defaultOpen={true}` on every cold launch.
 *   - Cross-tab / cross-window sync via the BroadcastChannel surface
 *     in `@pivox/storage`'s `notify.ts` — same shape as ThemeSwitcher.
 *
 * # Why a wrapper instead of modifying shadcn
 *
 * Vendored shadcn code stays untouched (project policy). This
 * component uses shadcn's documented controlled-mode props (`open`,
 * `onOpenChange`) so behavior is layered on without diverging from
 * upstream.
 *
 * # Tradeoff: shadcn still writes its own dead cookie
 *
 * `sidebar.tsx` line 83 unconditionally writes
 * `document.cookie = "sidebar_state=..."` whenever `setOpen` fires
 * inside shadcn (including under controlled mode). That cookie is
 * NEVER read by anything in Pivox — it's pure noise visible in
 * DevTools. The Pivox-managed `pivox.sidebar-state` cookie owns
 * persistence. Accepted as the cost of not modifying shadcn.
 */
export type SidebarProviderProps = Omit<
  ShadcnSidebarProviderProps,
  'open' | 'onOpenChange' | 'defaultOpen'
> & {
  /**
   * SSR-resolved initial value from the `pivox.sidebar-state` cookie.
   * Threaded by the route's beforeLoad so the lazy useState
   * initializer matches the server-rendered HTML exactly — no
   * hydration mismatch on the first paint.
   *
   * Optional. When omitted (electron, pure CSR), the wrapper falls
   * back to a client-side `storage.get(SIDEBAR_OPEN)` and finally to
   * shadcn's default of `true`.
   */
  initialOpen?: boolean;
};

export function SidebarProvider({
  initialOpen,
  children,
  ...rest
}: SidebarProviderProps) {
  // Lazy initializer mirrors useAppShell's `initialActiveOrganization`
  // pattern: prefer the SSR-seeded value (matches server HTML exactly),
  // then fall through to client-side storage, then to the shadcn
  // default (`true`).
  //
  // The order matters on start: SSR passes initialOpen, and the
  // client's first render MUST use that same value to avoid a
  // hydration mismatch. `storage.get` on the client would read the
  // same cookie, but going through the cached @pivox/storage layer
  // means we trust the value from the same source on both sides.
  const [open, setOpenState] = useState<boolean>(() => {
    if (initialOpen !== undefined) return initialOpen;
    if (typeof window === 'undefined') return true;
    return storage.get(SIDEBAR_OPEN) ?? true;
  });

  // Cross-context subscription. BroadcastChannel delivers to OTHER
  // tabs/windows; the cache inside @pivox/storage is already updated
  // by the time this handler fires (see notify.ts). storage.get
  // returns the freshly-broadcast value, NOT the stale cookie.
  useEffect(() => {
    const unsubscribe = subscribeToChanges(SIDEBAR_OPEN.name, () => {
      const fresh = storage.get(SIDEBAR_OPEN);
      if (fresh !== null) setOpenState(fresh);
    });
    return unsubscribe;
  }, []);

  const handleOpenChange = (next: boolean) => {
    setOpenState(next);
    // storage.set: writes to the cookie (start) or localStorage
    // (electron), primes the local cache, AND posts a broadcast for
    // other tabs/windows. See @pivox/storage/operations.ts.
    storage.set(SIDEBAR_OPEN, next);
  };

  return (
    <ShadcnSidebarProvider
      open={open}
      onOpenChange={handleOpenChange}
      {...rest}
    >
      {children}
    </ShadcnSidebarProvider>
  );
}
