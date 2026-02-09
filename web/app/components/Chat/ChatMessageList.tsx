'use client';

import { useMemo } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { Box, ScrollArea } from '@mantine/core';
import { useChatContext } from './Chat.context';
import type { PartsMap, ToolPartsMap } from './Chat.types';
import { ChatMessageItem } from './ChatMessageItem';
import { defaultParts } from './ChatParts';
import { defaultToolParts } from './ChatToolParts';

interface ChatMessageListProps {
  parts?: PartsMap;
  toolParts?: ToolPartsMap;
}

export function ChatMessageList({
  parts: partsProp,
  toolParts: toolPartsProp,
}: ChatMessageListProps) {
  const { state, meta } = useChatContext();
  const resolvedParts = useMemo(() => ({ ...defaultParts, ...partsProp }), [partsProp]);
  const resolvedToolParts = useMemo(
    () => ({ ...defaultToolParts, ...toolPartsProp }),
    [toolPartsProp]
  );

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
    <ScrollArea flex={1} mih={0} viewportRef={meta.viewportRef} scrollbars="y">
      <Box
        pos="relative"
        maw={768}
        w="100%"
        mx="auto"
        style={{ height: virtualizer.getTotalSize() }}
      >
        <Box
          pos="absolute"
          top={0}
          left={0}
          w="100%"
          style={{ transform: `translateY(${virtualizer.getVirtualItems()[0]?.start ?? 0}px)` }}
        >
          {virtualizer.getVirtualItems().map((virtualRow) => (
            <ChatMessageItem
              key={virtualRow.key}
              index={virtualRow.index}
              message={state.messages[virtualRow.index]}
              measureElement={virtualizer.measureElement}
              parts={resolvedParts}
              toolParts={resolvedToolParts}
            />
          ))}
        </Box>
      </Box>
    </ScrollArea>
  );
}
