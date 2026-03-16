"use client"

import { useState } from "react"
import { getAuth, sendEmailVerification } from "firebase/auth"
import type { VerifyEmailContextValue, VerifyEmailState } from "@pivox/ui/verify-email-card"
import { firebaseErrorMessage } from "@/shared/firebase-error"

export function useVerifyEmail(): VerifyEmailContextValue {
  const auth = getAuth()
  const [resent, setResent] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const state: VerifyEmailState = {
    email: auth.currentUser?.email ?? null,
    resent,
    error,
  }

  const actions = {
    resendVerification: async () => {
      setError(null)
      setResent(false)
      try {
        if (!auth.currentUser) {
          setError("No user signed in")
          return
        }
        await sendEmailVerification(auth.currentUser)
        setResent(true)
      } catch (e) {
        setError(firebaseErrorMessage(e))
      }
    },
  }

  return { state, actions }
}
