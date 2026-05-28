'use client';

import { SidebarProvider as ShadcnSidebarProvider } from '@pivox/primitives/sidebar';
import { SIDEBAR_OPEN, storage } from '@pivox/storage';
import { useStorageValue } from '@pivox/storage/react';
import { type ComponentProps } from 'react';

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
 *
 * # Per-tab semantics, not cross-tab
 *
 * `SIDEBAR_OPEN` ships with `broadcast: false`. Different tabs are
 * different workflows; toggling the sidebar in one window does NOT
 * collapse it in another open window. Reload-in-same-tab still
 * picks up the latest persisted value (cookies are origin-shared,
 * so the cookie reflects whatever was last written by any tab — but
 * within a single tab's session, the user's last toggle stays put).
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
   * Threaded by the route's beforeLoad so the hook's server snapshot
   * matches the client snapshot — no hydration mismatch on the
   * first paint.
   *
   * Optional. When omitted (electron, pure CSR), the hook's client
   * snapshot falls back to `storage.get(SIDEBAR_OPEN)`; if that's
   * null (first launch, no persisted state), we use shadcn's
   * default of `true`.
   */
  initialOpen?: boolean;
};

export function SidebarProvider({
  initialOpen,
  children,
  ...rest
}: SidebarProviderProps) {
  // useStorageValue centralizes the read + same-window pub-sub +
  // (conditional) cross-tab broadcast subscription. Since
  // SIDEBAR_OPEN has `broadcast: false`, no BroadcastChannel
  // subscriber is attached — only same-window pub-sub fires, so
  // toggling in this tab updates this tab's UI but does NOT touch
  // other tabs' sidebar state. Per-tab semantics by design.
  const stored = useStorageValue(SIDEBAR_OPEN, initialOpen);
  // Null means "not yet persisted" — first session in this tab/
  // browser. Default to open (shadcn's default).
  const open = stored ?? true;

  const handleOpenChange = (next: boolean) => {
    // storage.set fires the same-window pub-sub → useStorageValue
    // re-reads → React re-renders with the new value. No manual
    // setState needed.
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
