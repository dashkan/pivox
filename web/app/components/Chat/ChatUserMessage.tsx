import type { UIMessage } from '@tanstack/ai-react';
import { Paper } from '@mantine/core';
import type { PartsMap } from './Chat.types';
import classes from './Chat.module.css';

interface ChatUserMessageProps {
  message: UIMessage;
  parts: PartsMap;
}

export function ChatUserMessage({ message, parts }: ChatUserMessageProps) {
  return (
    <div className={classes.userRow}>
      <Paper bg="blue" c="white" py="sm" px="md" radius="lg" maw="75%">
        {message.parts.map((part, idx) => {
          const Renderer = parts[part.type];
          return Renderer ? <Renderer key={idx} part={part} messageRole="user" /> : null;
        })}
      </Paper>
    </div>
  );
}
