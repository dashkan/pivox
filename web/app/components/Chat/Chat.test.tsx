import { useRef } from 'react';
import { render, screen } from '@/test-utils';
import { Chat } from './Chat';
import { ChatContext } from './Chat.context';
import type { ChatActions, ChatMeta, ChatState } from './Chat.types';

const mockState: ChatState = {
  messages: [],
  input: '',
  isLoading: false,
  error: undefined,
  files: [],
};

const mockActions: ChatActions = {
  setInput: vi.fn(),
  submit: vi.fn(),
  addFiles: vi.fn(),
  removeFile: vi.fn(),
};

function TestChat() {
  const viewportRef = useRef<HTMLDivElement>(null);

  const mockMeta: ChatMeta = {
    viewportRef,
    canSubmit: false,
  };

  return (
    <ChatContext value={{ state: mockState, actions: mockActions, meta: mockMeta }}>
      <Chat.Frame>
        <Chat.EmptyState />
        <Chat.MessageList />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </ChatContext>
  );
}

describe('Chat component', () => {
  it('renders empty state message', () => {
    render(<TestChat />);
    expect(screen.getByText('Send a message to start a conversation.')).toBeInTheDocument();
  });
});
