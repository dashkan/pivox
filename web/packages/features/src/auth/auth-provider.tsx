'use client';

import { useCallback, useEffect, useReducer, useState } from 'react';
import { AuthContext } from './use-auth';
import type { User } from 'firebase/auth';

/**
 * Workaround for Firebase SDK bug: `user.reload()` calls an internal
 * `mergeProviderData()` that merges old + new providers instead of replacing.
 * Unlinked providers are never removed because they're absent from the server
 * response and survive the merge. This function patches providerData after
 * reload() by fetching the truth from the REST API and replacing the stale array.
 *
 * See: `mergeProviderData()` in @firebase/auth — line ~1545 of the ESM bundle.
 */
async function patchProviderData(user: User): Promise<void> {
  const idToken = await user.getIdToken();
  const apiKey = (user as unknown as { auth: { config: { apiKey: string } } })
    .auth.config.apiKey;
  const res = await fetch(
    `https://identitytoolkit.googleapis.com/v1/accounts:lookup?key=${apiKey}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ idToken }),
    },
  );
  if (!res.ok) return;
  const data = await res.json();
  const serverProviders: Array<{
    providerId: string;
    displayName?: string;
    email?: string;
    photoUrl?: string;
    rawId?: string;
  }> = data.users?.[0]?.providerUserInfo;
  if (!Array.isArray(serverProviders)) return;

  // Build the correct provider list from the server response.
  const serverProviderIds = new Set(serverProviders.map((p) => p.providerId));

  // Mutate the user's providerData in place — remove providers the server
  // doesn't have, preserving the SDK's internal array reference.
  const pd = user.providerData;
  for (let i = pd.length - 1; i >= 0; i--) {
    if (!serverProviderIds.has(pd[i]!.providerId)) {
      pd.splice(i, 1);
    }
  }
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [, forceUpdate] = useReducer((c: number) => c + 1, 0);

  useEffect(() => {
    let unsubscribe: (() => void) | undefined;

    import('firebase/auth').then(({ getAuth, onIdTokenChanged }) => {
      const auth = getAuth();
      unsubscribe = onIdTokenChanged(auth, (firebaseUser) => {
        setUser(firebaseUser);
        setLoading(false);
      });
    });

    return () => unsubscribe?.();
  }, []);

  const refreshUser = useCallback(async () => {
    const { getAuth } = await import('firebase/auth');
    const auth = getAuth();
    if (auth.currentUser) {
      // reload() fetches fresh user properties (displayName, emailVerified, etc.)
      // but its internal mergeProviderData() is buggy — it never removes
      // unlinked providers. patchProviderData() fixes this by pruning
      // providers that the server no longer returns.
      await auth.currentUser.reload();
      await patchProviderData(auth.currentUser);
      setUser(auth.currentUser);
      forceUpdate();
    }
  }, []);

  const signOut = useCallback(async () => {
    const { getAuth, signOut: firebaseSignOut } = await import('firebase/auth');
    const auth = getAuth();
    await firebaseSignOut(auth);
  }, []);

  return (
    <AuthContext value={{ user, loading, signOut, refreshUser }}>
      {children}
    </AuthContext>
  );
}
