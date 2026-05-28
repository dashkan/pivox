import { organizationId } from '@pivox/client';
import { useAuth, usePivoxUserId } from '@pivox/features/auth';
import { ChatFeature } from '@pivox/features/chat';
import { useAppShellContext } from '@pivox/ui/app-shell';
import { Chat } from '@pivox/ui/chat';
import { createFileRoute } from '@tanstack/react-router';
import { useCallback } from 'react';

export const Route = createFileRoute('/_app/chat')({
  component: ChatPage,
});

function ChatPage() {
  const { state: shellState } = useAppShellContext();
  const { user: firebaseUser } = useAuth();
  const pivoxUserId = usePivoxUserId();

  // The chat needs three things to authorize and route:
  //   - active org slug         (AppShell context, user-picked)
  //   - Pivox user UUID         (Firebase ID-token custom claim,
  //                              read once per token rotation)
  //   - Firebase ID token getter (AuthProvider context)
  //
  // Same hook surface as the Electron renderer (web/apps/electron/.../
  // _app/chat.tsx) so the two apps share both the transport AND the
  // Pivox-side identity wiring — start's server-side claim extraction
  // is the SSR optimization, not a different source of truth.
  const activeOrg = shellState.activeOrganization;

  const getAuthToken = useCallback(async () => {
    if (!firebaseUser) {
      // The _app gate already verified an authenticated session;
      // reaching here without a Firebase user means the client
      // hasn't hydrated yet. Throwing surfaces as a stream-side
      // error chunk the user can retry from.
      throw new Error('Firebase user not available');
    }
    return firebaseUser.getIdToken();
  }, [firebaseUser]);

  if (!activeOrg || !pivoxUserId) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        {pivoxUserId === undefined
          ? 'Loading…'
          : 'Pick an organization to start a chat.'}
      </div>
    );
  }

  const orgSlug = organizationId(activeOrg);
  const parent = `organizations/${orgSlug}/users/${pivoxUserId}`;

  return (
    <ChatFeature parent={parent} getAuthToken={getAuthToken}>
      <Chat.Root>
        <Chat.Viewport>
          <Chat.Empty>
            <p className="text-sm">Start the conversation…</p>
          </Chat.Empty>
          <Chat.Messages />
        </Chat.Viewport>
        <Chat.Composer />
      </Chat.Root>
    </ChatFeature>
  );
}
