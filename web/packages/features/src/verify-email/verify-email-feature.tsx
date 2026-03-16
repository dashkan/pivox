"use client"

import { VerifyEmailCard } from "@pivox/ui/verify-email-card"
import { useVerifyEmail } from "./use-verify-email"

export function VerifyEmailFeature({
  children,
}: {
  children: React.ReactNode
}) {
  const value = useVerifyEmail()

  return (
    <VerifyEmailCard.Provider value={value}>
      {children}
    </VerifyEmailCard.Provider>
  )
}
