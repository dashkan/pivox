import { OAuthProvider, getAuth, signInWithCredential } from 'firebase/auth';

import type { RedirectTransport } from '@/shared/redirect-transport';
import type { FirebaseError } from 'firebase/app';
import type { User } from 'firebase/auth';

import { buildBrokerCredential } from '@/shared/broker-credential';
import { setPendingLink } from '@/shared/pending-link';

// Firebase provider id (what the auth cards emit) → broker provider
// path segment. SSO providers already arrive as `oidc.<slug>` and pass
// through unmapped.
export const BROKER_PROVIDER: Record<string, string> = {
  'google.com': 'google',
  'github.com': 'github',
};

// Broker provider path segment → human label for the account-link
// prompt. SSO (`oidc.<slug>`) collapses to a generic "SSO".
const PROVIDER_DISPLAY_NAME: Record<string, string> = {
  google: 'Google',
  github: 'GitHub',
};

function providerDisplayName(provider: string): string {
  if (provider.startsWith('oidc.')) {
    return 'SSO';
  }
  return PROVIDER_DISPLAY_NAME[provider] ?? provider;
}

/**
 * Maps a broker/IdP error code (the `error` field of a failed
 * BrokerRedirectResult) to a user-facing message.
 */
export function brokerErrorMessage(code: string): string {
  switch (code) {
    case 'access_denied':
    case 'popup_closed':
      return 'Sign-in was cancelled.';
    case 'popup_blocked':
      return 'The sign-in window was blocked. Allow pop-ups and try again.';
    case 'auth_timeout':
      return 'Sign-in timed out. Please try again.';
    default:
      return 'Sign-in could not be completed. Please try again.';
  }
}

/**
 * Callbacks `signInViaBroker` invokes as the flow resolves.
 *
 * `onSuccess` is awaitable so consumers can do server-side work
 * (e.g. minting a Firebase session cookie via `createSession`) before
 * the caller moves on / navigates. Sync handlers still satisfy the
 * `void | Promise<void>` union.
 */
export interface BrokerSignInHandlers {
  onSuccess?: (user: User) => void | Promise<void>;
  onLinkRequired?: (email: string) => void;
  setError: (message: string) => void;
}

/**
 * Runs the broker OAuth flow for `provider` through `transport` and
 * signs the user in with the resulting Firebase credential.
 *
 * On a credential collision (`account-exists-with-different-credential`)
 * the pending credential is stashed via `setPendingLink` and
 * `onLinkRequired` is called so the caller can route to the
 * account-link flow — the broker path keeps parity with the legacy
 * popup path here. Any other failure surfaces through `setError`.
 *
 * `provider` is a broker path segment (`google`, `github`, or
 * `oidc.<slug>`); callers map Firebase provider ids via `BROKER_PROVIDER`.
 */
export async function signInViaBroker(
  transport: RedirectTransport,
  input: { provider: string; loginHint?: string; signal?: AbortSignal },
  handlers: BrokerSignInHandlers,
): Promise<void> {
  // Guard the await: runBrokerOAuth normally resolves with a typed
  // result (success or a failure code), but an unexpected throw must
  // still surface to the user rather than escape as a rejection.
  const result = await transport.runBrokerOAuth(input).catch(() => null);
  if (!result) {
    handlers.setError('Sign-in could not be started. Please try again.');
    return;
  }
  if (!result.ok) {
    handlers.setError(brokerErrorMessage(result.error));
    return;
  }

  try {
    const credential = await signInWithCredential(
      getAuth(),
      buildBrokerCredential(result),
    );
    await handlers.onSuccess?.(credential.user);
  } catch (e) {
    const err = e as FirebaseError;
    if (
      err.code === 'auth/account-exists-with-different-credential' &&
      err.customData?.email
    ) {
      const pendingCredential = OAuthProvider.credentialFromError(err);
      if (pendingCredential) {
        const email = err.customData.email as string;
        setPendingLink({
          email,
          credential: pendingCredential,
          providerName: providerDisplayName(input.provider),
        });
        handlers.onLinkRequired?.(email);
        return;
      }
    }
    handlers.setError('Sign-in could not be completed. Please try again.');
  }
}
