import type { UIMessage } from '@tanstack/ai-react';
import { Box } from '@mantine/core';
import { ChatAssistantMessage } from './ChatAssistantMessage';
import { ChatUserMessage } from './ChatUserMessage';

interface ChatMessageItemProps {
  index: number;
  message: UIMessage;
  measureElement: (node: Element | null | undefined) => void;
}

export function ChatMessageItem({ index, message, measureElement }: ChatMessageItemProps) {
  return (
    <Box data-index={index} ref={measureElement} py="xs" px="md">
      {message.role === 'user' ? (
        <ChatUserMessage message={message} />
      ) : (
        <ChatAssistantMessage message={message} />
      )}
    </Box>
  );
}
