import type { ReactNode } from 'react';

/**
 * Router-agnostic navigation primitive injected into the route gates.
 *
 * `@pivox/features` must not depend on any router: the gates
 * (`AuthGateFeature`, `OrgGateFeature`) render a redirect, but the
 * *mechanism* is the consumer's concern. Each app injects a component
 * that performs its router's declarative redirect — TanStack Router's
 * `<Navigate>` in both the Electron renderer and the (future) client
 * gates — adapted to this loose shape.
 *
 * Only the primitive is injected; the destinations (`/auth/login`,
 * `/auth/create-org`) stay hardcoded inside the gates because the
 * redirect target IS what each gate is for.
 */
export type NavigateComponent = (props: {
  to: string;
  replace?: boolean;
}) => ReactNode;
