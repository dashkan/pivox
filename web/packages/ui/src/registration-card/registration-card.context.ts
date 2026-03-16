"use client"

import { createContext, use } from "react"
import type { RegistrationContextValue } from "./registration-card.types"

export const RegistrationContext = createContext<RegistrationContextValue | null>(null)

export function useRegistrationContext() {
  const ctx = use(RegistrationContext)
  if (!ctx) {
    throw new Error("RegistrationCard subcomponents must be used within a RegistrationCard.Provider")
  }
  return ctx
}
