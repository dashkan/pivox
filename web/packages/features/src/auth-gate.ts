/**
 * `@pivox/features/auth-gate` — top-of-app gate that redirects
 * unauthenticated users to `/auth/login`. Wraps every route subtree
 * meant to be authenticated; downstream gates (`OrgGateFeature`,
 * etc.) layer their own preconditions on top.
 */

export { AuthGateFeature } from './auth-gate/auth-gate-feature';
