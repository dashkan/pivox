'use client';

import { IconDeviceLaptop, IconMoon, IconSun } from '@tabler/icons-react';
import {
  isLightColor,
  useMantineColorScheme,
  useMantineTheme,
  type MantineColorScheme,
} from '@mantine/core';
import { useMounted } from '@mantine/hooks';
import classes from './ColorSchemeToggle.module.css';

const options: { value: MantineColorScheme; icon: typeof IconSun }[] = [
  { value: 'light', icon: IconSun },
  { value: 'auto', icon: IconDeviceLaptop },
  { value: 'dark', icon: IconMoon },
];

export function ColorSchemeToggle() {
  const { colorScheme, setColorScheme } = useMantineColorScheme();
  const theme = useMantineTheme();
  const mounted = useMounted();
  const activeIndex = mounted ? options.findIndex((o) => o.value === colorScheme) : -1;

  const primaryFilled =
    theme.colors[theme.primaryColor][theme.primaryShade as number] ??
    theme.colors[theme.primaryColor][6];
  const activeIconColor = isLightColor(primaryFilled)
    ? 'var(--mantine-color-black)'
    : 'var(--mantine-color-white)';

  return (
    <div
      className={classes.track}
      role="radiogroup"
      aria-label="Color scheme"
      style={{ '--cst-active-color': activeIconColor } as React.CSSProperties}
    >
      <div
        className={classes.indicator}
        data-position={activeIndex === -1 ? 1 : activeIndex}
        style={{ width: 26 }}
      />
      {options.map(({ value, icon: Icon }) => (
        <button
          key={value}
          type="button"
          role="radio"
          aria-checked={mounted && colorScheme === value}
          aria-label={value}
          className={classes.option}
          data-active={(mounted && colorScheme === value) || undefined}
          onClick={() => setColorScheme(value)}
        >
          <Icon size={14} stroke={1.5} className={classes.icon} />
        </button>
      ))}
    </div>
  );
}
