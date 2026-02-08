'use client';

import { useCallback, type ReactNode } from 'react';
import type { AnyClientTool } from '@tanstack/ai';
import { ChatContext } from './Chat.context';
import { useChatStore } from './useChatStore';
import { useVoiceInput } from './useVoiceInput';

interface ChatProviderProps {
  children: ReactNode;
  tools?: AnyClientTool[];
  endpoint?: string;
}

export function ChatProvider({ children, tools, endpoint }: ChatProviderProps) {
  const { state, actions, meta } = useChatStore({ tools, endpoint });

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
