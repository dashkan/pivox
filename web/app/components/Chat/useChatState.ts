'use client';

import { useEffect, useRef, useState } from 'react';
import { fetchServerSentEvents, useChat } from '@tanstack/ai-react';
import type { ChatContextValue } from './Chat.types';

export function useChatState(endpoint = '/api/chat'): ChatContextValue {
  const [input, setInput] = useState('');
  const viewportRef = useRef<HTMLDivElement>(null);

  const { messages, sendMessage, isLoading, error } = useChat({
    connection: fetchServerSentEvents(endpoint),
  });

  useEffect(() => {
    if (messages.length > 0) {
      requestAnimationFrame(() => {
        viewportRef.current?.scrollTo({ top: viewportRef.current.scrollHeight });
      });
    }
  }, [messages]);

  const submit = () => {
    if (input.trim() && !isLoading) {
      sendMessage(input);
      setInput('');
    }
  };

  return {
    state: { messages, input, isLoading, error },
    actions: { setInput, submit },
    meta: { viewportRef, canSubmit: input.trim() !== '' && !isLoading },
  };
}
