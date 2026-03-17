"use client"

import { useState } from "react"
import { cn } from "@pivox/primitives/utils"
import { Button } from "@pivox/primitives/button"
import { Input } from "@pivox/primitives/input"
import { Field, FieldLabel } from "@pivox/primitives/field"
import { Separator } from "@pivox/primitives/separator"
import { Badge } from "@pivox/primitives/badge"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@pivox/primitives/dialog"
import { ScrollArea } from "@pivox/primitives/scroll-area"
import { UserProfileContext, useUserProfileContext } from "./user-profile-card.context"
import type { UserProfileContextValue } from "./user-profile-card.types"
import { UserAvatar } from "@/user-avatar/user-avatar"

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function UserProfileCardProvider({
  value,
  children,
}: {
  value: UserProfileContextValue
  children: React.ReactNode
}) {
  return <UserProfileContext value={value}>{children}</UserProfileContext>
}

/* ------------------------------------------------------------------ */
/*  Root — Dialog with sidebar + content                              */
/* ------------------------------------------------------------------ */

function UserProfileCardRoot({
  open,
  onOpenChange,
  className,
  children,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  className?: string
  children: React.ReactNode
}) {
  const { actions } = useUserProfileContext()
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        actions.clearStatus()
        onOpenChange(value)
      }}
    >
      <DialogContent
        className={cn(
          "sm:max-w-3xl gap-0 overflow-hidden p-0",
          className,
        )}
        onEscapeKeyDown={(e) => {
          if (document.activeElement?.closest("[data-inline-edit]")) {
            e.preventDefault()
          }
        }}
      >
        <DialogTitle className="sr-only">Profile settings</DialogTitle>
        <DialogDescription className="sr-only">
          Manage your account and security settings
        </DialogDescription>
        <div className="flex h-[520px]">{children}</div>
      </DialogContent>
    </Dialog>
  )
}

/* ------------------------------------------------------------------ */
/*  Sidebar                                                           */
/* ------------------------------------------------------------------ */

function UserProfileCardSidebar({ className }: { className?: string }) {
  const { state, actions } = useUserProfileContext()

  return (
    <nav
      className={cn(
        "flex w-48 shrink-0 flex-col gap-1 border-r bg-muted/30 p-3",
        className,
      )}
    >
      <button
        type="button"
        data-active={state.activePage === "account" || undefined}
        className="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-muted data-[active]:bg-muted data-[active]:font-medium data-[active]:text-foreground"
        onClick={() => actions.setActivePage("account")}
      >
        <UserIcon />
        Account
      </button>
      <button
        type="button"
        data-active={state.activePage === "security" || undefined}
        className="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-muted data-[active]:bg-muted data-[active]:font-medium data-[active]:text-foreground"
        onClick={() => actions.setActivePage("security")}
      >
        <ShieldIcon />
        Security
      </button>
    </nav>
  )
}

/* ------------------------------------------------------------------ */
/*  AccountPage                                                       */
/* ------------------------------------------------------------------ */

function UserProfileCardAccountPage({ className }: { className?: string }) {
  const { state } = useUserProfileContext()
  if (state.activePage !== "account") return null
  return (
    <div className={cn("flex flex-1 flex-col", className)}>
      <div className="border-b px-6 py-4">
        <h2 className="text-lg font-semibold">Account</h2>
        <p className="text-sm text-muted-foreground">
          Manage your account information
        </p>
      </div>
      <StatusSubsection />
      <ScrollArea className="flex-1">
        <div className="flex flex-col">
          <ProfileSubsection />
          <Separator />
          <EmailSubsection />
          <Separator />
          <ConnectedAccountsSubsection />
          <Separator />
          <DangerSubsection />
        </div>
      </ScrollArea>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  SecurityPage                                                      */
/* ------------------------------------------------------------------ */

function UserProfileCardSecurityPage({ className }: { className?: string }) {
  const { state } = useUserProfileContext()
  if (state.activePage !== "security") return null
  return (
    <div className={cn("flex flex-1 flex-col", className)}>
      <div className="border-b px-6 py-4">
        <h2 className="text-lg font-semibold">Security</h2>
        <p className="text-sm text-muted-foreground">
          Manage your security settings
        </p>
      </div>
      <StatusSubsection />
      <ScrollArea className="flex-1">
        <div className="flex flex-col">
          <PasswordSubsection />
          <Separator />
          <MFASubsection />
          <Separator />
          <ActiveSessionsSubsection />
        </div>
      </ScrollArea>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Subsections (internal)                                            */
/* ------------------------------------------------------------------ */

function ProfileSubsection() {
  const { state, actions } = useUserProfileContext()
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(state.displayName ?? "")

  const handleCancel = () => {
    setName(state.displayName ?? "")
    setEditing(false)
  }

  return (
    <div className="px-6 py-4">
      <h3 className="mb-3 text-sm font-medium">Profile</h3>
      <div className="flex items-center gap-4">
        <UserAvatar src={state.photoURL} name={state.displayName} size="lg" />
        <div className="flex flex-1 flex-col gap-0.5">
          {editing ? (
            <form
              data-inline-edit
              className="flex items-center gap-2"
              onSubmit={async (e) => {
                e.preventDefault()
                await actions.updateDisplayName(name)
                setEditing(false)
              }}
              onReset={handleCancel}
              onKeyDown={(e) => {
                if (e.key === "Escape") { e.stopPropagation(); handleCancel() }
              }}
            >
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="h-7 text-sm"
                autoFocus
              />
              <Button type="submit" size="sm">Save</Button>
              <Button type="reset" size="sm" variant="ghost">Cancel</Button>
            </form>
          ) : (
            <button
              type="button"
              className="flex items-center gap-2 text-left"
              onClick={() => setEditing(true)}
            >
              <span className="font-medium">
                {state.displayName || "No name set"}
              </span>
              <span className="text-xs text-muted-foreground">Edit</span>
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

function EmailSubsection() {
  const { state, actions } = useUserProfileContext()
  return (
    <div className="px-6 py-4">
      <h3 className="mb-3 text-sm font-medium">Email addresses</h3>
      <div className="flex items-center justify-between rounded-lg border p-3">
        <div className="flex items-center gap-2">
          <span className="text-sm">{state.email}</span>
          <Badge variant="secondary" className="text-xs">Primary</Badge>
          {state.emailVerified ? (
            <Badge variant="secondary" className="text-xs">Verified</Badge>
          ) : (
            <Badge variant="outline" className="text-xs text-destructive">Unverified</Badge>
          )}
        </div>
        {!state.emailVerified && (
          <Button
            size="sm"
            variant="ghost"
            onClick={actions.sendVerificationEmail}
          >
            Resend verification
          </Button>
        )}
      </div>
      {/* TODO: + Add an email address */}
    </div>
  )
}

const providerLabels: Record<string, string> = {
  "google.com": "Google",
  "github.com": "GitHub",
  "apple.com": "Apple",
  password: "Email & Password",
}

function ConnectedAccountsSubsection() {
  const { state, actions } = useUserProfileContext()
  return (
    <div className="px-6 py-4">
      <h3 className="mb-3 text-sm font-medium">Connected accounts</h3>
      <div className="flex flex-col gap-2">
        {state.providers.map((provider) => (
          <div
            key={provider.providerId}
            className="flex items-center justify-between rounded-lg border p-3"
          >
            <div className="flex flex-col gap-0.5">
              <span className="text-sm font-medium">
                {providerLabels[provider.providerId] ?? provider.providerId}
              </span>
              {provider.email && (
                <span className="text-xs text-muted-foreground">{provider.email}</span>
              )}
            </div>
            {/* TODO: Remove unlink for password provider — manage via password section instead */}
            {state.providers.length > 1 && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => actions.unlinkProvider(provider.providerId)}
              >
                Unlink
              </Button>
            )}
          </div>
        ))}
      </div>
      {/* TODO: + Connect account */}
    </div>
  )
}

function DangerSubsection() {
  const { actions } = useUserProfileContext()
  const [confirming, setConfirming] = useState(false)

  return (
    <div className="px-6 py-4">
      <h3 className="mb-3 text-sm font-medium text-destructive">Danger</h3>
      <div className="flex items-center justify-between rounded-lg border p-3">
        <div>
          <p className="text-sm font-medium">Delete account</p>
          <p className="text-xs text-muted-foreground">
            Delete your account and all its associated data
          </p>
        </div>
        {confirming ? (
          <div className="flex gap-2">
            <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
              Cancel
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={async () => {
                await actions.deleteAccount()
                setConfirming(false)
              }}
            >
              Confirm
            </Button>
          </div>
        ) : (
          <Button size="sm" variant="destructive" onClick={() => setConfirming(true)}>
            Delete account
          </Button>
        )}
      </div>
    </div>
  )
}

function SetPasswordSubsection() {
  const { actions } = useUserProfileContext()
  const [setting, setSetting] = useState(false)
  const [newPassword, setNewPassword] = useState("")
  const [confirmPassword, setConfirmPassword] = useState("")
  const [error, setError] = useState<string | null>(null)

  const handleCancel = () => {
    setSetting(false)
    setNewPassword("")
    setConfirmPassword("")
    setError(null)
  }

  return (
    <div className="px-6 py-4">
      <div className="mb-3 flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">Password</h3>
          {!setting && (
            <p className="text-xs text-muted-foreground">
              No password set. Add one to sign in with email and password.
            </p>
          )}
        </div>
        {!setting && (
          <Button size="sm" variant="outline" onClick={() => setSetting(true)}>
            Set password
          </Button>
        )}
      </div>
      {setting && (
        <form
          data-inline-edit
          className="flex flex-col gap-3"
          onSubmit={async (e) => {
            e.preventDefault()
            setError(null)
            if (newPassword.length < 6) {
              setError("Password must be at least 6 characters")
              return
            }
            if (newPassword !== confirmPassword) {
              setError("Passwords do not match")
              return
            }
            try {
              await actions.setPassword(newPassword)
              setSetting(false)
              setNewPassword("")
              setConfirmPassword("")
            } catch {
              // error surfaced via context
            }
          }}
          onReset={handleCancel}
          onKeyDown={(e) => {
            if (e.key === "Escape") { e.stopPropagation(); handleCancel() }
          }}
        >
          <Field>
            <FieldLabel>Password</FieldLabel>
            <Input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} autoFocus />
          </Field>
          <Field>
            <FieldLabel>Confirm password</FieldLabel>
            <Input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} />
          </Field>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div className="flex gap-2">
            <Button type="submit" size="sm">Set password</Button>
            <Button type="reset" size="sm" variant="ghost">Cancel</Button>
          </div>
        </form>
      )}
    </div>
  )
}

function PasswordSubsection() {
  const { state, actions } = useUserProfileContext()
  const [changing, setChanging] = useState(false)
  const [currentPassword, setCurrentPassword] = useState("")
  const [newPassword, setNewPassword] = useState("")
  const [confirmPassword, setConfirmPassword] = useState("")
  const [error, setError] = useState<string | null>(null)

  const hasPasswordProvider = state.providers.some((p) => p.providerId === "password")

  const handleCancel = () => {
    setChanging(false)
    setCurrentPassword("")
    setNewPassword("")
    setConfirmPassword("")
    setError(null)
  }

  if (!hasPasswordProvider) {
    return <SetPasswordSubsection />
  }

  return (
    <div className="px-6 py-4">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-medium">Password</h3>
        {!changing && (
          <Button size="sm" variant="outline" onClick={() => setChanging(true)}>
            Change password
          </Button>
        )}
      </div>
      {changing && (
        <form
          data-inline-edit
          className="flex flex-col gap-3"
          onSubmit={async (e) => {
            e.preventDefault()
            setError(null)
            if (newPassword !== confirmPassword) {
              setError("Passwords do not match")
              return
            }
            try {
              await actions.changePassword(currentPassword, newPassword)
              handleCancel()
            } catch {
              setError("Failed to change password")
            }
          }}
          onReset={handleCancel}
          onKeyDown={(e) => {
            if (e.key === "Escape") { e.stopPropagation(); handleCancel() }
          }}
        >
          <Field>
            <FieldLabel>Current password</FieldLabel>
            <Input type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} autoFocus />
          </Field>
          <Field>
            <FieldLabel>New password</FieldLabel>
            <Input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} />
          </Field>
          <Field>
            <FieldLabel>Confirm new password</FieldLabel>
            <Input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} />
          </Field>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div className="flex gap-2">
            <Button type="submit" size="sm">Update password</Button>
            <Button type="reset" size="sm" variant="ghost">Cancel</Button>
          </div>
        </form>
      )}
      {!state.emailVerified && !changing && (
        <div className="mt-3 flex items-center justify-between rounded-lg border border-destructive/20 bg-destructive/5 p-3">
          <p className="text-sm text-muted-foreground">
            Your email is not verified.
          </p>
          <Button
            size="sm"
            variant="ghost"
            onClick={actions.sendVerificationEmail}
          >
            Resend verification
          </Button>
        </div>
      )}
    </div>
  )
}

function MFASubsection() {
  return (
    <div className="px-6 py-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">Two-factor authentication</h3>
          <p className="text-xs text-muted-foreground">Add an extra layer of security to your account</p>
        </div>
        <Badge variant="outline" className="text-xs">Coming soon</Badge>
      </div>
    </div>
  )
}

function ActiveSessionsSubsection() {
  return (
    <div className="px-6 py-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">Active sessions</h3>
          <p className="text-xs text-muted-foreground">Manage your active sessions across devices</p>
        </div>
        <Badge variant="outline" className="text-xs">Coming soon</Badge>
      </div>
    </div>
  )
}

function StatusSubsection() {
  const { state } = useUserProfileContext()
  if (!state.error && !state.success) return null
  return (
    <div className="mx-6 my-3 rounded-lg border px-4 py-3">
      {state.error && <p className="text-sm text-destructive">{state.error}</p>}
      {state.success && <p className="text-sm text-muted-foreground">{state.success}</p>}
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Icons                                                             */
/* ------------------------------------------------------------------ */

function UserIcon() {
  return (
    <svg className="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2" />
      <circle cx="12" cy="7" r="4" />
    </svg>
  )
}

function ShieldIcon() {
  return (
    <svg className="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z" />
      <path d="m9 12 2 2 4-4" />
    </svg>
  )
}

/* ------------------------------------------------------------------ */
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const UserProfileCard = {
  Provider: UserProfileCardProvider,
  Root: UserProfileCardRoot,
  Sidebar: UserProfileCardSidebar,
  AccountPage: UserProfileCardAccountPage,
  SecurityPage: UserProfileCardSecurityPage,
  Context: UserProfileContext,
}
