import { getApps, initializeApp } from "firebase/app"
import { connectAuthEmulator, getAuth } from "firebase/auth"

const firebaseConfig = {
  apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
  authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
  projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
}

export function ensureFirebase() {
  if (getApps().length > 0) return

  const app = initializeApp(firebaseConfig)
  const auth = getAuth(app)

  if (import.meta.env.DEV) {
    connectAuthEmulator(auth, "http://localhost:9099", { disableWarnings: true })
  }
}
