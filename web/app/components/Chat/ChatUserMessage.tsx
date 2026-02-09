import type { UIMessage } from '@tanstack/ai-react';
import { Box, Paper } from '@mantine/core';
import type { PartsMap } from './Chat.types';
import classes from './Chat.module.css';

interface ChatUserMessageProps {
  message: UIMessage;
  parts: PartsMap;
}

export function ChatUserMessage({ message, parts }: ChatUserMessageProps) {
  return (
    <Box className={classes.userRow}>
      <Paper
        bg="var(--mantine-primary-color-filled)"
        c="white"
        py="sm"
        px="md"
        radius="lg"
        maw="75%"
      >
        {message.parts.map((part, idx) => {
          const Renderer = parts[part.type];
          return Renderer ? <Renderer key={idx} part={part} messageRole="user" /> : null;
        })}
      </Paper>
    </Box>
  );
}
