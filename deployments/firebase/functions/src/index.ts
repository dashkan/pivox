import { HttpsError, onRequest } from "firebase-functions/v2/https";
import { setGlobalOptions } from "firebase-functions";
import {
  beforeUserCreated,
  beforeUserSignedIn,
} from "firebase-functions/v2/identity";
import { logger } from "firebase-functions/v2";
import { defineSecret, defineString } from "firebase-functions/params";

declare const __DEV__: boolean;

setGlobalOptions({ maxInstances: 10 });

const pivoxApiUrl = defineString("PIVOX_API_URL", {
  description: "Base URL of the Pivox API server",
  default: "https://pivox.ngrok.app",
  label: "API URL",
});

/**
 * Returns an Authorization header for calling the Pivox API.
 *
 * Dev mode: returns "Bearer <SHARED_SECRET>" from the environment, matching
 * the Go backend's dev-mode `requireSecret` middleware.
 *
 * Prod mode: mints an OIDC identity token via the Cloud Function's service
 * account, verified by the Go backend against Google's JWKS.
 */
async function getAuthorizationHeader(targetAudience: string): Promise<string> {
  if (__DEV__) {
    const secret = process.env.SHARED_SECRET;
    if (!secret) {
      throw new Error("SHARED_SECRET env var is required in dev mode");
    }
    return `Bearer ${secret}`;
  } else {
    const { GoogleAuth } = await import("google-auth-library");
    const auth = new GoogleAuth();
    const client = await auth.getIdTokenClient(targetAudience);
    const headers = await client.getRequestHeaders();
    const bearer = headers.get("Authorization");
    if (!bearer) {
      throw new Error("Failed to obtain OIDC identity token");
    }
    return bearer;
  }
}

/**
 * Calls the Pivox internal sync endpoint to upsert an account.
 * Throws on failure so blocking functions reject the auth operation.
 */
async function syncAccount(
  firebaseUid: string,
  fields: {
    email: string;
    email_verified: boolean;
    display_name: string;
    photo_url: string;
    disabled: boolean;
  },
): Promise<void> {
  const url = `${pivoxApiUrl.value()}/internal/v1/accounts:sync`;
  const payload = { firebase_uid: firebaseUid, ...fields };

  // The audience for the OIDC token is the base URL of the API server.
  const authorization = await getAuthorizationHeader(pivoxApiUrl.value());

  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: authorization,
    },
    body: JSON.stringify(payload),
  });

  if (!res.ok) {
    const body = await res.text();
    logger.error("Failed to sync account", {
      status: res.status,
      body,
      firebaseUid,
    });
    throw new HttpsError("internal", "Failed to sync account");
  }

  const data = (await res.json()) as { account_id: string };
  logger.info("Account synced", {
    firebaseUid,
    accountId: data.account_id,
  });
}

/**
 * Blocks user creation until the account is synced to Pivox.
 * If the API is unreachable or returns an error, user creation fails.
 */
export const syncAccountOnCreate = beforeUserCreated(async (event) => {
  const user = event.data!;

  await syncAccount(user.uid, {
    email: user.email ?? "",
    email_verified: user.emailVerified ?? false,
    display_name: user.displayName ?? "",
    photo_url: user.photoURL ?? "",
    disabled: user.disabled ?? false,
  });
});

/**
 * Syncs account fields on every sign-in. Catches up on any changes
 * made in Firebase (email, display name, photo, disabled, etc.)
 * since the last sync.
 */
export const syncAccountOnSignIn = beforeUserSignedIn(async (event) => {
  const user = event.data!;

  await syncAccount(user.uid, {
    email: user.email ?? "",
    email_verified: user.emailVerified ?? false,
    display_name: user.displayName ?? "",
    photo_url: user.photoURL ?? "",
    disabled: user.disabled ?? false,
  });
});

// ---------------------------------------------------------------------------
// GitHub OAuth callback
// ---------------------------------------------------------------------------
//
// Single GitHub OAuth App for every Pivox client (macOS, Windows, web).
// The client_secret lives only here — never in a binary or a browser.
//
// Flow:
//   1. Client opens github.com/login/oauth/authorize with
//      redirect_uri = this function and state = "<platform>:<nonce>".
//   2. GitHub redirects the user's browser to this function with
//      ?code&state.
//   3. We exchange the code for an access_token server-side.
//   4. We bounce the user back to a platform-specific URL with the
//      access_token in the query string:
//         native → pivox://oauth/github/callback?access_token=…&state=…
//         web    → <GITHUB_WEB_COMPLETE_URL>?access_token=…&state=…
//      Native's ASWebAuthenticationSession intercepts the custom
//      scheme; web's completion page reads the token and calls
//      Firebase's signInWithCredential in the opener.
//
// Why we abandoned Firebase's built-in web popup: the popup hardcodes
// redirect_uri = firebaseapp.com/__/auth/handler in its authorize
// call, which requires the OAuth App's registered callback to be that
// same URL. One OAuth App can only hold one callback URL — so we
// can't satisfy both web (Firebase handler) and native (our function)
// at the same time. Moving web to the manual flow lets one OAuth App
// serve every platform.

const githubClientID = defineString("GITHUB_CLIENT_ID", {
  description: "GitHub OAuth App client ID",
});

const githubClientSecret = defineSecret("GITHUB_CLIENT_SECRET");

// Set this to the function's own public URL after deploy, e.g.
// https://us-central1-pivox-cloud.cloudfunctions.net/githubOAuthCallback.
// We need the exact same string that's registered on the GitHub OAuth
// App so the redirect_uri sent in the token-exchange POST matches the
// redirect_uri sent in the earlier authorize request.
const githubCallbackURL = defineString("GITHUB_CALLBACK_URL", {
  description: "Public HTTPS URL of this callback function",
});

// Where web-initiated sign-ins land after the exchange. The page at
// this URL reads ?access_token=…&state=… from the query, validates
// state against the opener-generated nonce, and calls
// signInWithCredential(GithubAuthProvider.credential(access_token)).
const githubWebCompleteURL = defineString("GITHUB_WEB_COMPLETE_URL", {
  description: "Web completion URL for GitHub OAuth",
  default: "https://pivox-cloud.web.app/auth/github-complete",
});

interface GitHubTokenResponse {
  access_token?: string;
  error?: string;
  error_description?: string;
}

export const githubOAuthCallback = onRequest(
  {
    secrets: [githubClientSecret],
    maxInstances: 10,
  },
  async (req, res) => {
    if (req.method !== "GET") {
      res.status(405).send("method_not_allowed");
      return;
    }

    const code =
      typeof req.query.code === "string" ? req.query.code : undefined;
    const state =
      typeof req.query.state === "string" ? req.query.state : undefined;
    const upstreamError =
      typeof req.query.error === "string" ? req.query.error : undefined;
    const upstreamErrorDescription =
      typeof req.query.error_description === "string"
        ? req.query.error_description
        : undefined;

    // GitHub returned an error (user declined, invalid app config,
    // etc.). Bounce the error back to the caller so they can show it.
    if (upstreamError) {
      logger.warn("GitHub OAuth returned an error", {
        error: upstreamError,
        description: upstreamErrorDescription,
      });
      sendPlatformRedirect(res, state, {
        error: upstreamError,
        error_description: upstreamErrorDescription ?? upstreamError,
      });
      return;
    }

    if (!code || !state) {
      res.status(400).send("missing_code_or_state");
      return;
    }

    const clientID = githubClientID.value();
    const clientSecret = githubClientSecret.value();
    const callbackURL = githubCallbackURL.value();
    if (!clientID || !clientSecret || !callbackURL) {
      logger.error("GitHub OAuth callback is not fully configured");
      res.status(500).send("server_not_configured");
      return;
    }

    try {
      const githubRes = await fetch(
        "https://github.com/login/oauth/access_token",
        {
          method: "POST",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded",
            Accept: "application/json",
          },
          body: new URLSearchParams({
            client_id: clientID,
            client_secret: clientSecret,
            code,
            redirect_uri: callbackURL,
          }).toString(),
        },
      );

      const data = (await githubRes.json()) as GitHubTokenResponse;

      if (!githubRes.ok || !data.access_token) {
        logger.warn("GitHub token exchange failed", {
          status: githubRes.status,
          error: data.error,
          description: data.error_description,
        });
        sendPlatformRedirect(res, state, {
          error: data.error ?? "exchange_failed",
          error_description: data.error_description ?? "",
        });
        return;
      }

      sendPlatformRedirect(res, state, { access_token: data.access_token });
    } catch (err) {
      logger.error("GitHub token exchange threw", err);
      res.status(502).send("upstream_unavailable");
    }
  },
);

// Minimal slice of the response object we use. Firebase-functions'
// handler signature uses express's Response under the hood; we model
// just what we need rather than pulling @types/express into the
// project.
interface CallbackResponse {
  status(code: number): CallbackResponse;
  send(body: string): void;
  redirect(status: number, url: string): void;
}

// Directs the user's browser from this function back to the
// originating platform. State is always echoed so the caller can
// match the callback to its in-flight nonce (CSRF defence).
function sendPlatformRedirect(
  res: CallbackResponse,
  state: string | undefined,
  fields: Record<string, string>,
): void {
  const platform = (state ?? "").split(":", 1)[0];
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(fields)) {
    if (value.length > 0) params.set(key, value);
  }
  if (state) params.set("state", state);

  if (platform === "native") {
    // ASWebAuthenticationSession intercepts the pivox:// scheme. Some
    // browsers refuse to follow a Location header from HTTPS to a
    // custom scheme, so emit an HTML page whose JS drives the
    // navigation — works everywhere.
    const target = `pivox://oauth/github/callback?${params.toString()}`;
    const escaped = JSON.stringify(target);
    res.status(200).send(
      `<!doctype html><html><head><meta charset="utf-8">` +
        `<title>Returning to Pivox</title>` +
        `<script>location.replace(${escaped});</script>` +
        `</head><body>Returning to Pivox…</body></html>`,
    );
    return;
  }

  if (platform === "web") {
    const base = githubWebCompleteURL.value();
    res.redirect(302, `${base}?${params.toString()}`);
    return;
  }

  logger.warn("Unknown state platform prefix; refusing to redirect", { state });
  res.status(400).send("invalid_state_platform");
}
