import { createFileRoute, useRouter } from '@tanstack/react-router'
import { ForgotPasswordFeature } from '@pivox/features/forgot-password'
import { ForgotPasswordCard } from '@pivox/ui/forgot-password-card'

export const Route = createFileRoute('/auth/forgot-password')({
  component: ForgotPasswordPage,
})

function ForgotPasswordPage() {
  const router = useRouter()

  return (
    <ForgotPasswordFeature>
      <ForgotPasswordCard.Root>
        <ForgotPasswordCard.Header />
        <ForgotPasswordCard.EmailField />
        <ForgotPasswordCard.SuccessMessage />
        <ForgotPasswordCard.SubmitButton />
        <ForgotPasswordCard.Footer
          onClick={() => router.navigate({ to: '/auth/login' })}
        />
      </ForgotPasswordCard.Root>
    </ForgotPasswordFeature>
  )
}
