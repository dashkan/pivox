/** Claims consumed from the Keycloak id_token (decoded, not signature-verified). */
export interface OidcClaims {
  sub?: string;
  email?: string;
  email_verified?: boolean;
  name?: string;
  preferred_username?: string;
  picture?: string;
}

/**
 * Decodes a JWT's payload claims WITHOUT verifying the signature. Returns
 * undefined for a structurally invalid token (not three segments, or a payload
 * that isn't base64url-encoded JSON).
 *
 * Decode-for-display only: the token was verified by Keycloak at issuance and is
 * re-verified by the Cloud Controller on every API call (authorization uses the
 * access token, not this). Uses Node's `Buffer` — this package targets Node (the
 * `start` server and the Electron main process), never the browser.
 */
export function decodeIdTokenClaims(idToken: string): OidcClaims | undefined {
  const parts = idToken.split('.');
  if (parts.length !== 3) return undefined;
  const payload = parts[1];
  if (payload === undefined) return undefined;
  try {
    const json = Buffer.from(payload, 'base64url').toString('utf8');
    const claims: unknown = JSON.parse(json);
    if (typeof claims !== 'object' || claims === null) return undefined;
    return claims;
  } catch {
    return undefined;
  }
}
