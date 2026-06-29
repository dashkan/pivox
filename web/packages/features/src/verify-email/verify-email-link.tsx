'use client';

import { useFirebaseUser } from '@/auth/firebase-user';

/**
 * Soft nag link rendered in the app shell when the signed-in user
 * hasn't verified their email yet. Returns null when there's no user
 * or when the user is already verified — so consumers can drop it
 * unconditionally into the header without an outer guard.
 *
 * Registration no longer redirects to the verify-email screen on
 * success; this link is the discoverable affordance to get there
 * when the user wants to verify.
 */
export function VerifyEmailLink({
  onClick,
  className,
}: {
  onClick: () => void;
  className?: string;
}) {
  const { user } = useFirebaseUser();
  if (!user || user.emailVerified) return null;
  return (
    <button
      type="button"
      onClick={onClick}
      className={
        className ??
        'text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline'
      }
    >
      Verify your email
    </button>
  );
}
