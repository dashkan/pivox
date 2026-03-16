import { createFileRoute, useRouter } from '@tanstack/react-router'
import { RegistrationFeature } from '@pivox/features/registration'
import { RegistrationCard } from '@pivox/ui/registration-card'

export const Route = createFileRoute('/register')({ component: RegisterPage })

function RegisterPage() {
  const router = useRouter()

  return (
    <RegistrationFeature onSuccess={() => router.navigate({ to: '/verify-email' as string })}>
      <RegistrationCard.Root>
        <RegistrationCard.Header />
        <RegistrationCard.EmailField />
        <RegistrationCard.DisplayNameField />
        <RegistrationCard.PasswordField />
        <RegistrationCard.ConfirmPasswordField />
        <RegistrationCard.SubmitButton />
        <RegistrationCard.Separator />
        <RegistrationCard.SocialButtons providers={['google']} />
        <RegistrationCard.Footer
          onClick={() => router.navigate({ to: '/login' })}
        />
      </RegistrationCard.Root>
    </RegistrationFeature>
  )
}
