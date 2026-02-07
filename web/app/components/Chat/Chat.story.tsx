import { Chat } from './Chat';
import { useChatState } from './useChatState';

export default {
  title: 'Chat',
};

export const Usage = () => {
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
};
