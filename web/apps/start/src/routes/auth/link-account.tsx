import { createFileRoute, useRouter } from '@tanstack/react-router'
import { LinkAccountFeature } from '@pivox/features/link-account'
import { LinkAccountCard } from '@pivox/ui/link-account-card'

export const Route = createFileRoute('/auth/link-account')({
  component: LinkAccountPage,
})

function LinkAccountPage() {
  const router = useRouter()

  return (
    <LinkAccountFeature onSuccess={() => router.navigate({ to: '/' })}>
      <LinkAccountCard.Root>
        <LinkAccountCard.Header />
        <LinkAccountCard.PasswordField />
        <LinkAccountCard.SubmitButton />
        <LinkAccountCard.Footer
          onClick={() => router.navigate({ to: '/auth/login' })}
        />
      </LinkAccountCard.Root>
    </LinkAccountFeature>
  )
}
