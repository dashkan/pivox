'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchServerSentEvents, useChat } from '@tanstack/ai-react';
import {
  canSubmit,
  createFileAttachment,
  revokeAllFileAttachments,
  revokeFileAttachment,
  submitMessage,
} from './Chat.files';
import type { ChatContextValue, FileAttachment } from './Chat.types';

export function useChatState(endpoint = '/api/chat'): ChatContextValue {
  const [input, setInput] = useState('');
  const [files, setFiles] = useState<FileAttachment[]>([]);
  const viewportRef = useRef<HTMLDivElement>(null);

  // Keep refs in sync so submit() always reads the latest values
  // (avoids stale closure when voice input calls setInput then submit in the same tick)
  const inputRef = useRef(input);
  inputRef.current = input;
  const filesRef = useRef(files);
  filesRef.current = files;

  const { messages, sendMessage, append, isLoading, error } = useChat({
    connection: fetchServerSentEvents(endpoint),
  });

  useEffect(() => {
    if (messages.length > 0) {
      requestAnimationFrame(() => {
        viewportRef.current?.scrollTo({ top: viewportRef.current.scrollHeight });
      });
    }
  }, [messages]);

  const addFiles = useCallback((newFiles: File[]) => {
    setFiles((prev) => [...prev, ...newFiles.map(createFileAttachment)]);
  }, []);

  const removeFile = useCallback((id: string) => {
    setFiles((prev) => {
      const file = prev.find((f) => f.id === id);
      if (file) {
        revokeFileAttachment(file);
      }
      return prev.filter((f) => f.id !== id);
    });
  }, []);

  const clearFiles = useCallback(() => {
    setFiles((prev) => {
      revokeAllFileAttachments(prev);
      return [];
    });
  }, []);

  const submit = async () => {
    const currentInput = inputRef.current;
    const currentFiles = filesRef.current;
    const sent = await submitMessage({
      input: currentInput,
      files: currentFiles,
      isLoading,
      sendMessage,
      append,
    });
    if (sent) {
      setInput('');
      if (currentFiles.length > 0) {
        clearFiles();
      }
    }
  };

  return {
    state: { messages, input, isLoading, error, files },
    actions: { setInput, submit, addFiles, removeFile },
    meta: { viewportRef, canSubmit: canSubmit(input, files, isLoading) },
  };
}
