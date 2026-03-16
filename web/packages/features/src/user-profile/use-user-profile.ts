"use client"

import { useState } from "react"
import {
  EmailAuthProvider,
  deleteUser,
  reauthenticateWithCredential,
  updatePassword,
  updateProfile,
} from "firebase/auth"
import type {
  UserProfileActions,
  UserProfileContextValue,
  UserProfileState,
} from "@pivox/ui/user-profile-card"
import { useAuth } from "@/auth/use-auth"
import { firebaseErrorMessage } from "@/shared/firebase-error"

export function useUserProfile(onClose?: () => void): UserProfileContextValue {
  const { user, signOut } = useAuth()
  const [activePage, setActivePage] = useState<"account" | "security">("account")
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)

  const clearStatus = () => {
    setError(null)
    setSuccess(null)
  }

  const state: UserProfileState = {
    displayName: user?.displayName ?? null,
    email: user?.email ?? null,
    photoURL: user?.photoURL ?? null,
    emailVerified: user?.emailVerified ?? false,
    providers:
      user?.providerData.map((p) => ({
        providerId: p.providerId,
        displayName: p.displayName,
        email: p.email,
        photoURL: p.photoURL,
      })) ?? [],
    activePage,
    error,
    success,
  }

  const actions: UserProfileActions = {
    setActivePage,
    updateDisplayName: async (name) => {
      clearStatus()
      try {
        if (!user) throw new Error("Not signed in")
        await updateProfile(user, { displayName: name })
        setSuccess("Display name updated")
      } catch (e) {
        setError(firebaseErrorMessage(e))
      }
    },

    updatePhoto: async (_file) => {
      clearStatus()
      // TODO: Upload file to Firebase Storage, get download URL,
      //       then call updateProfile(user, { photoURL })
      setError("Photo upload is not yet implemented")
    },

    removePhoto: async () => {
      clearStatus()
      try {
        if (!user) throw new Error("Not signed in")
        await updateProfile(user, { photoURL: "" })
        setSuccess("Photo removed")
      } catch (e) {
        setError(firebaseErrorMessage(e))
      }
    },

    changePassword: async (currentPassword, newPassword) => {
      clearStatus()
      try {
        if (!user || !user.email) throw new Error("Not signed in")
        const credential = EmailAuthProvider.credential(
          user.email,
          currentPassword,
        )
        await reauthenticateWithCredential(user, credential)
        await updatePassword(user, newPassword)
        setSuccess("Password updated")
      } catch (e) {
        setError(firebaseErrorMessage(e))
        throw e
      }
    },

    deleteAccount: async () => {
      clearStatus()
      try {
        if (!user) throw new Error("Not signed in")
        // TODO: May need reauthentication — handle auth/requires-recent-login
        await deleteUser(user)
        onClose?.()
      } catch (e) {
        setError(firebaseErrorMessage(e))
      }
    },

    signOut: async () => {
      onClose?.()
      await signOut()
    },
  }

  return { state, actions }
}
