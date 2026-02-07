'use client';

import { useVirtualizer } from '@tanstack/react-virtual';
import { Box, ScrollArea } from '@mantine/core';
import { useChatContext } from './Chat.context';
import { ChatMessageItem } from './ChatMessageItem';

export function ChatMessageList() {
  const { state, meta } = useChatContext();

  const virtualizer = useVirtualizer({
    count: state.messages.length,
    getScrollElement: () => meta.viewportRef.current,
    estimateSize: () => 80,
    overscan: 5,
  });

  if (state.messages.length === 0) {
    return null;
  }

  return (
    <ScrollArea flex={1} viewportRef={meta.viewportRef} scrollbars="y">
      <Box
        pos="relative"
        maw={768}
        w="100%"
        mx="auto"
        style={{ height: virtualizer.getTotalSize() }}
      >
        <div
          style={{
            position: 'absolute',
            top: 0,
            left: 0,
            width: '100%',
            transform: `translateY(${virtualizer.getVirtualItems()[0]?.start ?? 0}px)`,
          }}
        >
          {virtualizer.getVirtualItems().map((virtualRow) => (
            <ChatMessageItem
              key={virtualRow.key}
              index={virtualRow.index}
              message={state.messages[virtualRow.index]}
              measureElement={virtualizer.measureElement}
            />
          ))}
        </div>
      </Box>
    </ScrollArea>
  );
}
