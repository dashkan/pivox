'use client';

import { useCallback, type ReactNode } from 'react';
import type { AnyClientTool } from '@tanstack/ai';
import type { UIMessage } from '@tanstack/ai-react';
import { ChatContext } from './Chat.context';
import { useChatStore } from './useChatStore';
import { useVoiceInput } from './useVoiceInput';

interface ChatProviderProps {
  children: ReactNode;
  tools?: AnyClientTool[];
  endpoint?: string;
  initialMessages?: Array<UIMessage>;
}

export function ChatProvider({ children, tools, endpoint, initialMessages }: ChatProviderProps) {
  const { state, actions, meta } = useChatStore({ tools, endpoint, initialMessages });

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
