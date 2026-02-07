import type { UIMessage } from '@tanstack/ai-react';
import { Paper, Text } from '@mantine/core';
import classes from './Chat.module.css';

interface ChatAssistantMessageProps {
  message: UIMessage;
}

export function ChatAssistantMessage({ message }: ChatAssistantMessageProps) {
  return (
    <div className={classes.assistantRow}>
      <Paper className={classes.assistantBubble} py="sm" px="md" radius="lg" maw="75%">
        {message.parts.map((part, idx) => {
          if (part.type === 'thinking') {
            return (
              <Paper key={idx} className={classes.thinking} p="xs" mb="xs" withBorder radius="md">
                <Text size="sm" c="dimmed" fs="italic">
                  {part.content}
                </Text>
              </Paper>
            );
          }
          if (part.type === 'text') {
            return (
              <Text key={idx} size="sm" style={{ whiteSpace: 'pre-wrap' }}>
                {part.content}
              </Text>
            );
          }
          return null;
        })}
      </Paper>
    </div>
  );
}
