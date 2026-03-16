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
import type { FirebaseError } from "firebase/app"
import { firebaseErrorMessage } from "@/shared/firebase-error"
import { setPendingLink } from "@/shared/pending-link"

const socialProviders = {
  google: () => new GoogleAuthProvider(),
  github: () => new GithubAuthProvider(),
  apple: () => new OAuthProvider("apple.com"),
} as const

const providerNames: Record<string, string> = {
  google: "Google",
  github: "GitHub",
  apple: "Apple",
}

export function useLogin(
  onSuccess?: (user: User) => void,
  onLinkRequired?: (email: string) => void,
): LoginContextValue {
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
      } catch (e) {
        const err = e as FirebaseError
        if (
          err.code === "auth/account-exists-with-different-credential" &&
          err.customData?.email
        ) {
          const credential = OAuthProvider.credentialFromError(err)
          if (credential) {
            setPendingLink({
              email: err.customData.email as string,
              credential,
              providerName: providerNames[provider] ?? provider,
            })
            onLinkRequired?.(err.customData.email as string)
          }
        }
      }
    },

    ssoLogin: async () => {
      try {
        const auth = getAuth()
        const ssoProvider = new OAuthProvider("oidc.pivox")
        const result = await signInWithPopup(auth, ssoProvider)
        onSuccess?.(result.user)
      } catch (e) {
        const err = e as FirebaseError
        if (
          err.code === "auth/account-exists-with-different-credential" &&
          err.customData?.email
        ) {
          const credential = OAuthProvider.credentialFromError(err)
          if (credential) {
            setPendingLink({
              email: err.customData.email as string,
              credential,
              providerName: "SSO",
            })
            onLinkRequired?.(err.customData.email as string)
          }
        }
      }
    },
  }

  const meta: LoginMeta = { emailRef }

  return { state, actions, meta }
}
