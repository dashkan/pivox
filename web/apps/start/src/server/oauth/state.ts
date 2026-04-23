import crypto from 'node:crypto'

/**
 * HMAC-signed `state` parameter for the OAuth flow.
 *
 * What we pack into state:
 *   - `n` (nonce): random per-request, binds the authorize request
 *     to the eventual callback so we can reject replays.
 *   - `r` (return): where to redirect the user after successful auth.
 *     For native: `pivox://...`. For web: a same-origin path.
 *   - `p` (provider): provider id, so the callback route knows what
 *     to verify against even if the URL is tampered with.
 *   - `t` (issued at): seconds-since-epoch, for TTL enforcement.
 *
 * We don't store anything server-side — state is stateless, signed
 * with a server-only HMAC key. Rotating the key invalidates all
 * in-flight flows, which is acceptable (user just retries).
 *
 * Format: `<base64url(payload)>.<base64url(HMAC-SHA256(key, payload))>`.
 * The same scheme is used by JWS HS256 but we skip the JOSE envelope
 * because we control both ends.
 */

export interface StatePayload {
  n: string
  r: string
  p: string
  t: number
}

const ALGO = 'sha256'
const MAX_AGE_SECONDS = 10 * 60 // 10 minutes — enough for slow sign-in

/**
 * HMAC signing key derived from the shared app secret.
 *
 * We don't use `PIVOX_APP_KEY` directly — raw reuse of one key for
 * multiple cryptographic purposes (encryption AND signing AND ...)
 * weakens security proofs and creates subtle cross-protocol attack
 * surface. Instead we do standard key-separation via HKDF-style
 * derivation: `subkey = HMAC(app_key, "oauth-state")`. Each distinct
 * use of `PIVOX_APP_KEY` in the codebase should pass its own unique
 * label string so the derived keys are independent.
 */
function signingKey(): Buffer {
  const appKey = process.env.PIVOX_APP_KEY
  if (!appKey || appKey.length < 32) {
    throw new Error(
      'PIVOX_APP_KEY is missing or < 32 chars. Set it in .envrc.'
    )
  }
  return crypto.createHmac(ALGO, appKey).update('oauth-state').digest()
}

export function signState(payload: Omit<StatePayload, 'n' | 't'>): string {
  const full: StatePayload = {
    ...payload,
    n: crypto.randomBytes(16).toString('base64url'),
    t: Math.floor(Date.now() / 1000),
  }
  const body = Buffer.from(JSON.stringify(full)).toString('base64url')
  const mac = crypto.createHmac(ALGO, signingKey()).update(body).digest('base64url')
  return `${body}.${mac}`
}

export function verifyState(token: string): StatePayload | null {
  const parts = token.split('.')
  if (parts.length !== 2) return null
  const [body, mac] = parts

  // Constant-time compare to avoid timing oracles.
  const expected = crypto.createHmac(ALGO, signingKey()).update(body).digest('base64url')
  const macBuf = Buffer.from(mac, 'utf8')
  const expectedBuf = Buffer.from(expected, 'utf8')
  if (macBuf.length !== expectedBuf.length) return null
  if (!crypto.timingSafeEqual(macBuf, expectedBuf)) return null

  let parsed: StatePayload
  try {
    parsed = JSON.parse(Buffer.from(body, 'base64url').toString('utf8')) as StatePayload
  } catch {
    return null
  }

  if (typeof parsed.t !== 'number') return null
  const age = Math.floor(Date.now() / 1000) - parsed.t
  if (age < 0 || age > MAX_AGE_SECONDS) return null

  return parsed
}
