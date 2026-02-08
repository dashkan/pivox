import type { UIMessage } from '@tanstack/ai-react';
import { Chat } from './Chat';

export default {
  title: 'Chat',
};

export const Usage = () => {
  return (
    <Chat.Provider>
      <Chat.Frame>
        <Chat.EmptyState />
        <Chat.MessageList />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </Chat.Provider>
  );
};

const sampleMessages: UIMessage[] = [
  {
    id: '1',
    role: 'user',
    parts: [{ type: 'text', content: 'What is the capital of France?' }],
  },
  {
    id: '2',
    role: 'assistant',
    parts: [{ type: 'text', content: 'The capital of France is Paris.' }],
  },
  {
    id: '3',
    role: 'user',
    parts: [{ type: 'text', content: 'Tell me more about it.' }],
  },
  {
    id: '4',
    role: 'assistant',
    parts: [
      {
        type: 'text',
        content:
          'Paris is the largest city in France with a population of over 2 million. It is known for landmarks like the Eiffel Tower, the Louvre Museum, and Notre-Dame Cathedral.',
      },
    ],
  },
];

export const WithInitialMessages = () => {
  return (
    <Chat.Provider initialMessages={sampleMessages}>
      <Chat.Frame>
        <Chat.EmptyState />
        <Chat.MessageList />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </Chat.Provider>
  );
};
