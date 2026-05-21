import { getApps, initializeApp } from 'firebase/app';

const firebaseConfig = {
  apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
  authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
  projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
};

export function ensureFirebase() {
  if (getApps().length > 0) return;

  // initializeApp registers the app in Firebase's global registry as a
  // side effect; downstream getAuth() calls retrieve it.
  initializeApp(firebaseConfig);
}
