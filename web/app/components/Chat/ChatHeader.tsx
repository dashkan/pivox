'use client';

import { IconPlus } from '@tabler/icons-react';
import { ActionIcon, Group, Tooltip } from '@mantine/core';
import { useChatContext } from './Chat.context';

export function ChatHeader() {
  const { state, actions } = useChatContext();

  return (
    <Group h={48} px="md" justify="flex-end">
      <Tooltip label="New conversation">
        <ActionIcon
          variant="subtle"
          size="sm"
          onClick={actions.clear}
          disabled={state.isLoading || state.messages.length === 0}
          aria-label="New conversation"
        >
          <IconPlus size={16} />
        </ActionIcon>
      </Tooltip>
    </Group>
  );
}
