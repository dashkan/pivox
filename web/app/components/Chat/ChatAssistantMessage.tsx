import type { ToolCallPart, ToolResultPart } from '@tanstack/ai';
import type { UIMessage } from '@tanstack/ai-react';
import { Box, Paper } from '@mantine/core';
import type { PartsMap, ToolPartsMap } from './Chat.types';
import { ToolCallPartRenderer } from './ChatParts';
import { GenericToolResult } from './ChatToolParts';
import classes from './Chat.module.css';

interface ChatAssistantMessageProps {
  message: UIMessage;
  parts: PartsMap;
  toolParts?: ToolPartsMap;
}

function hasToolResult(message: UIMessage, toolCallId: string): boolean {
  return message.parts.some(
    (p) => p.type === 'tool-result' && (p as ToolResultPart).toolCallId === toolCallId
  );
}

function resolveToolCall(message: UIMessage, toolCallId: string): ToolCallPart | null {
  return (
    message.parts.find((p): p is ToolCallPart => p.type === 'tool-call' && p.id === toolCallId) ??
    null
  );
}

export function ChatAssistantMessage({ message, parts, toolParts }: ChatAssistantMessageProps) {
  return (
    <Box className={classes.assistantRow}>
      <Paper className={classes.assistantBubble} py="sm" px="md" radius="lg" maw="75%">
        {message.parts.map((part, idx) => {
          if (part.type === 'tool-call') {
            const callPart = part as ToolCallPart;
            if (hasToolResult(message, callPart.id)) {
              return null;
            }
            return <ToolCallPartRenderer key={idx} part={callPart} messageRole="assistant" />;
          }

          if (part.type === 'tool-result') {
            const resultPart = part as ToolResultPart;
            const callPart = resolveToolCall(message, resultPart.toolCallId);
            const toolName = callPart?.name ?? 'unknown';
            const ToolRenderer = toolParts?.[toolName] ?? GenericToolResult;
            return (
              <ToolRenderer
                key={idx}
                toolName={toolName}
                callPart={callPart!}
                resultPart={resultPart}
                messageRole="assistant"
              />
            );
          }

          const Renderer = parts[part.type];
          return Renderer ? <Renderer key={idx} part={part} messageRole="assistant" /> : null;
        })}
      </Paper>
    </Box>
  );
}
