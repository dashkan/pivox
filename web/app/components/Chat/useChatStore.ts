'use client';

import { useEffect, useRef } from 'react';
import { fetchServerSentEvents, useChat } from '@tanstack/ai-react';
import { create } from 'zustand';
import { devtools } from 'zustand/middleware';
import type { ChatContextValue } from './Chat.types';

const useChatUIStore = create<{
  input: string;
  setInput: (value: string) => void;
  clearInput: () => void;
}>()(
  devtools(
    (set) => ({
      input: '',
      setInput: (value) => set({ input: value }, undefined, 'setInput'),
      clearInput: () => set({ input: '' }, undefined, 'clearInput'),
    }),
    { name: 'ChatUIStore' }
  )
);

export function useChatStore(endpoint = '/api/chat'): ChatContextValue {
  const { input, setInput, clearInput } = useChatUIStore();
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
      clearInput();
    }
  };

  return {
    state: { messages, input, isLoading, error },
    actions: { setInput, submit },
    meta: { viewportRef, canSubmit: input.trim() !== '' && !isLoading },
  };
}
