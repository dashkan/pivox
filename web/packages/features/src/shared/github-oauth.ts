import {
  GithubAuthProvider,
  getAuth,
  signInWithCredential,
} from 'firebase/auth';

import type { UserCredential } from 'firebase/auth';

// Manual GitHub OAuth flow. Firebase's built-in `signInWithPopup` with
// `GithubAuthProvider` requires GitHub OAuth client credentials to be
// configured on the Firebase project — we keep them on our own broker
// instead so the same client_id/secret can serve native + web. The
// broker (web/apps/start /api/oauth/github.com/*) performs the
// authorization-code exchange server-side and posts the access token
// back via window.opener.postMessage from /auth/github-complete.
//
// Wire shape (must match callback.ts + github-complete.tsx):
//   window.postMessage({
//     type: 'pivox:github-auth',
//     access_token?: string,
//     error?: string,
//     error_description?: string,
//   }, location.origin)
//
// On success we build a Firebase credential from the access token and
// call signInWithCredential — this lets the rest of the auth code path
// (account-linking, useAuthState listeners, etc.) treat GitHub
// identically to Google/Apple popup sign-in.

interface GitHubAuthMessage {
  type: 'pivox:github-auth';
  access_token?: string;
  error?: string;
  error_description?: string;
}

const POPUP_NAME = 'pivox-github-oauth';
const POPUP_FEATURES = 'width=600,height=720,menubar=no,toolbar=no';

export async function signInWithGitHubPopup(): Promise<UserCredential> {
  const origin = window.location.origin;
  const returnUrl = `${origin}/auth/github-complete`;
  const startUrl = `${origin}/api/oauth/github.com/start?return=${encodeURIComponent(returnUrl)}`;

  const popup = window.open(startUrl, POPUP_NAME, POPUP_FEATURES);
  if (!popup) {
    throw makeAuthError('auth/popup-blocked', 'Sign-in popup was blocked');
  }

  const message = await waitForMessage(popup, origin);

  if (message.error) {
    // User denied / scope rejected at GitHub. Map to the same code
    // the rest of the UI suppresses so we don't surface a redundant
    // "you cancelled" toast.
    if (message.error === 'access_denied') {
      throw makeAuthError(
        'auth/popup-closed-by-user',
        'Sign-in popup was closed',
      );
    }
    throw makeAuthError(
      'auth/internal-error',
      message.error_description || message.error,
    );
  }

  if (!message.access_token) {
    throw makeAuthError(
      'auth/internal-error',
      'GitHub OAuth callback returned no token',
    );
  }

  const credential = GithubAuthProvider.credential(message.access_token);
  // Firebase throws auth/account-exists-with-different-credential here
  // when the email is already linked to another provider — callers
  // catch that code explicitly and route to the link-account flow.
  return signInWithCredential(getAuth(), credential);
}

function waitForMessage(
  popup: Window,
  expectedOrigin: string,
): Promise<GitHubAuthMessage> {
  return new Promise((resolve, reject) => {
    let settled = false;

    const onMessage = (event: MessageEvent) => {
      if (event.origin !== expectedOrigin) return;
      // event.data is typed `any` by the DOM lib; narrow defensively
      // before trusting it. Same-origin postMessage can come from any
      // script — we only want our broker's payload.
      const data: unknown = event.data;
      if (
        !data ||
        typeof data !== 'object' ||
        !('type' in data) ||
        data.type !== 'pivox:github-auth'
      ) {
        return;
      }
      cleanup();
      resolve(data as GitHubAuthMessage);
    };

    const pollClosed = window.setInterval(() => {
      if (popup.closed) {
        cleanup();
        reject(
          makeAuthError(
            'auth/popup-closed-by-user',
            'Sign-in popup was closed',
          ),
        );
      }
    }, 400);

    function cleanup() {
      if (settled) return;
      settled = true;
      window.removeEventListener('message', onMessage);
      window.clearInterval(pollClosed);
    }

    window.addEventListener('message', onMessage);
  });
}

function makeAuthError(code: string, message: string): Error {
  // Shape mirrors FirebaseError just enough for `firebaseErrorMessage`
  // and the calling `socialLogin` handlers to route on `.code`.
  const err = new Error(message) as Error & { code: string; name: string };
  err.code = code;
  err.name = 'FirebaseError';
  return err;
}
