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
import { AppleIcon, GitHubIcon, GoogleIcon } from "@/shared/social-icons"

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

function LoginCardRoot({
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
  providers = ["google", "github"],
  className,
}: {
  providers?: Array<"google" | "github" | "apple">
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
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const LoginCard = {
  Provider: LoginCardProvider,
  Root: LoginCardRoot,
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
