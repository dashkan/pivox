'use client';

import { Chat } from '@/components/Chat/Chat';
import { useChatState } from '@/components/Chat/useChatState';

export default function Home() {
  const { state, actions, meta } = useChatState();

  return (
    <Chat.Provider state={state} actions={actions} meta={meta}>
      <Chat.Frame>
        <Chat.EmptyState />
        <Chat.MessageList />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </Chat.Provider>
  );
}
