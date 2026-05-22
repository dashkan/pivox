/**
 * `@pivox/features/broker` — the shared OAuth broker-flow foundation.
 *
 * Consumed by the per-platform `RedirectTransport` implementations (the
 * browser popup transport in `apps/start`, the loopback / custom-scheme
 * transport in `apps/electron`) and by the auth feature hooks.
 */

export type {
  BrokerCredentialKind,
  BrokerRedirectResult,
  RedirectTransport,
} from '@/shared/redirect-transport';
export {
  buildBrokerStartUrl,
  parseBrokerRedirect,
} from '@/shared/redirect-transport';
export { buildBrokerCredential } from '@/shared/broker-credential';
export { resolveSsoProvider } from '@/shared/resolve-sso-provider';
