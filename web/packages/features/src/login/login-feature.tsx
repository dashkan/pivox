"use client"

import { LoginCard } from "@pivox/ui/login-card"
import { useLogin } from "./use-login"
import type { User } from "firebase/auth"

export function LoginFeature({
  onSuccess,
  children,
}: {
  onSuccess?: (user: User) => void
  children: React.ReactNode
}) {
  const value = useLogin(onSuccess)

  return <LoginCard.Provider value={value}>{children}</LoginCard.Provider>
}
