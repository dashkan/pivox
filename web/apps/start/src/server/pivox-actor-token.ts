/**
 * SSR-acting-as actor-token minter for server-side prefetch.
 *
 * The Pivox backend's CompositeAuthService routes by JWT `iss`
 * claim: tokens issued by Firebase go to Firebase Admin; tokens
 * signed by an allowlisted SSR service account go to the
 * keyfunc-backed JWT verifier. This module is the server side of
 * that contract — given a verified Pivox identity UUID, it returns
 * a Bearer-shaped JWT that the backend will accept as "the SSR
 * server, acting on behalf of this user."
 *
 * Minimal claim set. The JWT carries `iss`, `aud`, `iat`, `exp`,
 * `actor_uid` — that's it. Profile data (email, displayName,
 * photoURL) is not embedded: handlers that need it join the
 * `identities` row at query time, which avoids JWT/DB drift and
 * keeps the trusted-claim surface small. See the conversation log
 * for the YAGNI / drift / single-source-of-truth tradeoff that
 * landed here.
 *
 * Mints happen via Google's iamcredentials.signJwt API, which the
 * SSR server invokes as itself (the service account hosting this
 * process). Production: GCP service account on the runtime
 * (Cloud Run, GKE, etc.). Dev: gcloud ADC.
 */

import { GoogleAuth } from 'google-auth-library';

/**
 * A function that returns a Bearer-shaped JWT for the given Pivox
 * user UUID, with built-in cache + concurrent-mint dedupe.
 */
export type ActorTokenSource = (pivoxUserId: string) => Promise<string>;

/**
 * Low-level minter: mints one JWT, no caching. Tests stub this to
 * exercise the cache layer without touching GCP. Production wires
 * it via createGcpActorTokenMint.
 */
export type ActorTokenMint = (
  pivoxUserId: string,
) => Promise<{ token: string; expiresAt: number }>;

/**
 * Default token lifetime. iamcredentials.signJwt accepts up to 12h;
 * 1h matches Firebase ID token lifetime, which keeps the operational
 * mental model consistent across both auth surfaces.
 */
const DEFAULT_LIFETIME_SECONDS = 60 * 60;

/**
 * Refresh threshold: re-mint when the cached token has less than
 * this many milliseconds left. Avoids handing out a token that
 * expires mid-flight on the backend.
 */
const REFRESH_HEADROOM_MS = 60_000;

/**
 * createActorTokenSource wraps a low-level mint with an in-memory
 * per-uid cache and a Promise-dedupe layer. Concurrent calls for
 * the same uid during a cache miss share one mint round-trip.
 *
 * Cache is process-local + unbounded. The expected operator
 * population per Pivox SSR process is O(10–10k) active users —
 * each entry ~250 bytes (uid + token + expiresAt + Map overhead),
 * so even 100k users is ~25 MB. If deployments grow beyond that,
 * wrap in an LRU. Not done now: YAGNI + measurable when we hit it.
 */
export function createActorTokenSource(
  mint: ActorTokenMint,
): ActorTokenSource {
  const cache = new Map<string, { token: string; expiresAt: number }>();
  const inflight = new Map<string, Promise<string>>();

  return function getActorToken(uid: string): Promise<string> {
    const now = Date.now();
    const cached = cache.get(uid);
    if (cached && cached.expiresAt > now + REFRESH_HEADROOM_MS) {
      return Promise.resolve(cached.token);
    }

    const existing = inflight.get(uid);
    if (existing) return existing;

    // Set the inflight entry BEFORE we start awaiting on mint().
    // In Node's single-threaded model the bare-IIFE shape was safe
    // by accident (synchronous code runs to completion before any
    // microtask), but `.then()` chaining + pre-registration makes
    // the ordering obviously correct without relying on that
    // implicit guarantee.
    const promise = mint(uid)
      .then((result) => {
        cache.set(uid, result);
        return result.token;
      })
      .finally(() => {
        // Clear the inflight entry on both success and failure so
        // a transient mint failure doesn't pin a rejected Promise
        // that future callers would await forever.
        inflight.delete(uid);
      });

    inflight.set(uid, promise);
    return promise;
  };
}

/**
 * Configuration for the production GCP-backed minter.
 *
 * `serviceAccountEmail` is the SA hosting this process — the SSR
 * server's own identity, NOT the user's. Tokens carry it as `iss`,
 * and the backend's allowlist (PIVOX_SSR_ALLOWED_SERVICE_ACCOUNTS)
 * must contain this email or every token will be rejected at the
 * issuer-allowlist check.
 *
 * `audience` is the backend's expected `aud` claim. Matches
 * PIVOX_SSR_AUDIENCE on the backend.
 */
export interface GcpActorTokenMintConfig {
  serviceAccountEmail: string;
  audience: string;
  /** Override default token lifetime (seconds). Default 3600. */
  lifetimeSeconds?: number;
}

/**
 * createGcpActorTokenMint builds the production ActorTokenMint
 * that calls Google's iamcredentials.signJwt API.
 *
 * Auth resolution: GoogleAuth follows ADC — service-account JSON
 * from GOOGLE_APPLICATION_CREDENTIALS, metadata server on Cloud Run
 * / GKE, gcloud user identity for local dev. The runtime SA must
 * have `iam.serviceAccountTokenCreator` on the SA it's signing as
 * (typically itself).
 *
 * Throws synchronously on missing config; mint errors surface from
 * the returned Promise so cache-layer callers can retry.
 */
export function createGcpActorTokenMint(
  cfg: GcpActorTokenMintConfig,
): ActorTokenMint {
  if (!cfg.serviceAccountEmail) {
    throw new Error('pivox-actor-token: serviceAccountEmail is required');
  }
  if (!cfg.audience) {
    throw new Error('pivox-actor-token: audience is required');
  }
  const lifetimeSeconds = cfg.lifetimeSeconds ?? DEFAULT_LIFETIME_SECONDS;

  // Narrow scope to IAM. Broader cloud-platform would work but
  // least-privilege says only ask for what signJwt needs. (On
  // Cloud Run / GKE metadata server returns project-default scopes
  // and ignores this; the option only takes effect when ADC
  // resolves to a SA key file via GOOGLE_APPLICATION_CREDENTIALS.)
  const auth = new GoogleAuth({
    scopes: ['https://www.googleapis.com/auth/iam'],
  });

  const signJwtURL =
    'https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/' +
    encodeURIComponent(cfg.serviceAccountEmail) +
    ':signJwt';

  return async function mint(pivoxUserId: string) {
    const iat = Math.floor(Date.now() / 1000);
    const exp = iat + lifetimeSeconds;

    const client = await auth.getClient();
    const res = await client.request<{ signedJwt: string }>({
      method: 'POST',
      url: signJwtURL,
      // iamcredentials.signJwt: payload is a JSON STRING (not an
      // object). The API signs it verbatim, embedding the kid of
      // the SA's current signing key in the JWT header so the
      // backend's keyfunc-based verifier can look it up.
      data: {
        payload: JSON.stringify({
          iss: cfg.serviceAccountEmail,
          aud: cfg.audience,
          iat,
          exp,
          actor_uid: pivoxUserId,
        }),
      },
    });

    if (!res.data.signedJwt) {
      throw new Error('pivox-actor-token: signJwt returned empty signedJwt');
    }

    return {
      token: res.data.signedJwt,
      // Convert exp (seconds since epoch) to ms since epoch for the
      // cache layer's Date.now() comparisons.
      expiresAt: exp * 1000,
    };
  };
}
