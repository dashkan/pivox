'use client';

import { fetchServerSentEvents } from '@tanstack/ai-react';
import { useClientTools } from '@/ai/tools/useClientTools';
import { Chat } from '@/components/Chat/Chat';

export default function Home() {
  const { tools, toolParts } = useClientTools();

  return (
    <Chat.Provider connection={fetchServerSentEvents('/api/chat')} tools={tools}>
      <Chat.Root>
        <Chat.Header />
        <Chat.EmptyState />
        <Chat.MessageList
          parts={{ ...Chat.defaultParts }}
          toolParts={{ ...Chat.defaultToolParts, ...toolParts }}
        />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Root>
    </Chat.Provider>
  );
}
