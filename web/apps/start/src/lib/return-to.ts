import { safeInternalPath } from '@pivox/ui/form-page';

/** The search-param key that carries the launching route forward to a form page. */
export const RETURN_PARAM = 'from';

/** Where a connector form returns when `?from=` is absent or fails sanitizing. */
export const CONNECTORS_LIST_ROUTE = '/connectors';

/**
 * The app origin used to resolve a candidate `from` path. On the client this is
 * the real origin; on the SSR pass it is a stable placeholder. Either way the
 * result is identical for a valid RELATIVE `from` (the only kind the list ever
 * emits), because `safeInternalPath` returns only `pathname+search+hash` — the
 * origin never leaks — so there is no hydration mismatch.
 */
function appOrigin(): string {
  return typeof window === 'undefined'
    ? 'http://ssr.invalid'
    : window.location.origin;
}

/**
 * Sanitize the `?from=` value to a safe same-app path, falling back to a given
 * resource-list route. This is the open-redirect defense shared by every routed
 * resource form (connectors, secrets, …): an attacker-supplied external /
 * protocol-relative / backslash `from` is rejected and the user lands on the
 * resource's list instead. `FormPage` never runs this — the route's nav hook
 * does, then injects `cancel` / `onSubmitSuccess` that navigate to the result.
 */
export function resolveReturnTo(
  from: string | undefined,
  fallback: string,
): string {
  return safeInternalPath(from, appOrigin()) ?? fallback;
}

/**
 * Connectors-specific `resolveReturnTo`, kept as a thin wrapper so existing
 * connector call sites (and their tests) stay unchanged while the generic
 * resolver backs both consumers.
 */
export function resolveConnectorReturn(from: string | undefined): string {
  return resolveReturnTo(from, CONNECTORS_LIST_ROUTE);
}
