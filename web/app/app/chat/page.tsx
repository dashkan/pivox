'use client';

import { useMemo } from 'react';
import { useMantineColorScheme } from '@mantine/core';
import { fetchServerSentEvents } from '@tanstack/ai-react';
import { setThemeDef } from '@/ai/tools/theme';
import { ThemeToolResult } from '@/ai/tools/theme.result';
import { Chat } from '@/components/Chat/Chat';

export default function Home() {
  const { setColorScheme } = useMantineColorScheme();

  const tools = useMemo(
    () => [
      setThemeDef.client(({ theme }) => {
        setColorScheme(theme);
        return { theme };
      }),
    ],
    [setColorScheme]
  );

  return (
    <Chat.Provider connection={fetchServerSentEvents('/api/chat')} tools={tools}>
      <Chat.Frame>
        <Chat.Header />
        <Chat.EmptyState />
        <Chat.MessageList
          parts={{ ...Chat.defaultParts }}
          toolParts={{ ...Chat.defaultToolParts, set_theme: ThemeToolResult }}
        />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </Chat.Provider>
  );
}
