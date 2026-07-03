import { describe, expect, it } from 'vitest';

import { decodeIdTokenClaims } from '@/claims';

/** Builds a structurally-valid JWT string (header.payload.signature) whose payload is `claims`. */
function jwt(claims: Record<string, unknown>): string {
  const b64 = (o: unknown) => Buffer.from(JSON.stringify(o)).toString('base64url');
  return `${b64({ alg: 'RS256', typ: 'JWT' })}.${b64(claims)}.sig`;
}

describe('decodeIdTokenClaims', () => {
  it('decodes the payload claims of a well-formed id_token', () => {
    const token = jwt({
      sub: 'user-123',
      email: 'user@acme.test',
      name: 'Ada Lovelace',
      preferred_username: 'ada',
      picture: 'https://cdn/pic.png',
    });
    expect(decodeIdTokenClaims(token)).toEqual({
      sub: 'user-123',
      email: 'user@acme.test',
      name: 'Ada Lovelace',
      preferred_username: 'ada',
      picture: 'https://cdn/pic.png',
    });
  });

  it('returns undefined for a token without exactly three segments', () => {
    expect(decodeIdTokenClaims('only.two')).toBeUndefined();
    expect(decodeIdTokenClaims('a.b.c.d')).toBeUndefined();
    expect(decodeIdTokenClaims('notajwt')).toBeUndefined();
  });

  it('returns undefined when the payload is not base64url JSON', () => {
    expect(decodeIdTokenClaims('h.%%%notbase64%%%.s')).toBeUndefined();
    const nonObject = `h.${Buffer.from('"a string"').toString('base64url')}.s`;
    expect(decodeIdTokenClaims(nonObject)).toBeUndefined();
  });
});
