"use client"

import { AppLayout } from "@pivox/ui/app-layout"
import { useAppLayout } from "./use-app-layout"

export function AppLayoutFeature({
  onNavigateToLogin,
  children,
}: {
  onNavigateToLogin: () => void
  children: React.ReactNode
}) {
  const value = useAppLayout(onNavigateToLogin)

  return <AppLayout.Provider value={value}>{children}</AppLayout.Provider>
}
