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

  const signOut = async () => {
    const { getAuth, signOut: firebaseSignOut } = await import("firebase/auth")
    const auth = getAuth()
    await firebaseSignOut(auth)
  }

  return (
    <AuthContext value={{ user, loading, signOut }}>
      {children}
    </AuthContext>
  )
}
