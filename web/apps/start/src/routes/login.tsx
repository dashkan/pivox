import { createFileRoute, useRouter } from '@tanstack/react-router'
import { LoginFeature } from '@pivox/features/login'
import { LoginCard } from '@pivox/ui/login-card'

export const Route = createFileRoute('/login')({ component: LoginPage })

function LoginPage() {
  const router = useRouter()

  return (
    <LoginFeature onSuccess={() => router.navigate({ to: '/' })}>
      <LoginCard.Root>
        <LoginCard.Header />
        <LoginCard.EmailField />
        <LoginCard.PasswordField />
        <div className="flex items-center justify-between px-4">
          <LoginCard.RememberMe />
          <LoginCard.ForgotPassword
            onClick={() => router.navigate({ to: '/forgot-password'  })}
          />
        </div>
        <LoginCard.SubmitButton />
        <LoginCard.Separator />
        <LoginCard.SocialButtons providers={['google']} />
        <LoginCard.Footer
          onClick={() => router.navigate({ to: '/register'  })}
        />
      </LoginCard.Root>
    </LoginFeature>
  )
}
