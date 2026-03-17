"use client"

import { useEffect, useState } from "react"
import { AuthContext } from "./use-auth"
import type { User } from "firebase/auth"

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let unsubscribe: (() => void) | undefined

    import("firebase/auth").then(({ getAuth, onAuthStateChanged }) => {
      const auth = getAuth()
      unsubscribe = onAuthStateChanged(auth, (firebaseUser) => {
        setUser(firebaseUser)
        setLoading(false)
      })
    })

    return () => unsubscribe?.()
  }, [])

  // TODO: Firebase SDK has no callback for emailVerified changes.
  // When we handle SMTP ourselves, implement a mechanism to refresh
  // user state after email verification (e.g., custom webhook, polling,
  // or tab focus listener via user.reload()).
  const refreshUser = async () => {
    const { getAuth } = await import("firebase/auth")
    const currentUser = getAuth().currentUser
    if (currentUser) {
      await currentUser.reload()
      setUser({ ...currentUser })
    }
  }

  const signOut = async () => {
    const { getAuth, signOut: firebaseSignOut } = await import("firebase/auth")
    const auth = getAuth()
    await firebaseSignOut(auth)
  }

  return (
    <AuthContext value={{ user, loading, signOut, refreshUser }}>
      {children}
    </AuthContext>
  )
}
