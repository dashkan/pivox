'use client';

import { useCallback, type ReactNode } from 'react';
import type { AnyClientTool } from '@tanstack/ai';
import type { ConnectionAdapter, UIMessage } from '@tanstack/ai-react';
import { ChatContext } from './Chat.context';
import { useChatStore } from './useChatStore';
import { useVoiceInput } from './useVoiceInput';

interface ChatProviderProps {
  children: ReactNode;
  connection: ConnectionAdapter;
  tools?: AnyClientTool[];
  initialMessages?: Array<UIMessage>;
}

export function ChatProvider({ children, connection, tools, initialMessages }: ChatProviderProps) {
  const { state, actions, meta } = useChatStore({ connection, tools, initialMessages });

  const handleVoiceTranscript = useCallback(
    (text: string) => {
      actions.setInput(text);
      requestAnimationFrame(() => actions.submit());
    },
    [actions]
  );

  const voice = useVoiceInput({ onTranscript: handleVoiceTranscript });

  return <ChatContext value={{ state, actions, meta: { ...meta, voice } }}>{children}</ChatContext>;
}
