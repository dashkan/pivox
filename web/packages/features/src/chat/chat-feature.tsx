'use client';

import { Chat } from '@pivox/ui/chat';

import { useChatFeature } from './use-chat-feature';

import type { UseChatFeatureOptions } from './use-chat-feature';

/**
 * ChatFeature wires the Pivox chat surface — mirror of
 * `ImageEditorFeature`. Builds the Chat context value via
 * `useChatFeature` (currently a pass-through to `useChatState`) and
 * provides it to children via `Chat.Provider`.
 *
 * Usage in a route:
 *
 *   <ChatFeature
 *     parent={`organizations/${orgSlug}/users/${userId}`}
 *     getAuthToken={getFirebaseIdToken}
 *   >
 *     <Chat.Root>
 *       <Chat.Viewport>
 *         <Chat.Empty>Start the conversation…</Chat.Empty>
 *         <Chat.Messages />
 *       </Chat.Viewport>
 *       <Chat.Composer />
 *     </Chat.Root>
 *   </ChatFeature>
 */
export function ChatFeature({
  children,
  ...options
}: UseChatFeatureOptions & {
  children: React.ReactNode;
}) {
  const value = useChatFeature(options);

  return <Chat.Provider value={value}>{children}</Chat.Provider>;
}
