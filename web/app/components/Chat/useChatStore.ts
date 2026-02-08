'use client';

import { useEffect, useRef } from 'react';
import type { AnyClientTool } from '@tanstack/ai';
import { type ConnectionAdapter, useChat } from '@tanstack/ai-react';
import { create } from 'zustand';
import { devtools } from 'zustand/middleware';
import {
  canSubmit,
  createFileAttachment,
  revokeAllFileAttachments,
  revokeFileAttachment,
  submitMessage,
} from './Chat.files';
import type { ChatContextValue, FileAttachment } from './Chat.types';

const useChatUIStore = create<{
  input: string;
  setInput: (value: string) => void;
  clearInput: () => void;
  files: FileAttachment[];
  addFiles: (newFiles: File[]) => void;
  removeFile: (id: string) => void;
  clearFiles: () => void;
}>()(
  devtools(
    (set) => ({
      input: '',
      setInput: (value) => set({ input: value }, undefined, 'setInput'),
      clearInput: () => set({ input: '' }, undefined, 'clearInput'),
      files: [],
      addFiles: (newFiles) =>
        set(
          (state) => ({ files: [...state.files, ...newFiles.map(createFileAttachment)] }),
          undefined,
          'addFiles'
        ),
      removeFile: (id) =>
        set(
          (state) => {
            const file = state.files.find((f) => f.id === id);
            if (file) {
              revokeFileAttachment(file);
            }
            return { files: state.files.filter((f) => f.id !== id) };
          },
          undefined,
          'removeFile'
        ),
      clearFiles: () =>
        set(
          (state) => {
            revokeAllFileAttachments(state.files);
            return { files: [] };
          },
          undefined,
          'clearFiles'
        ),
    }),
    { name: 'ChatUIStore' }
  )
);

interface UseChatStoreOptions {
  connection: ConnectionAdapter;
  tools?: AnyClientTool[];
  initialMessages?: Array<import('@tanstack/ai-react').UIMessage>;
}

export function useChatStore({ connection, tools, initialMessages }: UseChatStoreOptions): ChatContextValue {
  const { input, setInput, clearInput, files, addFiles, removeFile, clearFiles } = useChatUIStore();
  const viewportRef = useRef<HTMLDivElement>(null);

  const { messages, sendMessage, setMessages, stop, isLoading, error } = useChat({
    connection,
    tools,
    initialMessages,
  });

  useEffect(() => {
    if (messages.length > 0) {
      requestAnimationFrame(() => {
        viewportRef.current?.scrollTo({ top: viewportRef.current.scrollHeight });
      });
    }
  }, [messages]);

  const submit = async () => {
    // Read latest values from the store to avoid stale closure
    // (e.g. when voice input calls setInput then submit in the same tick)
    const { input: currentInput, files: currentFiles } = useChatUIStore.getState();
    const sent = await submitMessage({
      input: currentInput,
      files: currentFiles,
      isLoading,
      sendMessage,
    });
    if (sent) {
      clearInput();
      if (currentFiles.length > 0) {
        clearFiles();
      }
    }
  };

  return {
    state: { messages, input, isLoading, error, files },
    actions: { setInput, submit, stop, setMessages, addFiles, removeFile },
    meta: { viewportRef, canSubmit: canSubmit(input, files, isLoading) },
  };
}
