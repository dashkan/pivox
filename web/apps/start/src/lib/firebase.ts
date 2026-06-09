import { ensureFirebaseApp } from '@pivox/features/auth';

const firebaseConfig = {
  apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
  authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
  projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
  storageBucket: import.meta.env.VITE_FIREBASE_STORAGE_BUCKET,
  messagingSenderId: import.meta.env.VITE_FIREBASE_MESSAGING_SENDER_ID,
  appId: import.meta.env.VITE_FIREBASE_APP_ID,
};

// Shared hardened init (explicit persistence + storage.persist) lives in
// @pivox/features/auth so start and electron can't drift. SSR-guarded
// inside ensureFirebaseApp. Invoked at router construction, ahead of any
// getAuth() consumer.
export function ensureFirebase() {
  ensureFirebaseApp(firebaseConfig);
}
