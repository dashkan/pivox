import { ChatProvider } from './Chat.provider';
import { ChatEmptyState } from './ChatEmptyState';
import { ChatErrorAlert } from './ChatErrorAlert';
import { ChatFrame } from './ChatFrame';
import { ChatInput } from './ChatInput';
import { ChatMessageList } from './ChatMessageList';

export const Chat = {
  Provider: ChatProvider,
  Frame: ChatFrame,
  EmptyState: ChatEmptyState,
  MessageList: ChatMessageList,
  ErrorAlert: ChatErrorAlert,
  Input: ChatInput,
};
