'use client';

import { createContext, use } from 'react';

import type { ChatContextValue } from './chat.types';

/**
 * Chat context. Mirrors ImageEditorContext / AppShellContext.
 *
 * Set by `Chat.Provider` (the namespace's provider component) or by
 * `useChatState` (when callers want to pipe the value into a custom
 * provider tree). Consumed by every Chat subcomponent that needs
 * Pivox-side chat state — assistant-ui's runtime is exposed via meta
 * and the inner AssistantRuntimeProvider drives the rest.
 */
export const ChatContext = createContext<ChatContextValue | null>(null);

export function useChatContext(): ChatContextValue {
  const ctx = use(ChatContext);
  if (!ctx) {
    throw new Error(
      'Chat subcomponents must be used within a Chat.Provider',
    );
  }
  return ctx;
}
