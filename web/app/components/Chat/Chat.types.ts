import type { ComponentType, RefObject } from 'react';
import type { MessagePart, ToolCallPart, ToolResultPart } from '@tanstack/ai';
import type { UIMessage } from '@tanstack/ai-react';

export interface FileAttachment {
  id: string;
  file: File;
  name: string;
  type: string;
  size: number;
  previewUrl?: string;
}

export interface SerializedFile {
  name: string;
  type: string;
  size: number;
  data: string;
}

export interface ChatState {
  messages: Array<UIMessage>;
  input: string;
  isLoading: boolean;
  error: Error | undefined;
  files: FileAttachment[];
}

export interface ChatActions {
  setInput: (value: string) => void;
  submit: () => void;
  addFiles: (files: File[]) => void;
  removeFile: (id: string) => void;
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

export interface PartRendererProps<T extends MessagePart = MessagePart> {
  part: T;
  messageRole: 'user' | 'assistant';
}

export type PartRenderer<T extends MessagePart = MessagePart> = ComponentType<PartRendererProps<T>>;

export type PartsMap = Partial<Record<MessagePart['type'], PartRenderer>>;

export interface ToolPartRendererProps {
  toolName: string;
  callPart: ToolCallPart;
  resultPart?: ToolResultPart;
  messageRole: 'user' | 'assistant';
}

export type ToolPartRenderer = ComponentType<ToolPartRendererProps>;

export type ToolPartsMap = Record<string, ToolPartRenderer>;
