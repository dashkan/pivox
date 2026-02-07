'use client';

import { Center, Text } from '@mantine/core';
import { useChatContext } from './Chat.context';

export function ChatEmptyState() {
  const { state } = useChatContext();

  if (state.messages.length > 0) {
    return null;
  }

  return (
    <Center flex={1}>
      <Text c="dimmed" size="sm">
        Send a message to start a conversation.
      </Text>
    </Center>
  );
}
