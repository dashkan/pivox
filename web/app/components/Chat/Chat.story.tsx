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
