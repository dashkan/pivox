'use client';

import { useMemo } from 'react';
import { useMantineColorScheme } from '@mantine/core';
import type { ToolPartsMap } from '@/components/Chat/Chat.types';
import { setThemeDef } from './theme';
import { ThemeToolResult } from './theme.result';

const toolParts: ToolPartsMap = {
  set_theme: ThemeToolResult,
};

export function useClientTools() {
  const { setColorScheme } = useMantineColorScheme();

  const tools = useMemo(
    () => [
      setThemeDef.client(({ theme }) => {
        setColorScheme(theme);
        return { theme };
      }),
    ],
    [setColorScheme]
  );

  return { tools, toolParts };
}
