import type { UIMessage } from '@tanstack/ai-react';
import { Box } from '@mantine/core';
import type { PartsMap, ToolPartsMap } from './Chat.types';
import { ChatAssistantMessage } from './ChatAssistantMessage';
import { ChatUserMessage } from './ChatUserMessage';

interface ChatMessageItemProps {
  index: number;
  message: UIMessage;
  measureElement: (node: Element | null | undefined) => void;
  parts: PartsMap;
  toolParts?: ToolPartsMap;
}

export function ChatMessageItem({
  index,
  message,
  measureElement,
  parts,
  toolParts,
}: ChatMessageItemProps) {
  return (
    <Box data-index={index} ref={measureElement} py="xs" px="md">
      {message.role === 'user' ? (
        <ChatUserMessage message={message} parts={parts} />
      ) : (
        <ChatAssistantMessage message={message} parts={parts} toolParts={toolParts} />
      )}
    </Box>
  );
}
