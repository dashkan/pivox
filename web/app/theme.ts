'use client';

import { dark, experimental_createTheme } from '@clerk/themes';
import { createTheme, type MantineColorsTuple } from '@mantine/core';
import styles from './theme.module.css';

const themeColor: MantineColorsTuple = [
  '#f7ecff',
  '#e7d6fb',
  '#caaaf1',
  '#ac7ce8',
  '#9354e0',
  '#833bdb',
  '#7b2eda',
  '#6921c2',
  '#5d1cae',
  '#501599',
];

export const theme = createTheme({
  colors: {
    theme: themeColor,
  },
  primaryColor: 'theme',
  spacing: {
    '2xs': '0.3125rem',
    '3xs': '0.25rem',
    '4xs': '0.125rem',
  },
});

export const clerkLightTheme = experimental_createTheme({
  variables: {
    colorBackground: 'var(--mantine-color-body)',
    colorForeground: 'var(--mantine-color-text)',
    colorNeutral: 'var(--mantine-color-default-color)',
    colorPrimary: 'var(--mantine-primary-color-light-color)',
    colorPrimaryForeground: 'var(--mantine-color-white)',
    colorDanger: 'var(--mantine-color-error)',
    colorSuccess: 'var(--mantine-color-green-filled)',
    colorWarning: 'var(--mantine-color-yellow-filled)',
    colorInputBackground: 'var(--mantine-color-dark-6)',
    colorInputForeground: 'var(--mantine-color-text)',
    colorModalBackdrop: 'rgba(0, 0, 0, .6)',
  },
  elements: {
    input: styles.input,
    button: styles.button,
  },
});

export const clerkDarkTheme = experimental_createTheme({
  baseTheme: [dark, clerkLightTheme],
});
