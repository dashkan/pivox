'use client';

import { IconAlertCircle } from '@tabler/icons-react';
import { Alert, Box } from '@mantine/core';
import { useChatContext } from './Chat.context';

export function ChatErrorAlert() {
  const { state } = useChatContext();

  if (!state.error) {
    return null;
  }

  return (
    <Box maw={768} mx="auto" px="md" w="100%">
      <Alert color="red" icon={<IconAlertCircle size={16} />}>
        {state.error.message}
      </Alert>
    </Box>
  );
}
