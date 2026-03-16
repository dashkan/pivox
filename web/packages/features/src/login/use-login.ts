"use client"

import { useActionState, useRef, useState } from "react"
import {
  GithubAuthProvider,
  GoogleAuthProvider,
  OAuthProvider,
  getAuth,
  signInWithEmailAndPassword,
  signInWithPopup,
} from "firebase/auth"
import type {
  LoginActions,
  LoginContextValue,
  LoginMeta,
  LoginState,
} from "@pivox/ui/login-card"
import type { User } from "firebase/auth"

const socialProviders = {
  google: () => new GoogleAuthProvider(),
  github: () => new GithubAuthProvider(),
  apple: () => new OAuthProvider("apple.com"),
} as const

export function useLogin(onSuccess?: (user: User) => void): LoginContextValue {
  const emailRef = useRef<HTMLInputElement | null>(null)
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")

  const [formState, formAction] = useActionState(
    async (_prev: { error: string | null }) => {
      try {
        const auth = getAuth()
        const credential = await signInWithEmailAndPassword(auth, email, password)
        onSuccess?.(credential.user)
        return { error: null }
      } catch (e) {
        return { error: firebaseErrorMessage(e) }
      }
    },
    { error: null },
  )

  const state: LoginState = {
    email,
    password,
    error: formState.error,
  }

  const actions: LoginActions = {
    updateEmail: setEmail,
    updatePassword: setPassword,
    formAction,

    socialLogin: async (provider) => {
      try {
        const auth = getAuth()
        const result = await signInWithPopup(auth, socialProviders[provider]())
        onSuccess?.(result.user)
      } catch {
        // social login errors are shown via popup, not inline
      }
    },

    ssoLogin: async () => {
      try {
        const auth = getAuth()
        const ssoProvider = new OAuthProvider("oidc.pivox")
        const result = await signInWithPopup(auth, ssoProvider)
        onSuccess?.(result.user)
      } catch {
        // SSO errors are shown via popup
      }
    },
  }

  const meta: LoginMeta = { emailRef }

  return { state, actions, meta }
}

function firebaseErrorMessage(e: unknown): string {
  if (typeof e === "object" && e !== null && "code" in e) {
    const code = (e as { code: string }).code
    switch (code) {
      case "auth/invalid-email":
        return "Invalid email address"
      case "auth/user-disabled":
        return "This account has been disabled"
      case "auth/user-not-found":
        return "No account found with this email"
      case "auth/wrong-password":
        return "Incorrect password"
      case "auth/invalid-credential":
        return "Invalid email or password"
      case "auth/popup-closed-by-user":
        return "Sign-in popup was closed"
      case "auth/popup-blocked":
        return "Sign-in popup was blocked. Please allow popups"
      default:
        return "Something went wrong. Please try again"
    }
  }
  return "Something went wrong. Please try again"
}
