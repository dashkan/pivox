'use client';

import { Chat } from '@/components/Chat/Chat';
import { useChatStore } from '@/components/Chat/useChatStore';

export default function Home() {
  const { state, actions, meta } = useChatStore();

  return (
    <Chat.Provider state={state} actions={actions} meta={meta}>
      <Chat.Frame>
        <Chat.EmptyState />
        <Chat.MessageList
          parts={{ ...Chat.defaultParts }}
          toolParts={{ ...Chat.defaultToolParts, generate_image: Chat.ImageToolResult }}
        />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </Chat.Provider>
  );
}
