import { toolDefinition } from '@tanstack/ai';
import { z } from 'zod';

export const generateImageDef = toolDefinition({
  name: 'generate_image',
  description: 'Generate an image based on a text prompt',
  inputSchema: z.object({
    prompt: z.string().describe('A description of the image to generate'),
    width: z.number().optional().default(512),
    height: z.number().optional().default(512),
  }),
  outputSchema: z.object({
    url: z.string(),
    alt: z.string(),
  }),
});

export const generateImageServer = generateImageDef.server(async ({ prompt, width, height }) => {
  const url = `https://picsum.photos/${width}/${height}`;
  return { url, alt: prompt };
});
