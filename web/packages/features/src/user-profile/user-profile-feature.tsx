"use client"

import { UserProfileCard } from "@pivox/ui/user-profile-card"
import { useUserProfile } from "./use-user-profile"

export function UserProfileFeature({
  onClose,
  children,
}: {
  onClose?: () => void
  children: React.ReactNode
}) {
  const value = useUserProfile(onClose)

  return (
    <UserProfileCard.Provider value={value}>
      {children}
    </UserProfileCard.Provider>
  )
}
