import { useEffect } from 'react'
import { getAuth } from 'firebase/auth'
import { UserProfileCard } from '@pivox/ui/user-profile-card'
import { useUserProfile } from '@pivox/features/user-profile'
import { useAuth } from '@pivox/features/auth'

export function ElectronUserProfileFeature({
  onClose,
  children,
}: {
  onClose?: () => void
  children: React.ReactNode
}) {
  const value = useUserProfile(onClose)
  const { refreshUser } = useAuth()

  // Listen for deep link callbacks to refresh user after linking
  useEffect(() => {
    if (import.meta.env.DEV) return

    const unsubscribe = window.api.onAuthDeepLink(async (data) => {
      if (data.linked === 'true') {
        await refreshUser()
      }
    })

    return unsubscribe
  }, [refreshUser])

  // Override linkProvider for production builds
  const overriddenValue = import.meta.env.DEV
    ? value
    : {
        ...value,
        actions: {
          ...value.actions,
          linkProvider: async (providerId: string) => {
            try {
              const auth = getAuth()
              const user = auth.currentUser
              if (!user) throw new Error('Not signed in')
              const idToken = await user.getIdToken()
              await window.api.startLinkProvider(providerId, idToken)
            } catch {
              // Error will come back via deep link
            }
          },
        },
      }

  return (
    <UserProfileCard.Provider value={overriddenValue}>
      {children}
    </UserProfileCard.Provider>
  )
}
