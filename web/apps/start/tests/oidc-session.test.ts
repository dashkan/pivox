import { describe, expect, it } from 'vitest';

import { decodeIdTokenClaims, toServerSession } from '../src/server/oidc-session';

/** Build an unsigned-but-well-formed JWT (header.payload.sig) for decode tests. */
function makeIdToken(claims: Record<string, unknown>): string {
  const seg = (o: unknown) =>
    Buffer.from(JSON.stringify(o)).toString('base64url');
  return `${seg({ alg: 'RS256', typ: 'JWT' })}.${seg(claims)}.signature`;
}

describe('decodeIdTokenClaims', () => {
  it('decodes the JWT payload claims', () => {
    const token = makeIdToken({
      sub: 'kc-sub-123',
      email: 'ada@example.com',
      email_verified: true,
      name: 'Ada',
    });
    expect(decodeIdTokenClaims(token)).toMatchObject({
      sub: 'kc-sub-123',
      email: 'ada@example.com',
      email_verified: true,
      name: 'Ada',
    });
  });

  it('returns undefined when the token is not three segments', () => {
    expect(decodeIdTokenClaims('not-a-jwt')).toBeUndefined();
    expect(decodeIdTokenClaims('only.two')).toBeUndefined();
  });

  it('returns undefined when the payload is not valid JSON', () => {
    // "aGVsbG8" is base64url for "hello" — decodes fine, but JSON.parse fails.
    expect(decodeIdTokenClaims('header.aGVsbG8.sig')).toBeUndefined();
  });
});

describe('toServerSession', () => {
  it('maps claims to the session shape with sub as the single id', () => {
    expect(
      toServerSession({
        sub: 'kc-sub-123',
        email: 'ada@example.com',
        email_verified: true,
        name: 'Ada Lovelace',
        picture: 'https://example.com/p.png',
      }),
    ).toEqual({
      id: 'kc-sub-123',
      email: 'ada@example.com',
      displayName: 'Ada Lovelace',
      photoURL: 'https://example.com/p.png',
    });
  });

  it('falls back displayName to preferred_username and nulls missing fields', () => {
    expect(toServerSession({ sub: 's', preferred_username: 'ada' })).toEqual({
      id: 's',
      email: null,
      displayName: 'ada',
      photoURL: null,
    });
  });
});
