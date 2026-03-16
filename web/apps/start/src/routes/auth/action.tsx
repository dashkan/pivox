import { createFileRoute, useRouter } from '@tanstack/react-router'
import { useEffect } from 'react'

// TODO: Configure Firebase to use https://pivox.app/auth/action as the
//       action URL in Firebase Console > Authentication > Templates
// TODO: Handle 'recoverEmail' mode for email change reversal
// TODO: Handle 'verifyAndChangeEmail' mode for email change confirmation

type ActionSearch = {
  mode: string
  oobCode: string
  continueUrl?: string
  lang?: string
}

export const Route = createFileRoute('/auth/action')({
  validateSearch: (search: Record<string, unknown>): ActionSearch => ({
    mode: (search.mode as string) || '',
    oobCode: (search.oobCode as string) || '',
    continueUrl: (search.continueUrl as string) || undefined,
    lang: (search.lang as string) || undefined,
  }),
  component: ActionPage,
})

function ActionPage() {
  const router = useRouter()
  const { mode, oobCode } = Route.useSearch()

  useEffect(() => {
    switch (mode) {
      case 'resetPassword':
        router.navigate({
          to: '/auth/reset-password',
          search: { oobCode },
        })
        break
      case 'verifyEmail':
        // TODO: Apply the verification code here via applyActionCode(auth, oobCode)
        //       then redirect to a success page or login
        router.navigate({ to: '/auth/login' })
        break
      default:
        router.navigate({ to: '/auth/login' })
        break
    }
  }, [mode, oobCode, router])

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <p className="text-sm text-muted-foreground">Processing...</p>
    </div>
  )
}
