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
  stop: () => void;
  setMessages: (messages: Array<UIMessage>) => void;
  addFiles: (files: File[]) => void;
  removeFile: (id: string) => void;
}

export type WaveformMode = 'bars' | 'wave';

export interface VoiceInput {
  isSupported: boolean;
  isRecording: boolean;
  transcript: string;
  analyser: AnalyserNode | null;
  waveformMode: WaveformMode;
  start: () => void;
  stop: () => void;
  toggleWaveformMode: () => void;
}

export interface ChatMeta {
  viewportRef: RefObject<HTMLDivElement | null>;
  canSubmit: boolean;
  voice?: VoiceInput;
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
