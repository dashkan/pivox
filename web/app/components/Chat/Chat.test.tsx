import { render, screen } from '@/test-utils';
import { Chat } from './Chat';

vi.mock('@tanstack/ai-react', () => ({
  fetchServerSentEvents: () => ({}),
  useChat: () => ({
    messages: [],
    sendMessage: vi.fn(),
    isLoading: false,
    error: null,
  }),
}));

vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: () => ({
    getTotalSize: () => 0,
    getVirtualItems: () => [],
    measureElement: vi.fn(),
    scrollToIndex: vi.fn(),
  }),
}));

describe('Chat component', () => {
  it('renders empty state message', () => {
    render(<Chat />);
    expect(screen.getByText('Send a message to start a conversation.')).toBeInTheDocument();
  });
});
