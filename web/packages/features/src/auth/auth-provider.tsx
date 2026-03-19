'use client';

import { useCallback, useEffect, useState } from 'react';
import { AuthContext } from './use-auth';
import type { User } from 'firebase/auth';

// Ensures the startup reload only runs once per page load, not on every
// AuthProvider mount (e.g., React Strict Mode double-mount).
let didReloadOnStartup = false;

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let unsubscribe: (() => void) | undefined;

    import('firebase/auth').then(({ getAuth, onIdTokenChanged }) => {
      const auth = getAuth();
      // onIdTokenChanged fires on sign-in, sign-out, and token refresh
      // (covers more cases than onAuthStateChanged)
      unsubscribe = onIdTokenChanged(auth, (firebaseUser) => {
        setUser(firebaseUser);
        setLoading(false);

        // On app startup, reload the user from the server to pick up
        // changes made on other clients (e.g., provider linked/unlinked
        // on the web while Electron was closed). After reload(), we must
        // re-read auth.currentUser to get a fresh object — the old
        // reference keeps stale providerData.
        if (!didReloadOnStartup && firebaseUser) {
          didReloadOnStartup = true;
          firebaseUser.reload().then(() => {
            setUser(auth.currentUser);
          }).catch(() => {
            // Reload failed (offline, etc.) — stale data is better than
            // no data, so silently continue.
          });
        }
      });
    });

    return () => unsubscribe?.();
  }, []);

  const refreshUser = useCallback(async () => {
    const { getAuth } = await import('firebase/auth');
    const auth = getAuth();
    if (auth.currentUser) {
      await auth.currentUser.reload();
      // Re-read currentUser after reload — the old reference keeps stale
      // providerData even though reload() updates the internal auth state.
      setUser(auth.currentUser);
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
