import { HttpsError } from "firebase-functions/v2/https";
import { setGlobalOptions } from "firebase-functions";
import {
  beforeUserCreated,
  beforeUserSignedIn,
} from "firebase-functions/v2/identity";
import { logger } from "firebase-functions/v2";
import { defineString } from "firebase-functions/params";

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
