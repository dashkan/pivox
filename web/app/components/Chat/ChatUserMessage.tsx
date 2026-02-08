import { IconFile } from '@tabler/icons-react';
import type { UIMessage } from '@tanstack/ai-react';
import { Box, Group, Image, Paper, Text } from '@mantine/core';
import type { PartsMap, SerializedFile } from './Chat.types';
import classes from './Chat.module.css';

interface ChatUserMessageProps {
  message: UIMessage;
  parts: PartsMap;
}

export function ChatUserMessage({ message, parts }: ChatUserMessageProps) {
  const files = (message as UIMessage & { _files?: SerializedFile[] })._files;

  return (
    <Box className={classes.userRow}>
      <Paper bg="var(--mantine-primary-color-filled)" c="white" py="sm" px="md" radius="lg" maw="75%">
        {files && files.length > 0 && (
          <Group gap="xs" wrap="wrap" mb="xs">
            {files.map((file, idx) =>
              file.type.startsWith('image/') ? (
                <Image
                  key={idx}
                  src={`data:${file.type};base64,${file.data}`}
                  alt={file.name}
                  w={120}
                  h={120}
                  fit="cover"
                  radius="sm"
                />
              ) : (
                <Paper key={idx} bg="rgba(255,255,255,0.15)" radius="sm" px="xs" py={4}>
                  <Group gap={4} wrap="nowrap">
                    <IconFile size={14} />
                    <Text size="xs" maw={120} truncate>
                      {file.name}
                    </Text>
                  </Group>
                </Paper>
              )
            )}
          </Group>
        )}
        {message.parts.map((part, idx) => {
          const Renderer = parts[part.type];
          return Renderer ? <Renderer key={idx} part={part} messageRole="user" /> : null;
        })}
      </Paper>
    </Box>
  );
}
