'use client';

import type { ReactNode } from 'react';
import { ChatContext } from './Chat.context';
import type { ChatActions, ChatMeta, ChatState } from './Chat.types';

interface ChatProviderProps {
  children: ReactNode;
  state: ChatState;
  actions: ChatActions;
  meta: ChatMeta;
}

export function ChatProvider({ children, state, actions, meta }: ChatProviderProps) {
  return <ChatContext value={{ state, actions, meta }}>{children}</ChatContext>;
}
