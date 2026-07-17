import { Navigate as RouterNavigate } from '@tanstack/react-router';

import type { NavigateComponent } from '@pivox/features/auth-gate';

/**
 * Adapts TanStack Router's strictly-typed `<Navigate>` to the loose
 * `NavigateComponent` primitive that `@pivox/features` route gates inject.
 * `@pivox/features` is router-agnostic — the router is the app's concern —
 * so the renderer supplies this one adapter and passes it to the gates.
 * Defined once here so each gate call site stays a single terse prop.
 */
export const Navigate: NavigateComponent = ({ to, replace }) => (
  <RouterNavigate to={to} replace={replace} />
);
