import { ChatProvider } from './Chat.provider';
import { ChatAssistantMessage } from './ChatAssistantMessage';
import { ChatEmptyState } from './ChatEmptyState';
import { ChatErrorAlert } from './ChatErrorAlert';
import { ChatFrame } from './ChatFrame';
import { ChatInput } from './ChatInput';
import { ChatMessageList } from './ChatMessageList';
import { defaultParts } from './ChatParts';
import { defaultToolParts, GenericToolResult, ImageToolResult } from './ChatToolParts';
import { ChatUserMessage } from './ChatUserMessage';

export const Chat = {
  Provider: ChatProvider,
  Frame: ChatFrame,
  EmptyState: ChatEmptyState,
  MessageList: ChatMessageList,
  ErrorAlert: ChatErrorAlert,
  Input: ChatInput,
  UserMessage: ChatUserMessage,
  AssistantMessage: ChatAssistantMessage,
  ImageToolResult,
  GenericToolResult,
  defaultParts,
  defaultToolParts,
};
