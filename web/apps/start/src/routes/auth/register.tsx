import { createFileRoute, useRouter } from '@tanstack/react-router'
import { RegistrationFeature } from '@pivox/features/registration'
import { RegistrationCard } from '@pivox/ui/registration-card'

export const Route = createFileRoute('/auth/register')({ component: RegisterPage })

function RegisterPage() {
  const router = useRouter()

  return (
    <RegistrationFeature
      onSuccess={(user) =>
        router.navigate({
          to: user.emailVerified ? '/' : '/auth/verify-email',
        })
      }
      onLinkRequired={() => router.navigate({ to: '/auth/link-account' })}
    >
      <RegistrationCard.Root>
        <RegistrationCard.Header />
        <RegistrationCard.EmailField />
        <RegistrationCard.DisplayNameField />
        <RegistrationCard.PasswordField />
        <RegistrationCard.ConfirmPasswordField />
        <RegistrationCard.SubmitButton />
        <RegistrationCard.Separator />
        <RegistrationCard.SocialButtons />
        <RegistrationCard.Footer
          onClick={() => router.navigate({ to: '/auth/login' })}
        />
      </RegistrationCard.Root>
    </RegistrationFeature>
  )
}
