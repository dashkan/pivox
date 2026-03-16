"use client"

import { UserProfileCard } from "@pivox/ui/user-profile-card"
import { useUserProfile } from "./use-user-profile"

export function UserProfileFeature({
  children,
}: {
  children: React.ReactNode
}) {
  const value = useUserProfile()

  return (
    <UserProfileCard.Provider value={value}>
      {children}
    </UserProfileCard.Provider>
  )
}
