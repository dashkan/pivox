import { toolDefinition } from '@tanstack/ai';
import { z } from 'zod';

export const setThemeDef = toolDefinition({
  name: 'set_theme',
  description:
    'Change the application color scheme. Use this when the user asks to switch to dark mode, light mode, or auto/system theme.',
  inputSchema: z.object({
    theme: z
      .enum(['light', 'dark', 'auto'])
      .describe('The color scheme to apply: light, dark, or auto (follows system preference)'),
  }),
  outputSchema: z.object({
    theme: z.enum(['light', 'dark', 'auto']),
  }),
});

