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
    data: z.string().describe('Base64-encoded PNG image'),
    mimeType: z.string(),
    alt: z.string(),
  }),
});

export const generateImageServer = generateImageDef.server(async ({ prompt, width, height }) => {
  // Placeholder: fetch a random image and return as base64
  const res = await fetch(`https://picsum.photos/${width}/${height}`);
  const buffer = await res.arrayBuffer();
  const data = Buffer.from(buffer).toString('base64');
  const mimeType = res.headers.get('content-type') ?? 'image/jpeg';
  return { data, mimeType, alt: prompt };
});
