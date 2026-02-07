import { useRef } from 'react';
import { render, screen } from '@/test-utils';
import { Chat } from './Chat';
import type { ChatActions, ChatMeta, ChatState } from './Chat.types';

const mockState: ChatState = {
  messages: [],
  input: '',
  isLoading: false,
  error: undefined,
};

const mockActions: ChatActions = {
  setInput: vi.fn(),
  submit: vi.fn(),
};

function TestChat() {
  const viewportRef = useRef<HTMLDivElement>(null);

  const mockMeta: ChatMeta = {
    viewportRef,
    canSubmit: false,
  };

  return (
    <Chat.Provider state={mockState} actions={mockActions} meta={mockMeta}>
      <Chat.Frame>
        <Chat.EmptyState />
        <Chat.MessageList />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </Chat.Provider>
  );
}

describe('Chat component', () => {
  it('renders empty state message', () => {
    render(<TestChat />);
    expect(screen.getByText('Send a message to start a conversation.')).toBeInTheDocument();
  });
});
