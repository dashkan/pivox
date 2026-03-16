"use client"

import { RegistrationCard } from "@pivox/ui/registration-card"
import { useRegistration } from "./use-registration"
import type { User } from "firebase/auth"

export function RegistrationFeature({
  onSuccess,
  children,
}: {
  onSuccess?: (user: User) => void
  children: React.ReactNode
}) {
  const value = useRegistration(onSuccess)

  return (
    <RegistrationCard.Provider value={value}>
      {children}
    </RegistrationCard.Provider>
  )
}
