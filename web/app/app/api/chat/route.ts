import { chat, toServerSentEventsResponse } from '@tanstack/ai';
import { anthropicText } from '@tanstack/ai-anthropic';
import { generateImageServer } from '@/ai/tools/image';
import { env } from '../../../env';

// Define adapters with their models - autocomplete works here!
const adapters = {
  anthropic: () => anthropicText('claude-sonnet-4-5'),
};

export async function POST(request: Request) {
  // Check for API key
  if (!env.ANTHROPIC_API_KEY) {
    return new Response(
      JSON.stringify({
        error: 'ANTHROPIC_API_KEY not configured',
      }),
      {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      }
    );
  }

  const { messages } = await request.json();

  try {
    // Create a streaming chat response
    const stream = chat({
      adapter: adapters.anthropic(),
      messages,
      tools: [generateImageServer],
    });

    // Convert stream to HTTP response
    return toServerSentEventsResponse(stream);
  } catch (error) {
    return new Response(
      JSON.stringify({
        error: error instanceof Error ? error.message : 'An error occurred',
      }),
      {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      }
    );
  }
}
