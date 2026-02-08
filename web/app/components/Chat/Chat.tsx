import { ChatProvider } from './Chat.provider';
import { ChatAssistantMessage } from './ChatAssistantMessage';
import { ChatEmptyState } from './ChatEmptyState';
import { ChatErrorAlert } from './ChatErrorAlert';
import { ChatFrame } from './ChatFrame';
import { ChatHeader } from './ChatHeader';
import { ChatInput } from './ChatInput';
import { ChatMessageList } from './ChatMessageList';
import { defaultParts } from './ChatParts';
import { defaultToolParts, GenericToolResult, ImageToolResult } from './ChatToolParts';
import { ChatUserMessage } from './ChatUserMessage';

export const Chat = {
  Provider: ChatProvider,
  Frame: ChatFrame,
  Header: ChatHeader,
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
