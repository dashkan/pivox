import {
  GithubAuthProvider,
  GoogleAuthProvider,
  OAuthProvider,
} from 'firebase/auth';

import type { BrokerRedirectResult } from '@/shared/redirect-transport';
import type { OAuthCredential } from 'firebase/auth';

type BrokerSuccess = Extract<BrokerRedirectResult, { ok: true }>;

/**
 * Builds the Firebase credential for a successful broker result. The
 * `kind` decides the provider:
 *   - github_access_token → GithubAuthProvider (OAuth access token)
 *   - google_id_token     → GoogleAuthProvider (id_token + access token)
 *   - oidc_id_token       → OAuthProvider(provider) (id_token + rawNonce)
 *
 * The caller feeds the returned credential straight into
 * signInWithCredential / linkWithCredential.
 */
export function buildBrokerCredential(result: BrokerSuccess): OAuthCredential {
  switch (result.kind) {
    case 'github_access_token':
      return GithubAuthProvider.credential(result.token);
    case 'google_id_token':
      return GoogleAuthProvider.credential(result.token, result.accessToken);
    case 'oidc_id_token':
      // Firebase recomputes sha256(rawNonce) and compares it to the
      // id_token's nonce claim — the broker only sends a nonce for
      // oidc_id_token, so it is present here.
      return new OAuthProvider(result.provider).credential({
        idToken: result.token,
        ...(result.nonce ? { rawNonce: result.nonce } : {}),
      });
    default: {
      // Exhaustiveness guard: adding a BrokerCredentialKind without a
      // case here stops compiling (`result.kind` is no longer `never`).
      const exhaustive: never = result.kind;
      throw new Error(
        `buildBrokerCredential: unhandled kind ${String(exhaustive)}`,
      );
    }
  }
}
