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

// The Electron renderer is cross-origin to the API (the desktop app
// loads from file:// in packaged builds and http://localhost:5173 in
// dev). Both paths send chat requests at a configured remote — the
// same VITE_BASE_URL env var the API client reads. Empty fallback
// matches the rest of the renderer's transport plumbing
// (lib/api-client.ts, lib/electron-redirect-transport.ts).
const BASE_URL = import.meta.env.VITE_BASE_URL ?? 'https://pivox.ngrok.app';

function ChatPage() {
  const { state: shellState } = useAppShellContext();
  const { user: firebaseUser } = useAuth();
  const pivoxUserId = usePivoxUserId();

  const activeOrg = shellState.activeOrganization;

  const getAuthToken = useCallback(async () => {
    if (!firebaseUser) {
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
    <ChatFeature parent={parent} baseUrl={BASE_URL} getAuthToken={getAuthToken}>
      <Chat.Thread />
      <Chat.Input />
    </ChatFeature>
  );
}
