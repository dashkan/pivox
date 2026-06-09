import { getApps, initializeApp } from 'firebase/app';
import {
  browserLocalPersistence,
  indexedDBLocalPersistence,
  initializeAuth,
} from 'firebase/auth';

const firebaseConfig = {
  apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
  authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
  projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
  storageBucket: import.meta.env.VITE_FIREBASE_STORAGE_BUCKET,
  messagingSenderId: import.meta.env.VITE_FIREBASE_MESSAGING_SENDER_ID,
  appId: import.meta.env.VITE_FIREBASE_APP_ID,
};

export function ensureFirebase() {
  if (typeof window === 'undefined') return;
  if (getApps().length > 0) return;

  // initializeApp registers the app in Firebase's global registry as a
  // side effect; downstream getAuth() calls retrieve it.
  const app = initializeApp(firebaseConfig);

  // Pin auth persistence explicitly instead of letting the first
  // getAuth() pick a default. With the default, if IndexedDB isn't
  // reachable at that first call the SDK silently locks to in-memory
  // persistence for the session — the user is then lost on every
  // reload while the server cookie lingers, exactly the half-auth
  // desync we hit. Ordered fallback: IndexedDB first, localStorage
  // next. `initializeAuth` must run before any getAuth() consumer;
  // ensureFirebase() is invoked at router construction, ahead of
  // render, so it does. No popupRedirectResolver: the app signs in via
  // signInWithCredential / signInWithCustomToken, never redirect/popup.
  initializeAuth(app, {
    persistence: [indexedDBLocalPersistence, browserLocalPersistence],
  });

  // Best-effort: request persistent-storage so the browser won't evict
  // Firebase's auth IndexedDB under storage pressure (the suspected
  // trigger for the session loss). Returns a promise we don't await.
  // `navigator.storage` / `.persist` are typed as always-present but
  // are absent in non-secure contexts and older browsers, so the
  // runtime guard is real despite the type saying it's unnecessary.
  // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition
  void navigator.storage?.persist?.();
}
