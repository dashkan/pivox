'use client';

import { useAuth } from '@/auth/use-auth';

import type { NavigateComponent } from '@/navigation';

/**
 * Gates the authenticated app shell on the user having a Keycloak session. Wrap
 * any route subtree that should only render for signed-in users; unauthenticated
 * visits redirect to `/auth/login`. Used by the Electron app (whose IPC-backed
 * provider has a real `loading` phase during boot restore); the web app gates
 * server-side instead.
 *
 *   - While auth is settling (`loading`), renders a tiny "Loading…"
 *     splash so we don't flash content for an unresolved state.
 *   - When there's no user, returns `<Navigate to="/auth/login" />`.
 *     Router primitive is Strict-Mode-safe and avoids the
 *     effect-driven imperative-navigate anti-pattern.
 *   - When there's a user, renders children. Downstream gates
 *     (`OrgGateFeature`, etc.) layer their own preconditions on top.
 *
 * The destination is hardcoded because the gate's purpose IS the
 * redirect; there's no use case for a different login URL. Only the
 * router primitive is injected (`Navigate`) so this package stays
 * router-agnostic — the consumer adapts its router's redirect
 * component to `NavigateComponent`.
 *
 * Mirrors the SwiftUI native auth check at
 * `native/platform/macos/swift/Auth/AuthGate.swift`.
 */
export function AuthGateFeature({
  Navigate,
  children,
}: {
  Navigate: NavigateComponent;
  children: React.ReactNode;
}) {
  const { user, loading } = useAuth();

  if (loading) return <AuthGateLoading />;
  if (!user) return <Navigate to="/auth/login" replace />;

  return <>{children}</>;
}

function AuthGateLoading() {
  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <p className="text-sm text-muted-foreground">Loading…</p>
    </div>
  );
}
