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
 * Calls the Pivox internal sync endpoint to upsert a identity row.
 * Throws on failure so blocking functions reject the auth operation.
 */
async function syncIdentity(
  firebaseUid: string,
  fields: {
    email: string;
    email_verified: boolean;
    display_name: string;
    photo_url: string;
    disabled: boolean;
  },
): Promise<{ identityId: string }> {
  const url = `${pivoxApiUrl.value()}/internal/v1/auth:syncIdentity`;
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
    logger.error("Failed to sync identity", {
      status: res.status,
      body,
      firebaseUid,
    });
    throw new HttpsError("internal", "Failed to sync identity");
  }

  const data = (await res.json()) as { identity_id: string };
  logger.info("Identity synced", {
    firebaseUid,
    identityId: data.identity_id,
  });
  return { identityId: data.identity_id };
}

/**
 * Blocks user creation until the identity is synced to Pivox.
 * If the API is unreachable or returns an error, user creation fails.
 *
 * Sets the `pivox_user_id` custom claim on the issued token so the
 * Pivox API can read the per-Pivox user UUID directly from the token
 * without an extra lookup. This is the universal user identifier
 * referenced by `org_members.principal_id`,
 * `space_members.principal_id`, and `group_members.user_id` — every
 * authenticated request carries it.
 */
export const syncIdentityOnCreate = beforeUserCreated(async (event) => {
  const user = event.data!;

  const { identityId } = await syncIdentity(user.uid, {
    email: user.email ?? "",
    email_verified: user.emailVerified ?? false,
    display_name: user.displayName ?? "",
    photo_url: user.photoURL ?? "",
    disabled: user.disabled ?? false,
  });

  return {
    customClaims: { pivox_user_id: identityId },
  };
});

/**
 * Syncs identity fields on every sign-in. Catches up on any
 * changes made in Firebase (email, display name, photo, disabled, etc.)
 * since the last sync.
 *
 * Re-stamps the `pivox_user_id` custom claim on every token issuance.
 * Setting it here as well as on create means a token issued before
 * the claim was added (e.g. tokens cached on a client that signed
 * up before this code shipped) gets the claim on its next refresh
 * — no manual re-auth needed.
 */
export const syncIdentityOnSignIn = beforeUserSignedIn(async (event) => {
  const user = event.data!;

  const { identityId } = await syncIdentity(user.uid, {
    email: user.email ?? "",
    email_verified: user.emailVerified ?? false,
    display_name: user.displayName ?? "",
    photo_url: user.photoURL ?? "",
    disabled: user.disabled ?? false,
  });

  return {
    customClaims: { pivox_user_id: identityId },
  };
});

// GitHub OAuth callback: removed.
// Replaced by the provider-agnostic broker in the start app at
// `web/apps/start/src/routes/api/oauth/$provider/{start,callback}.ts`.
// See that file for the current flow. Firebase blocking functions
// (beforeUserCreated / beforeUserSignedIn above) stay here — those
// are Firebase-internal triggers unrelated to OAuth plumbing.
