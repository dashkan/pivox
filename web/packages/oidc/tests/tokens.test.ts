import { afterEach, describe, expect, it, vi } from 'vitest';

import { EXPIRY_SKEW_MS, isTokenFresh, tokensFromResponse } from '@/tokens';

import type { TokenEndpointResponse } from 'openid-client';

// tokensFromResponse maps an openid-client token response into our JSON-safe
// stored shape and stamps an absolute expiry from the relative expires_in.
// isTokenFresh is the refresh-skew predicate both stores gate refresh on.

describe('tokensFromResponse', () => {
  it('maps access/refresh/id tokens and computes absolute expiry', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-01-01T00:00:00.000Z'));
    try {
      const response = {
        access_token: 'at',
        refresh_token: 'rt',
        id_token: 'idt',
        expires_in: 300,
      } as unknown as TokenEndpointResponse;

      const tokens = tokensFromResponse(response);

      expect(tokens.access_token).toBe('at');
      expect(tokens.refresh_token).toBe('rt');
      expect(tokens.id_token).toBe('idt');
      expect(tokens.expires_at).toBe(Date.now() + 300 * 1000);
    } finally {
      vi.useRealTimers();
    }
  });

  it('defaults expires_in to 300s when the response omits it', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-01-01T00:00:00.000Z'));
    try {
      const tokens = tokensFromResponse({ access_token: 'at' } as unknown as TokenEndpointResponse);
      expect(tokens.expires_at).toBe(Date.now() + 300 * 1000);
      expect(tokens.refresh_token).toBeUndefined();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('isTokenFresh', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('is true when the access token outlives the skew window', () => {
    expect(isTokenFresh({ expires_at: Date.now() + EXPIRY_SKEW_MS + 1_000 })).toBe(true);
  });

  it('is false when the access token is within the skew window', () => {
    expect(isTokenFresh({ expires_at: Date.now() + 5_000 })).toBe(false);
  });

  it('honours a custom skew', () => {
    const expires_at = Date.now() + 45_000;
    expect(isTokenFresh({ expires_at }, 30_000)).toBe(true);
    expect(isTokenFresh({ expires_at }, 60_000)).toBe(false);
  });
});
