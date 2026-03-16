"use client"

import { LinkAccountCard } from "@pivox/ui/link-account-card"
import { useLinkAccount } from "./use-link-account"
import type { User } from "firebase/auth"

export function LinkAccountFeature({
  onSuccess,
  children,
}: {
  onSuccess?: (user: User) => void
  children: React.ReactNode
}) {
  const value = useLinkAccount(onSuccess)

  return (
    <LinkAccountCard.Provider value={value}>
      {children}
    </LinkAccountCard.Provider>
  )
}
