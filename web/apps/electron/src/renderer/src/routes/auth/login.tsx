import { createFileRoute, useRouter } from '@tanstack/react-router'
import { LoginFeature } from '@pivox/features/login'
import { LoginCard } from '@pivox/ui/login-card'

export const Route = createFileRoute('/auth/login')({ component: LoginPage })

function LoginPage() {
  const router = useRouter()

  return (
    <LoginFeature
      onSuccess={() => router.navigate({ to: '/' })}
      onLinkRequired={() => router.navigate({ to: '/auth/link-account' })}
    >
      <LoginCard.Root>
        <LoginCard.Header />
        <LoginCard.EmailField />
        <LoginCard.PasswordField />
        <div className="flex items-center justify-between px-4">
          <LoginCard.RememberMe />
          <LoginCard.ForgotPassword
            onClick={() => router.navigate({ to: '/auth/forgot-password' })}
          />
        </div>
        <LoginCard.SubmitButton />
        <LoginCard.Separator />
        <LoginCard.SocialButtons providers={['google']} />
        <LoginCard.Footer
          onClick={() => router.navigate({ to: '/auth/register' })}
        />
      </LoginCard.Root>
    </LoginFeature>
  )
}
