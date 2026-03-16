"use client"

import { createContext, use } from "react"
import type { UserProfileContextValue } from "./user-profile-card.types"

export const UserProfileContext = createContext<UserProfileContextValue | null>(null)

export function useUserProfileContext() {
  const ctx = use(UserProfileContext)
  if (!ctx) {
    throw new Error("UserProfileCard subcomponents must be used within a UserProfileCard.Provider")
  }
  return ctx
}
