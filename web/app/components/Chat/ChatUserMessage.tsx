import type { UIMessage } from '@tanstack/ai-react';
import { Paper, Text } from '@mantine/core';
import classes from './Chat.module.css';

interface ChatUserMessageProps {
  message: UIMessage;
}

export function ChatUserMessage({ message }: ChatUserMessageProps) {
  return (
    <div className={classes.userRow}>
      <Paper bg="blue" c="white" py="sm" px="md" radius="lg" maw="75%">
        {message.parts.map((part, idx) =>
          part.type === 'text' ? (
            <Text key={idx} size="sm" style={{ whiteSpace: 'pre-wrap' }}>
              {part.content}
            </Text>
          ) : null
        )}
      </Paper>
    </div>
  );
}
