import type { TextPart, ThinkingPart, ToolCallPart } from '@tanstack/ai';
import { Group, Loader, Paper, Text } from '@mantine/core';
import type { PartRendererProps, PartsMap } from './Chat.types';
import classes from './Chat.module.css';

export function TextPartRenderer({ part }: PartRendererProps<TextPart>) {
  return (
    <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
      {part.content}
    </Text>
  );
}

export function ThinkingPartRenderer({ part }: PartRendererProps<ThinkingPart>) {
  return (
    <Paper className={classes.thinking} p="xs" mb="xs" withBorder radius="md">
      <Text size="sm" c="dimmed" fs="italic">
        {part.content}
      </Text>
    </Paper>
  );
}

export function ToolCallPartRenderer({ part }: PartRendererProps<ToolCallPart>) {
  return (
    <Group gap="xs" py="xs">
      <Loader size="xs" />
      <Text size="sm" c="dimmed">
        Running {part.name}...
      </Text>
    </Group>
  );
}

export const defaultParts: PartsMap = {
  text: TextPartRenderer as PartsMap['text'],
  thinking: ThinkingPartRenderer as PartsMap['thinking'],
};
