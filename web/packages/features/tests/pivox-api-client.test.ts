import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  __resetAuthReadyForTests,
  createPivoxApiClient,
} from '@/shared/pivox-api-client';

// Module-level mock state for firebase/auth. Tests drive the
// authStateCallback to simulate Firebase resolving (or not resolving)
// its persisted user before the first API call.
let mockCurrentUser: { getIdToken: () => Promise<string> } | null = null;
let authStateCallback: ((user: unknown) => void) | null = null;
let onAuthStateChangedCalls = 0;

vi.mock('firebase/auth', () => ({
  getAuth: () => ({
    get currentUser() {
      return mockCurrentUser;
    },
  }),
  onAuthStateChanged: (
    _auth: unknown,
    cb: (user: unknown) => void,
  ): (() => void) => {
    onAuthStateChangedCalls += 1;
    authStateCallback = cb;
    return () => {
      authStateCallback = null;
    };
  },
}));

describe('createPivoxApiClient', () => {
  beforeEach(() => {
    mockCurrentUser = null;
    authStateCallback = null;
    onAuthStateChangedCalls = 0;
    __resetAuthReadyForTests();
  });

  afterEach(() => {
    __resetAuthReadyForTests();
  });

  it('waits for the first onAuthStateChanged event before reading currentUser', async () => {
    // Build a recording fetch so we can inspect the outgoing request's
    // Authorization header without making a network call.
    let capturedAuth: string | null = null;
    const fetchStub = vi.fn((input: Request | string) => {
      if (input instanceof Request) {
        capturedAuth = input.headers.get('Authorization');
      }
      return Promise.resolve(
        new Response('{}', {
          status: 200,
          headers: { 'content-type': 'application/json' },
        }),
      );
    });

    // openapi-fetch captures `globalThis.fetch` at client
    // construction, so the stub must be installed BEFORE
    // createPivoxApiClient runs.
    const originalFetch = globalThis.fetch;
    globalThis.fetch = fetchStub as typeof fetch;
    const client = createPivoxApiClient({ baseUrl: 'https://api.test' });

    try {
      // Kick off the request BEFORE Firebase resolves its initial
      // state. With the fix in place, the middleware's getAuthToken
      // awaits waitForAuthReady, which is waiting on the callback.
      const requestPromise = client.GET('/v1/accounts/me/organizations', {
        params: { path: { parent: 'accounts/me' } },
      });

      // Give the middleware a tick to enter the await on
      // waitForAuthReady. If we resolved the callback synchronously
      // the test would still pass — but the realistic race is async,
      // so simulate that.
      await new Promise((resolve) => setTimeout(resolve, 0));

      // Firebase NOW resolves its persisted user. Set currentUser and
      // fire the auth-state-changed callback.
      mockCurrentUser = {
        getIdToken: () => Promise.resolve('fresh-id-token'),
      };
      authStateCallback?.(mockCurrentUser);

      await requestPromise;
    } finally {
      globalThis.fetch = originalFetch;
    }

    // The captured Authorization header must reflect the
    // post-Firebase-ready token. Without the fix, getAuthToken would
    // have read currentUser=null before the callback fired and the
    // header would be absent.
    expect(capturedAuth).toBe('Bearer fresh-id-token');
  });

  it('subscribes to onAuthStateChanged exactly once across multiple requests', async () => {
    // Module-level cache means the Promise is built once and reused.
    // Two consecutive requests should not register two listeners.
    mockCurrentUser = { getIdToken: () => Promise.resolve('t') };
    const fetchStub = vi.fn(() =>
      Promise.resolve(new Response('{}', { status: 200 })),
    );

    const originalFetch = globalThis.fetch;
    globalThis.fetch = fetchStub;
    const client = createPivoxApiClient({ baseUrl: 'https://api.test' });

    try {
      // Resolve the auth state immediately so both requests can
      // complete. Caller schedules the callback after the first
      // microtask so the first request enters the await.
      queueMicrotask(() => authStateCallback?.(mockCurrentUser));

      await client.GET('/v1/accounts/me/organizations', {
        params: { path: { parent: 'accounts/me' } },
      });
      await client.GET('/v1/accounts/me/organizations', {
        params: { path: { parent: 'accounts/me' } },
      });
    } finally {
      globalThis.fetch = originalFetch;
    }

    expect(onAuthStateChangedCalls).toBe(1);
  });

  it('omits Authorization header when no user is signed in after auth-ready', async () => {
    // Genuinely signed-out: Firebase fires the callback with null.
    // Optional chaining on `currentUser?.getIdToken()` yields
    // undefined, middleware doesn't set the header, request goes
    // out unauthenticated. Gateway answers 401 — caller's problem,
    // not the client's.
    let capturedAuth: string | null = 'sentinel';
    const fetchStub = vi.fn((input: Request | string) => {
      if (input instanceof Request) {
        capturedAuth = input.headers.get('Authorization');
      }
      return Promise.resolve(
        new Response('{}', {
          status: 200,
          headers: { 'content-type': 'application/json' },
        }),
      );
    });

    const originalFetch = globalThis.fetch;
    globalThis.fetch = fetchStub as typeof fetch;
    const client = createPivoxApiClient({ baseUrl: 'https://api.test' });

    try {
      const requestPromise = client.GET('/v1/accounts/me/organizations', {
        params: { path: { parent: 'accounts/me' } },
      });

      await new Promise((resolve) => setTimeout(resolve, 0));

      // No user — fire callback with null.
      mockCurrentUser = null;
      authStateCallback?.(null);

      await requestPromise;
    } finally {
      globalThis.fetch = originalFetch;
    }

    expect(capturedAuth).toBeNull();
  });
});
