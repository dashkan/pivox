'use client';

import { useEffect, useRef, useState } from 'react';
import { cn } from '@pivox/primitives/utils';
import { Button } from '@pivox/primitives/button';
import { Input } from '@pivox/primitives/input';
import { Field, FieldLabel } from '@pivox/primitives/field';
import { Separator } from '@pivox/primitives/separator';
import { Badge } from '@pivox/primitives/badge';
import { Alert, AlertAction, AlertDescription } from '@pivox/primitives/alert';
import {
  InputOTP,
  InputOTPGroup,
  InputOTPSlot,
} from '@pivox/primitives/input-otp';
import { QRCodeSVG } from 'qrcode.react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@pivox/primitives/dialog';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@pivox/primitives/popover';

import {
  UserProfileContext,
  useUserProfileContext,
} from './user-profile-card.context';
import type { UserProfileContextValue } from './user-profile-card.types';
import { UserAvatar } from '@/user-avatar/user-avatar';
import { AppleIcon, GitHubIcon, GoogleIcon } from '@/shared/social-icons';

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function UserProfileCardProvider({
  value,
  children,
}: {
  value: UserProfileContextValue;
  children: React.ReactNode;
}) {
  return <UserProfileContext value={value}>{children}</UserProfileContext>;
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
  open: boolean;
  onOpenChange: (open: boolean) => void;
  className?: string;
  children: React.ReactNode;
}) {
  const { actions } = useUserProfileContext();
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        actions.clearStatus();
        onOpenChange(value);
      }}
    >
      <DialogContent
        className={cn('sm:max-w-3xl gap-0 overflow-hidden p-0', className)}
        onEscapeKeyDown={(e) => {
          if (document.activeElement?.closest('[data-inline-edit]')) {
            e.preventDefault();
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
  );
}

/* ------------------------------------------------------------------ */
/*  Sidebar                                                           */
/* ------------------------------------------------------------------ */

function UserProfileCardSidebar({ className }: { className?: string }) {
  const { state, actions } = useUserProfileContext();

  return (
    <nav
      className={cn(
        'flex w-48 shrink-0 flex-col gap-1 border-r bg-muted/30 p-3',
        className,
      )}
    >
      <button
        type="button"
        data-active={state.activePage === 'account' || undefined}
        className="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-muted data-[active]:bg-muted data-[active]:font-medium data-[active]:text-foreground"
        onClick={() => actions.setActivePage('account')}
      >
        <UserIcon />
        Account
      </button>
      <button
        type="button"
        data-active={state.activePage === 'security' || undefined}
        className="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-muted data-[active]:bg-muted data-[active]:font-medium data-[active]:text-foreground"
        onClick={() => actions.setActivePage('security')}
      >
        <ShieldIcon />
        Security
      </button>
    </nav>
  );
}

/* ------------------------------------------------------------------ */
/*  AccountPage                                                       */
/* ------------------------------------------------------------------ */

function UserProfileCardAccountPage({ className }: { className?: string }) {
  const { state } = useUserProfileContext();
  if (state.activePage !== 'account') return null;
  return (
    <div className={cn('flex flex-1 flex-col', className)}>
      <div className="border-b px-6 py-4">
        <h2 className="text-lg font-semibold">Account</h2>
        <p className="text-sm text-muted-foreground">
          Manage your account information
        </p>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto [&::-webkit-scrollbar]:w-2.5 [&::-webkit-scrollbar-track]:transparent [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-border">
        <div className="flex flex-col">
          <ProfileSubsection />
          <Separator />
          <EmailSubsection />
          <Separator />
          <ConnectedAccountsSubsection />
          <Separator />
          <DangerSubsection />
          <StatusAlert />
        </div>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  SecurityPage                                                      */
/* ------------------------------------------------------------------ */

function UserProfileCardSecurityPage({ className }: { className?: string }) {
  const { state } = useUserProfileContext();
  if (state.activePage !== 'security') return null;
  return (
    <div className={cn('flex flex-1 flex-col', className)}>
      <div className="border-b px-6 py-4">
        <h2 className="text-lg font-semibold">Security</h2>
        <p className="text-sm text-muted-foreground">
          Manage your security settings
        </p>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto [&::-webkit-scrollbar]:w-2.5 [&::-webkit-scrollbar-track]:transparent [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-border">
        <div className="flex flex-col">
          <PasswordSubsection />
          <Separator />
          <MFASubsection />
          <StatusAlert />
        </div>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Subsections (internal)                                            */
/* ------------------------------------------------------------------ */

function ProfileSubsection() {
  const { state, actions } = useUserProfileContext();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(state.displayName ?? '');

  const [avatarOpen, setAvatarOpen] = useState(false);

  // Only show OAuth provider photos — password provider mirrors the
  // top-level user.photoURL so it's not an independent source
  const providerPhotos = state.providers
    .filter((p) => p.photoURL && p.providerId !== 'password')
    .map((p) => ({
      providerId: p.providerId,
      photoURL: p.photoURL!,
      label: providerLabels[p.providerId] ?? p.providerId,
    }));

  const handleCancel = () => {
    setName(state.displayName ?? '');
    setEditing(false);
  };

  return (
    <div className="px-6 py-4">
      <h3 className="mb-3 text-sm font-medium">Profile</h3>
      <div className="flex items-center gap-4">
        <Popover open={avatarOpen} onOpenChange={setAvatarOpen}>
          <PopoverTrigger asChild>
            <button type="button" className="group relative rounded-full">
              <UserAvatar
                src={state.photoURL}
                name={state.displayName}
                size="lg"
              />
              <div className="absolute inset-0 flex items-center justify-center rounded-full bg-black/50 opacity-0 transition-opacity group-hover:opacity-100">
                <CameraIcon />
              </div>
            </button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-56 p-2">
            <div className="flex flex-col gap-1">
              {providerPhotos.map((p) => {
                const isActive = state.photoURL === p.photoURL;
                return (
                  <button
                    key={p.providerId}
                    type="button"
                    className={cn(
                      'flex items-center gap-3 rounded-md px-2 py-1.5 text-sm hover:bg-muted',
                      isActive && 'bg-muted',
                    )}
                    onClick={async () => {
                      await actions.setPhotoURL(p.photoURL);
                      setAvatarOpen(false);
                    }}
                  >
                    <UserAvatar src={p.photoURL} name={null} size="sm" />
                    <span className="text-muted-foreground">
                      From {p.label}
                    </span>
                  </button>
                );
              })}
              {state.photoURL && (
                <>
                  <Separator />
                  <button
                    type="button"
                    className="flex items-center gap-3 rounded-md px-2 py-1.5 text-sm text-destructive hover:bg-muted"
                    onClick={async () => {
                      await actions.removePhoto();
                      setAvatarOpen(false);
                    }}
                  >
                    Remove photo
                  </button>
                </>
              )}
              {providerPhotos.length === 0 && !state.photoURL && (
                <p className="px-2 py-1.5 text-xs text-muted-foreground">
                  Connect a social account to use its profile photo
                </p>
              )}
            </div>
          </PopoverContent>
        </Popover>
        <div className="flex flex-1 flex-col gap-0.5">
          {editing ? (
            <form
              data-inline-edit
              className="flex items-center gap-2"
              onSubmit={async (e) => {
                e.preventDefault();
                await actions.updateDisplayName(name);
                setEditing(false);
              }}
              onReset={handleCancel}
              onKeyDown={(e) => {
                if (e.key === 'Escape') {
                  e.stopPropagation();
                  handleCancel();
                }
              }}
            >
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="h-7 text-sm"
                autoFocus
              />
              <Button type="submit" size="sm">
                Save
              </Button>
              <Button type="reset" size="sm" variant="ghost">
                Cancel
              </Button>
            </form>
          ) : (
            <button
              type="button"
              className="flex items-center gap-2 text-left"
              onClick={() => setEditing(true)}
            >
              <span className="font-medium">
                {state.displayName || 'No name set'}
              </span>
              <span className="text-xs text-muted-foreground">Edit</span>
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function EmailSubsection() {
  const { state, actions } = useUserProfileContext();
  const [editing, setEditing] = useState(false);
  const [newEmail, setNewEmail] = useState('');

  const handleCancel = () => {
    setNewEmail('');
    setEditing(false);
  };

  return (
    <div className="px-6 py-4">
      <h3 className="mb-3 text-sm font-medium">Email address</h3>
      <div className="flex items-center justify-between rounded-lg border p-3">
        <div className="flex items-center gap-2">
          <span className="text-sm">{state.email}</span>
          {state.emailVerified ? (
            <Badge variant="secondary" className="text-xs">
              Verified
            </Badge>
          ) : (
            <Badge variant="outline" className="text-xs text-destructive">
              Unverified
            </Badge>
          )}
        </div>
        <div className="flex gap-2">
          {!state.emailVerified && (
            <Button
              size="sm"
              variant="ghost"
              onClick={actions.sendVerificationEmail}
            >
              Resend verification
            </Button>
          )}
          {!editing && (
            <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
              Change
            </Button>
          )}
        </div>
      </div>
      {editing && (
        <form
          data-inline-edit
          className="mt-3 flex items-center gap-2"
          onSubmit={async (e) => {
            e.preventDefault();
            await actions.changeEmail(newEmail);
            handleCancel();
          }}
          onReset={handleCancel}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.stopPropagation();
              handleCancel();
            }
          }}
        >
          <Input
            type="email"
            placeholder="New email address"
            value={newEmail}
            onChange={(e) => setNewEmail(e.target.value)}
            className="h-8 text-sm"
            autoFocus
          />
          <Button type="submit" size="sm">
            Save
          </Button>
          <Button type="reset" size="sm" variant="ghost">
            Cancel
          </Button>
        </form>
      )}
    </div>
  );
}

const providerLabels: Record<string, string> = {
  'google.com': 'Google',
  'github.com': 'GitHub',
  'apple.com': 'Apple',
  password: 'Email & Password',
};

const providerIcons: Record<
  string,
  React.ComponentType<{ className?: string }>
> = {
  'google.com': GoogleIcon,
  'github.com': GitHubIcon,
  'apple.com': AppleIcon,
};

function ProviderRow({
  provider,
  canUnlink,
  onUnlink,
}: {
  provider: { providerId: string; email: string | null };
  canUnlink: boolean;
  onUnlink: () => Promise<void>;
}) {
  const [confirming, setConfirming] = useState(false);
  const Icon = providerIcons[provider.providerId];

  return (
    <div className="flex items-center justify-between rounded-lg border p-3">
      <div className="flex items-center gap-3">
        {Icon && <Icon className="size-4" />}
        <div className="flex flex-col gap-0.5">
          <span className="text-sm font-medium">
            {providerLabels[provider.providerId] ?? provider.providerId}
          </span>
          {provider.email && (
            <span className="text-xs text-muted-foreground">
              {provider.email}
            </span>
          )}
        </div>
      </div>
      {canUnlink &&
        (confirming ? (
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setConfirming(false)}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={async () => {
                await onUnlink();
                setConfirming(false);
              }}
            >
              Confirm
            </Button>
          </div>
        ) : (
          <Button size="sm" variant="ghost" onClick={() => setConfirming(true)}>
            Unlink
          </Button>
        ))}
    </div>
  );
}

function ConnectedAccountsSubsection() {
  const { state, actions } = useUserProfileContext();
  const linking = state.linkingProvider;
  const linkingDisplayName = linking
    ? (state.availableProviders.find((p) => p.providerId === linking)?.label ??
      state.providers.find((p) => p.providerId === linking)?.providerId ??
      linking)
    : null;

  return (
    <div className="px-6 py-4">
      <h3 className="mb-3 text-sm font-medium">Connected accounts</h3>
      {linking && (
        <p className="mb-3 text-xs text-muted-foreground">
          Linking {linkingDisplayName} — complete the sign-in in your browser to
          finish. This will expire in 2 minutes.
        </p>
      )}
      <div className="flex flex-col gap-2">
        {state.providers.map((provider) => (
          <ProviderRow
            key={provider.providerId}
            provider={provider}
            canUnlink={
              !linking &&
              provider.providerId !== 'password' &&
              state.providers.length > 1
            }
            onUnlink={() => actions.unlinkProvider(provider.providerId)}
          />
        ))}
        {state.availableProviders.map((provider) => {
          const Icon = providerIcons[provider.providerId];
          return (
            <button
              key={provider.providerId}
              type="button"
              disabled={!!linking}
              className={cn(
                'flex items-center justify-between rounded-lg border border-dashed p-3 text-muted-foreground',
                linking ? 'cursor-not-allowed opacity-50' : 'hover:bg-muted/50',
              )}
              onClick={() => actions.linkProvider(provider.providerId)}
            >
              <div className="flex items-center gap-3">
                {Icon && <Icon className="size-4" />}
                <span className="text-sm">{provider.label}</span>
              </div>
              <span className="text-xs">Connect</span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function DangerSubsection() {
  const { actions } = useUserProfileContext();
  const [confirming, setConfirming] = useState(false);

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
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setConfirming(false)}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={async () => {
                await actions.deleteAccount();
                setConfirming(false);
              }}
            >
              Confirm
            </Button>
          </div>
        ) : (
          <Button
            size="sm"
            variant="destructive"
            onClick={() => setConfirming(true)}
          >
            Delete account
          </Button>
        )}
      </div>
    </div>
  );
}

function SetPasswordSubsection() {
  const { actions } = useUserProfileContext();
  const [setting, setSetting] = useState(false);
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  const handleCancel = () => {
    setSetting(false);
    setNewPassword('');
    setConfirmPassword('');
    setError(null);
  };

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
            e.preventDefault();
            setError(null);
            if (newPassword.length < 6) {
              setError('Password must be at least 6 characters');
              return;
            }
            if (newPassword !== confirmPassword) {
              setError('Passwords do not match');
              return;
            }
            try {
              await actions.setPassword(newPassword);
              setSetting(false);
              setNewPassword('');
              setConfirmPassword('');
            } catch {
              // error surfaced via context
            }
          }}
          onReset={handleCancel}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.stopPropagation();
              handleCancel();
            }
          }}
        >
          <Field>
            <FieldLabel>Password</FieldLabel>
            <Input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              autoFocus
            />
          </Field>
          <Field>
            <FieldLabel>Confirm password</FieldLabel>
            <Input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
            />
          </Field>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div className="flex gap-2">
            <Button type="submit" size="sm">
              Set password
            </Button>
            <Button type="reset" size="sm" variant="ghost">
              Cancel
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}

function PasswordSubsection() {
  const { state, actions } = useUserProfileContext();
  const [changing, setChanging] = useState(false);
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  const hasPasswordProvider = state.providers.some(
    (p) => p.providerId === 'password',
  );

  const handleCancel = () => {
    setChanging(false);
    setCurrentPassword('');
    setNewPassword('');
    setConfirmPassword('');
    setError(null);
  };

  if (!hasPasswordProvider) {
    return <SetPasswordSubsection />;
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
            e.preventDefault();
            setError(null);
            if (newPassword !== confirmPassword) {
              setError('Passwords do not match');
              return;
            }
            try {
              await actions.changePassword(currentPassword, newPassword);
              handleCancel();
            } catch {
              setError('Failed to change password');
            }
          }}
          onReset={handleCancel}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.stopPropagation();
              handleCancel();
            }
          }}
        >
          <Field>
            <FieldLabel>Current password</FieldLabel>
            <Input
              type="password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              autoFocus
            />
          </Field>
          <Field>
            <FieldLabel>New password</FieldLabel>
            <Input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel>Confirm new password</FieldLabel>
            <Input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
            />
          </Field>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div className="flex gap-2">
            <Button type="submit" size="sm">
              Update password
            </Button>
            <Button type="reset" size="sm" variant="ghost">
              Cancel
            </Button>
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
  );
}

function MFASubsection() {
  const { state, actions } = useUserProfileContext();
  const [otpCode, setOtpCode] = useState('');
  const [showVerify, setShowVerify] = useState(false);
  const [confirming, setConfirming] = useState(false);

  // Not enrolled, no enrollment in progress
  if (!state.mfaEnrolled && !state.totpEnrollment) {
    return (
      <div className="px-6 py-4">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-sm font-medium">Two-factor authentication</h3>
            <p className="text-xs text-muted-foreground">
              Add an extra layer of security with an authenticator app
            </p>
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={actions.startTotpEnrollment}
          >
            Enable
          </Button>
        </div>
      </div>
    );
  }

  // Enrollment in progress
  if (state.totpEnrollment) {
    return (
      <div className="px-6 py-4" data-inline-edit>
        <h3 className="mb-3 text-sm font-medium">Set up authenticator app</h3>
        {!showVerify ? (
          <div className="flex flex-col items-center gap-4">
            <QRCodeSVG value={state.totpEnrollment.qrUrl} size={180} />
            <div className="w-full">
              <p className="mb-1 text-xs text-muted-foreground">
                Can&apos;t scan? Enter this key manually:
              </p>
              <code className="block break-all rounded bg-muted px-3 py-2 text-xs">
                {state.totpEnrollment.secret}
              </code>
            </div>
            <div className="flex w-full gap-2">
              <Button size="sm" onClick={() => setShowVerify(true)}>
                Next
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setShowVerify(false);
                  actions.cancelTotpEnrollment();
                }}
              >
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-col items-center gap-4">
            <p className="text-sm text-muted-foreground">
              Enter the 6-digit code from your authenticator app
            </p>
            <InputOTP maxLength={6} value={otpCode} onChange={setOtpCode}>
              <InputOTPGroup>
                <InputOTPSlot index={0} />
                <InputOTPSlot index={1} />
                <InputOTPSlot index={2} />
                <InputOTPSlot index={3} />
                <InputOTPSlot index={4} />
                <InputOTPSlot index={5} />
              </InputOTPGroup>
            </InputOTP>
            <div className="flex gap-2">
              <Button
                size="sm"
                disabled={otpCode.length !== 6}
                onClick={async () => {
                  await actions.verifyTotpEnrollment(otpCode);
                  setOtpCode('');
                  setShowVerify(false);
                }}
              >
                Verify
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setShowVerify(false);
                  setOtpCode('');
                }}
              >
                Back
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setShowVerify(false);
                  setOtpCode('');
                  actions.cancelTotpEnrollment();
                }}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}
      </div>
    );
  }

  // Enrolled
  return (
    <div className="px-6 py-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium">Two-factor authentication</h3>
          <Badge variant="secondary" className="text-xs">
            Enabled
          </Badge>
        </div>
        {confirming ? (
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setConfirming(false)}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={async () => {
                await actions.unenrollTotp();
                setConfirming(false);
              }}
            >
              Confirm
            </Button>
          </div>
        ) : (
          <Button
            size="sm"
            variant="outline"
            onClick={() => setConfirming(true)}
          >
            Disable
          </Button>
        )}
      </div>
    </div>
  );
}

function StatusAlert() {
  const { state, actions } = useUserProfileContext();
  const timerRef = useRef<ReturnType<typeof setTimeout>>(null);

  useEffect(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    if (state.success) {
      timerRef.current = setTimeout(actions.clearStatus, 3000);
    }
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [state.success, actions]);

  if (!state.error && !state.success) return null;

  const isError = !!state.error;

  return (
    <div className="sticky bottom-0 bg-background px-6 py-3">
      <Alert
        variant={isError ? 'destructive' : 'default'}
        className={
          isError
            ? 'border-error/30 bg-error/10 text-error'
            : 'border-success/30 bg-success/10 text-success'
        }
      >
        <AlertDescription className={isError ? 'text-error' : 'text-success'}>
          {state.error ?? state.success}
        </AlertDescription>
        <AlertAction>
          <button
            type="button"
            className="rounded-sm p-0.5 opacity-70 hover:opacity-100"
            onClick={actions.clearStatus}
          >
            <XIcon />
            <span className="sr-only">Dismiss</span>
          </button>
        </AlertAction>
      </Alert>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Icons                                                             */
/* ------------------------------------------------------------------ */

function XIcon() {
  return (
    <svg
      className="size-3.5"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M18 6 6 18" />
      <path d="m6 6 12 12" />
    </svg>
  );
}

function CameraIcon() {
  return (
    <svg
      className="size-4 text-white"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M14.5 4h-5L7 7H4a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2h-3l-2.5-3z" />
      <circle cx="12" cy="13" r="3" />
    </svg>
  );
}

function UserIcon() {
  return (
    <svg
      className="size-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2" />
      <circle cx="12" cy="7" r="4" />
    </svg>
  );
}

function ShieldIcon() {
  return (
    <svg
      className="size-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z" />
      <path d="m9 12 2 2 4-4" />
    </svg>
  );
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
};
