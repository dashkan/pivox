import { IconFile } from '@tabler/icons-react';
import type { DocumentPart, ImagePart, TextPart, ThinkingPart, ToolCallPart } from '@tanstack/ai';
import { Group, Image, Loader, Paper, Text } from '@mantine/core';
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

export function ImagePartRenderer({ part }: PartRendererProps<ImagePart>) {
  const src =
    part.source.type === 'data'
      ? `data:${part.source.mimeType};base64,${part.source.value}`
      : part.source.value;
  return <Image src={src} alt="attached image" w={120} h={120} fit="cover" radius="sm" />;
}

export function DocumentPartRenderer({ part }: PartRendererProps<DocumentPart>) {
  return (
    <Paper bg="rgba(255,255,255,0.15)" radius="sm" px="xs" py={4}>
      <Group gap={4} wrap="nowrap">
        <IconFile size={14} />
        <Text size="xs" c="dimmed">
          {part.source.mimeType ?? 'Document'}
        </Text>
      </Group>
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
  image: ImagePartRenderer as PartsMap['image'],
  document: DocumentPartRenderer as PartsMap['document'],
};
