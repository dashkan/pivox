import type { RefObject } from 'react';
import type { UIMessage } from '@tanstack/ai-react';

export interface ChatState {
  messages: Array<UIMessage>;
  input: string;
  isLoading: boolean;
  error: Error | undefined;
}

export interface ChatActions {
  setInput: (value: string) => void;
  submit: () => void;
}

export interface ChatMeta {
  viewportRef: RefObject<HTMLDivElement | null>;
  canSubmit: boolean;
}

export interface ChatContextValue {
  state: ChatState;
  actions: ChatActions;
  meta: ChatMeta;
}
