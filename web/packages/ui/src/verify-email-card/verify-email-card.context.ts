"use client"

import { createContext, use } from "react"
import type { VerifyEmailContextValue } from "./verify-email-card.types"

export const VerifyEmailContext = createContext<VerifyEmailContextValue | null>(null)

export function useVerifyEmailContext() {
  const ctx = use(VerifyEmailContext)
  if (!ctx) {
    throw new Error("VerifyEmailCard subcomponents must be used within a VerifyEmailCard.Provider")
  }
  return ctx
}
