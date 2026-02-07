import { createContext, use } from 'react';
import type { ChatContextValue } from './Chat.types';

export const ChatContext = createContext<ChatContextValue | null>(null);

export function useChatContext(): ChatContextValue {
  const ctx = use(ChatContext);
  if (!ctx) {
    throw new Error('Chat compound components must be used within <ChatProvider>');
  }
  return ctx;
}
