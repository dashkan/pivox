'use client';

import { IconSend } from '@tabler/icons-react';
import { ActionIcon, Box, Divider, Group, TextInput } from '@mantine/core';
import { useChatContext } from './Chat.context';

export function ChatInput() {
  const { state, actions, meta } = useChatContext();

  return (
    <>
      <Divider />
      <Box p="md" bg="var(--mantine-color-body)">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            actions.submit();
          }}
        >
          <Group gap="sm" align="flex-end" wrap="nowrap" maw={768} mx="auto">
            <TextInput
              value={state.input}
              onChange={(e) => actions.setInput(e.currentTarget.value)}
              placeholder="Send a message..."
              disabled={state.isLoading}
              flex={1}
            />
            <ActionIcon
              type="submit"
              variant="filled"
              size="input-sm"
              disabled={!meta.canSubmit}
              aria-label="Send message"
            >
              <IconSend size={16} />
            </ActionIcon>
          </Group>
        </form>
      </Box>
    </>
  );
}
