'use client';

import { useCallback, useEffect, useReducer, useState } from 'react';
import { AuthContext } from './use-auth';
import type { User } from 'firebase/auth';

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  // Incrementing a counter forces a re-render while keeping the real User reference
  const [, forceUpdate] = useReducer((c: number) => c + 1, 0);

  useEffect(() => {
    let unsubscribe: (() => void) | undefined;

    import('firebase/auth').then(({ getAuth, onIdTokenChanged }) => {
      const auth = getAuth();
      // onIdTokenChanged fires on sign-in, sign-out, and token refresh
      // (covers more cases than onAuthStateChanged)
      unsubscribe = onIdTokenChanged(auth, (firebaseUser) => {
        setUser(firebaseUser);
        setLoading(false);
      });
    });

    return () => unsubscribe?.();
  }, []);

  const refreshUser = useCallback(async () => {
    const { getAuth } = await import('firebase/auth');
    const currentUser = getAuth().currentUser;
    if (currentUser) {
      await currentUser.reload();
      // Keep the real User object (with all methods intact) and
      // force a re-render so consumers pick up the updated properties
      setUser(currentUser);
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
