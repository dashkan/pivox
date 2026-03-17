"use client"

import { createContext, use } from "react"
import type { User } from "firebase/auth"

export interface AuthContextValue {
  user: User | null
  loading: boolean
  signOut: () => Promise<void>
  refreshUser: () => Promise<void>
}

export const AuthContext = createContext<AuthContextValue | null>(null)

export function useAuth(): AuthContextValue {
  const ctx = use(AuthContext)
  if (!ctx) {
    throw new Error("useAuth must be used within an AuthProvider")
  }
  return ctx
}
