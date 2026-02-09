import { IconDeviceLaptop, IconMoon, IconSun } from '@tabler/icons-react';
import { Group, Text, ThemeIcon } from '@mantine/core';
import type { ToolPartRendererProps } from '@/components/Chat/Chat.types';

const icons: Record<string, typeof IconSun> = {
  light: IconSun,
  dark: IconMoon,
  auto: IconDeviceLaptop,
};

const labels: Record<string, string> = {
  light: 'Light mode',
  dark: 'Dark mode',
  auto: 'System theme',
};

export function ThemeToolResult({ resultPart }: ToolPartRendererProps) {
  if (!resultPart) {
    return null;
  }

  const { theme } = JSON.parse(resultPart.content) as { theme: string };
  const Icon = icons[theme] ?? IconDeviceLaptop;

  return (
    <Group gap="xs" py="xs">
      <ThemeIcon variant="light" size="sm" radius="xl">
        <Icon size={14} />
      </ThemeIcon>
      <Text size="sm" c="dimmed">
        Switched to {labels[theme] ?? theme}
      </Text>
    </Group>
  );
}
