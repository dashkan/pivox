import type { ReactNode } from 'react';
import { Stack } from '@mantine/core';

interface ChatFrameProps {
  children: ReactNode;
}

export function ChatFrame({ children }: ChatFrameProps) {
  return (
    <Stack h="100dvh" gap={0}>
      {children}
    </Stack>
  );
}
