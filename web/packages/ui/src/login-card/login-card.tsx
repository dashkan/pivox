"use client"

import { useFormStatus } from "react-dom"
import { cn } from "@pivox/primitives/utils"
import { Button } from "@pivox/primitives/button"
import {
  Card,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@pivox/primitives/card"
import { Input } from "@pivox/primitives/input"
import { Field, FieldError, FieldLabel } from "@pivox/primitives/field"
import { Checkbox } from "@pivox/primitives/checkbox"
import { Label } from "@pivox/primitives/label"
import { Separator } from "@pivox/primitives/separator"
import { LoginContext, useLoginContext } from "./login-card.context"
import type { LoginContextValue } from "./login-card.types"

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function LoginCardProvider({
  value,
  children,
}: {
  value: LoginContextValue
  children: React.ReactNode
}) {
  return <LoginContext value={value}>{children}</LoginContext>
}

/* ------------------------------------------------------------------ */
/*  Frame                                                             */
/* ------------------------------------------------------------------ */

function LoginCardFrame({
  className,
  children,
}: {
  className?: string
  children: React.ReactNode
}) {
  const { actions } = useLoginContext()
  return (
    <div className={cn("flex min-h-screen items-center justify-center p-4", className)}>
      <Card className="w-full max-w-sm">
        <form action={actions.formAction} className="flex flex-col gap-4">
          {children}
        </form>
      </Card>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Header                                                            */
/* ------------------------------------------------------------------ */

function LoginCardHeader({ className }: { className?: string }) {
  return (
    <CardHeader className={cn("text-center", className)}>
      <CardTitle className="text-xl">Sign in</CardTitle>
      <CardDescription>Sign in to your account</CardDescription>
    </CardHeader>
  )
}

/* ------------------------------------------------------------------ */
/*  EmailField                                                        */
/* ------------------------------------------------------------------ */

function LoginCardEmailField({ className }: { className?: string }) {
  const { state, actions, meta } = useLoginContext()
  const { pending } = useFormStatus()
  return (
    <Field className={cn("px-4", className)}>
      <FieldLabel>Email</FieldLabel>
      <Input
        ref={meta.emailRef}
        name="email"
        type="email"
        placeholder="name@example.com"
        autoComplete="email"
        value={state.email}
        onChange={(e) => actions.updateEmail(e.target.value)}
        disabled={pending}
      />
    </Field>
  )
}

/* ------------------------------------------------------------------ */
/*  PasswordField                                                     */
/* ------------------------------------------------------------------ */

function LoginCardPasswordField({ className }: { className?: string }) {
  const { state, actions } = useLoginContext()
  const { pending } = useFormStatus()
  return (
    <Field className={cn("px-4", className)}>
      <FieldLabel>Password</FieldLabel>
      <Input
        name="password"
        type="password"
        autoComplete="current-password"
        value={state.password}
        onChange={(e) => actions.updatePassword(e.target.value)}
        disabled={pending}
      />
    </Field>
  )
}

/* ------------------------------------------------------------------ */
/*  RememberMe                                                        */
/* ------------------------------------------------------------------ */

function LoginCardRememberMe({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <Checkbox id="remember" />
      <Label htmlFor="remember" className="text-sm font-normal">
        Remember me
      </Label>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  ForgotPassword                                                    */
/* ------------------------------------------------------------------ */

function LoginCardForgotPassword({
  onClick,
  className,
}: {
  onClick: () => void
  className?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "text-sm text-muted-foreground underline-offset-4 hover:text-primary hover:underline",
        className,
      )}
    >
      Forgot password?
    </button>
  )
}

/* ------------------------------------------------------------------ */
/*  SubmitButton                                                      */
/* ------------------------------------------------------------------ */

function LoginCardSubmitButton({ className }: { className?: string }) {
  const { state } = useLoginContext()
  const { pending } = useFormStatus()
  return (
    <div className={cn("flex flex-col gap-4 px-4", className)}>
      {state.error && <FieldError>{state.error}</FieldError>}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? "Please wait…" : "Sign in"}
      </Button>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Separator                                                         */
/* ------------------------------------------------------------------ */

function LoginCardSeparator({ className }: { className?: string }) {
  return (
    <div className={cn("relative px-4", className)}>
      <div className="absolute inset-x-4 inset-y-0 flex items-center">
        <Separator className="w-full" />
      </div>
      <div className="relative flex justify-center text-xs uppercase">
        <span className="bg-card px-2 text-muted-foreground">or</span>
      </div>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  SocialButtons                                                     */
/* ------------------------------------------------------------------ */

function LoginCardSocialButtons({
  providers,
  className,
}: {
  providers: Array<"google" | "github" | "apple">
  className?: string
}) {
  const { actions } = useLoginContext()
  const { pending } = useFormStatus()

  return (
    <div className={cn("flex flex-col gap-2 px-4", className)}>
      {providers.includes("google") && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={pending}
          onClick={() => actions.socialLogin("google")}
        >
          <GoogleIcon />
          Sign in with Google
        </Button>
      )}
      {providers.includes("github") && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={pending}
          onClick={() => actions.socialLogin("github")}
        >
          <GitHubIcon />
          Sign in with GitHub
        </Button>
      )}
      {providers.includes("apple") && (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={pending}
          onClick={() => actions.socialLogin("apple")}
        >
          <AppleIcon />
          Sign in with Apple
        </Button>
      )}
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  SSOButton                                                         */
/* ------------------------------------------------------------------ */

function LoginCardSSOButton({ className }: { className?: string }) {
  const { actions } = useLoginContext()
  const { pending } = useFormStatus()
  return (
    <Button
      type="button"
      variant="outline"
      className={cn("mx-4 w-auto", className)}
      disabled={pending}
      onClick={actions.ssoLogin}
    >
      Sign in with SSO
    </Button>
  )
}

/* ------------------------------------------------------------------ */
/*  Footer                                                            */
/* ------------------------------------------------------------------ */

function LoginCardFooter({
  onClick,
  className,
}: {
  onClick: () => void
  className?: string
}) {
  return (
    <CardFooter className={cn("justify-center", className)}>
      <p className="text-sm text-muted-foreground">
        Don&apos;t have an account?{" "}
        <button
          type="button"
          className="text-primary underline-offset-4 hover:underline"
          onClick={onClick}
        >
          Sign up
        </button>
      </p>
    </CardFooter>
  )
}

/* ------------------------------------------------------------------ */
/*  Icons                                                             */
/* ------------------------------------------------------------------ */

function GoogleIcon({ className }: { className?: string }) {
  return (
    <svg className={cn("size-4", className)} viewBox="0 0 24 24" aria-hidden="true">
      <path
        d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z"
        fill="#4285F4"
      />
      <path
        d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
        fill="#34A853"
      />
      <path
        d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
        fill="#FBBC05"
      />
      <path
        d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
        fill="#EA4335"
      />
    </svg>
  )
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg className={cn("size-4", className)} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2z" />
    </svg>
  )
}

function AppleIcon({ className }: { className?: string }) {
  return (
    <svg className={cn("size-4", className)} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M17.05 20.28c-.98.95-2.05.88-3.08.4-1.09-.5-2.08-.48-3.24 0-1.44.62-2.2.44-3.06-.4C2.79 15.25 3.51 7.59 9.05 7.31c1.35.07 2.29.74 3.08.8 1.18-.24 2.31-.93 3.57-.84 1.51.12 2.65.72 3.4 1.8-3.12 1.87-2.38 5.98.48 7.13-.57 1.5-1.31 2.99-2.54 4.09zM12.03 7.25c-.15-2.23 1.66-4.07 3.74-4.25.29 2.58-2.34 4.5-3.74 4.25z" />
    </svg>
  )
}

/* ------------------------------------------------------------------ */
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const LoginCard = {
  Provider: LoginCardProvider,
  Frame: LoginCardFrame,
  Header: LoginCardHeader,
  EmailField: LoginCardEmailField,
  PasswordField: LoginCardPasswordField,
  RememberMe: LoginCardRememberMe,
  ForgotPassword: LoginCardForgotPassword,
  SubmitButton: LoginCardSubmitButton,
  Separator: LoginCardSeparator,
  SocialButtons: LoginCardSocialButtons,
  SSOButton: LoginCardSSOButton,
  Footer: LoginCardFooter,
  Context: LoginContext,
}
