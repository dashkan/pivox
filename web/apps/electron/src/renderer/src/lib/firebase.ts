import { ensureFirebaseApp } from '@pivox/features/auth';

const firebaseConfig = {
  apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
  authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
  projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
};

// Shared hardened init (explicit IndexedDB/localStorage persistence +
// storage.persist) lives in @pivox/features/auth so the renderer can't
// silently fall back to in-memory persistence and lose the session on
// reload — the same guarantee the web start app gets.
export function ensureFirebase() {
  ensureFirebaseApp(firebaseConfig);
}
