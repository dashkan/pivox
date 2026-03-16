"use client"

import { useEffect, useState } from "react"
import { cn } from "@pivox/primitives/utils"
import { Button } from "@pivox/primitives/button"

type Theme = "light" | "system" | "dark"

const STORAGE_KEY = "pivox-theme"

const themes: Array<Theme> = ["light", "system", "dark"]

function getStoredTheme(): Theme {
  if (typeof window === "undefined") return "system"
  return (localStorage.getItem(STORAGE_KEY) as Theme | null) ?? "system"
}

function getSystemPreference(): "light" | "dark" {
  if (typeof window === "undefined") return "light"
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light"
}

function applyTheme(theme: Theme) {
  const resolved = theme === "system" ? getSystemPreference() : theme
  document.documentElement.classList.toggle("dark", resolved === "dark")
}

export function ThemeSwitcher({ className }: { className?: string }) {
  const [theme, setTheme] = useState<Theme>("system")
  const [mounted, setMounted] = useState(false)

  useEffect(() => {
    setTheme(getStoredTheme())
    setMounted(true)
  }, [])

  useEffect(() => {
    if (!mounted) return
    applyTheme(theme)
    localStorage.setItem(STORAGE_KEY, theme)
  }, [theme, mounted])

  useEffect(() => {
    const mql = window.matchMedia("(prefers-color-scheme: dark)")
    const handler = () => {
      if (getStoredTheme() === "system") applyTheme("system")
    }
    mql.addEventListener("change", handler)
    return () => mql.removeEventListener("change", handler)
  }, [])

  const cycle = () => {
    const idx = themes.indexOf(theme)
    const next = themes[(idx + 1) % themes.length]
    if (next) setTheme(next)
  }

  if (!mounted) {
    return (
      <Button variant="ghost" size="icon" className={className} disabled>
        <span className="size-4" />
      </Button>
    )
  }

  return (
    <Button
      variant="ghost"
      size="icon"
      className={cn("relative", className)}
      onClick={cycle}
      aria-label={`Theme: ${theme}`}
    >
      <span className="relative size-4">
        {/* Sun */}
        <svg
          className={cn(
            "absolute inset-0 size-4 transition-all duration-300",
            theme === "light"
              ? "scale-100 rotate-0 opacity-100"
              : "scale-0 rotate-90 opacity-0",
          )}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2" />
          <path d="M12 20v2" />
          <path d="m4.93 4.93 1.41 1.41" />
          <path d="m17.66 17.66 1.41 1.41" />
          <path d="M2 12h2" />
          <path d="M20 12h2" />
          <path d="m6.34 17.66-1.41 1.41" />
          <path d="m19.07 4.93-1.41 1.41" />
        </svg>

        {/* Monitor (system) */}
        <svg
          className={cn(
            "absolute inset-0 size-4 transition-all duration-300",
            theme === "system"
              ? "scale-100 rotate-0 opacity-100"
              : "scale-0 -rotate-90 opacity-0",
          )}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <rect width="20" height="14" x="2" y="3" rx="2" />
          <line x1="8" x2="16" y1="21" y2="21" />
          <line x1="12" x2="12" y1="17" y2="21" />
        </svg>

        {/* Moon */}
        <svg
          className={cn(
            "absolute inset-0 size-4 transition-all duration-300",
            theme === "dark"
              ? "scale-100 rotate-0 opacity-100"
              : "scale-0 rotate-90 opacity-0",
          )}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
        </svg>
      </span>
    </Button>
  )
}
