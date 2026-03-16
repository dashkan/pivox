"use client"

import { createContext, use } from "react"
import type { ResetPasswordContextValue } from "./reset-password-card.types"

export const ResetPasswordContext = createContext<ResetPasswordContextValue | null>(null)

export function useResetPasswordContext() {
  const ctx = use(ResetPasswordContext)
  if (!ctx) {
    throw new Error("ResetPasswordCard subcomponents must be used within a ResetPasswordCard.Provider")
  }
  return ctx
}
