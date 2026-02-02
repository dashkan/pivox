'use client';

import { useEffect, useRef, useState } from 'react';
import { fetchServerSentEvents, useChat } from '@tanstack/ai-react';
import { useVirtualizer } from '@tanstack/react-virtual';
import {
  ActionIcon,
  Alert,
  Box,
  Center,
  Divider,
  Group,
  Paper,
  ScrollArea,
  Stack,
  Text,
  TextInput,
} from '@mantine/core';
import { IconAlertCircle, IconSend } from '@tabler/icons-react';
import classes from './Chat.module.css';

export function Chat() {
  const [input, setInput] = useState('');
  const viewportRef = useRef<HTMLDivElement>(null);

  const { messages, sendMessage, isLoading, error } = useChat({
    connection: fetchServerSentEvents('/api/chat'),
  });

  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => viewportRef.current,
    estimateSize: () => 80,
    overscan: 5,
  });

  useEffect(() => {
    if (messages.length > 0) {
      requestAnimationFrame(() => {
        viewportRef.current?.scrollTo({ top: viewportRef.current.scrollHeight });
      });
    }
  }, [messages]);

  return (
    <Stack h="100dvh" gap={0}>
      {messages.length === 0 ? (
        <Center flex={1}>
          <Text c="dimmed" size="sm">
            Send a message to start a conversation.
          </Text>
        </Center>
      ) : (
        <ScrollArea flex={1} viewportRef={viewportRef} scrollbars="y">
          <Box
            pos="relative"
            maw={768}
            w="100%"
            mx="auto"
            style={{ height: virtualizer.getTotalSize() }}
          >
            <div
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                transform: `translateY(${virtualizer.getVirtualItems()[0]?.start ?? 0}px)`,
              }}
            >
              {virtualizer.getVirtualItems().map((virtualRow) => {
                const message = messages[virtualRow.index];
                const isUser = message.role === 'user';

                return (
                  <Box
                    key={virtualRow.key}
                    data-index={virtualRow.index}
                    ref={virtualizer.measureElement}
                    py="xs"
                    px="md"
                  >
                    {isUser ? (
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
                    ) : (
                      <div className={classes.assistantRow}>
                        <Paper className={classes.assistantBubble} py="sm" px="md" radius="lg" maw="75%">
                          {message.parts.map((part, idx) => {
                            if (part.type === 'thinking') {
                              return (
                                <Paper
                                  key={idx}
                                  className={classes.thinking}
                                  p="xs"
                                  mb="xs"
                                  withBorder
                                  radius="md"
                                >
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
                    )}
                  </Box>
                );
              })}
            </div>
          </Box>
        </ScrollArea>
      )}

      {error && (
        <Box maw={768} mx="auto" px="md" w="100%">
          <Alert color="red" icon={<IconAlertCircle size={16} />}>
            {error.message}
          </Alert>
        </Box>
      )}

      <Divider />
      <Box p="md" bg="var(--mantine-color-body)">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (input.trim() && !isLoading) {
              sendMessage(input);
              setInput('');
            }
          }}
        >
          <Group gap="sm" align="flex-end" wrap="nowrap" maw={768} mx="auto">
            <TextInput
              value={input}
              onChange={(e) => setInput(e.currentTarget.value)}
              placeholder="Send a message..."
              disabled={isLoading}
              flex={1}
            />
            <ActionIcon
              type="submit"
              variant="filled"
              size="input-sm"
              disabled={!input.trim() || isLoading}
              aria-label="Send message"
            >
              <IconSend size={16} />
            </ActionIcon>
          </Group>
        </form>
      </Box>
    </Stack>
  );
}
