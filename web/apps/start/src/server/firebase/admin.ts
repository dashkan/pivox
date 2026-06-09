import fs from 'node:fs';

import { cert, getApps, initializeApp } from 'firebase-admin/app';
import { getAuth } from 'firebase-admin/auth';

import type { App } from 'firebase-admin/app';

/**
 * Firebase Admin singleton for server routes.
 *
 * Init strategy:
 *   - `GOOGLE_APPLICATION_CREDENTIALS` (path to a service account
 *     JSON) — standard Google ADC env var, set by operators for
 *     local dev and CI.
 *   - If unset and we're on GCP (Cloud Run / GKE / App Engine),
 *     `initializeApp()` without args uses workload identity / the
 *     metadata server.
 *
 * Used by the OAuth callback to mint a Firebase custom token from a
 * provider-issued credential. Native/web then call
 * `signInWithCustomToken()` to complete the sign-in.
 */

let cached: App | undefined;

export function firebaseAdmin(): App {
  if (cached) return cached;
  // getApps()[0] is typed `App` (this tsconfig has no noUncheckedIndexedAccess);
  // the `.length > 0` check is the real runtime guard against an empty array.
  if (getApps().length > 0) {
    cached = getApps()[0];
    return cached;
  }

  const saPath = process.env.GOOGLE_APPLICATION_CREDENTIALS;

  if (saPath && fs.existsSync(saPath)) {
    const json = JSON.parse(fs.readFileSync(saPath, 'utf8')) as {
      project_id: string;
      client_email: string;
      private_key: string;
    };
    cached = initializeApp({
      credential: cert({
        projectId: json.project_id,
        clientEmail: json.client_email,
        privateKey: json.private_key,
      }),
      projectId: json.project_id,
    });
    return cached;
  }

  // Application Default Credentials path. On GCP this uses workload
  // identity / the metadata server. Locally it uses your
  // `gcloud auth application-default` credentials — which carry no
  // private key, so `createCustomToken` (it self-signs the JWT) can't
  // sign directly and throws "Failed to determine service account".
  // (`createSessionCookie` is unaffected — it mints via the Firebase
  // backend over ADC, no local signing.) Passing `serviceAccountId`
  // makes the Admin SDK sign custom tokens via the IAM Credentials API
  // AS that SA — the same SA the SSR actor-token path already signs as
  // (`PIVOX_SSR_SA_EMAIL`), whose Token Creator grant your ADC already
  // holds. So session-recovery custom-token minting works in dev too,
  // not just on GCP.
  const serviceAccountId = process.env.PIVOX_SSR_SA_EMAIL;
  cached = initializeApp(serviceAccountId ? { serviceAccountId } : undefined);
  return cached;
}

/**
 * Mints a short-lived Firebase custom token for the given UID.
 * Native/web then exchange it for a session via
 * `signInWithCustomToken()`. The token carries additional claims
 * the backend can read via the Firebase Auth session (e.g.
 * `provider` for audit/analytics).
 */
export async function mintCustomToken(
  uid: string,
  claims: Record<string, unknown> = {},
) {
  return getAuth(firebaseAdmin()).createCustomToken(uid, claims);
}
