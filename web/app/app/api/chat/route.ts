import { chat, toServerSentEventsResponse } from '@tanstack/ai';
import { anthropicText } from '@tanstack/ai-anthropic';
import { ollamaText } from '@tanstack/ai-ollama';
import { generateImageServer } from '@/ai/tools/image';
import { setThemeDef } from '@/ai/tools/theme';
import { env } from '@/env';

type Provider = 'anthropic' | 'ollama';

const provider: Provider = 'ollama';

const adapters = {
  anthropic: () => anthropicText('claude-sonnet-4-5'),
  ollama: () => ollamaText('llama4-xsmall-ctx'),
};

interface IncomingMessage {
  role: 'user' | 'assistant' | 'system';
  parts?: Array<{ type: string; content?: string }>;
  [key: string]: unknown;
}

/**
 * Strip the redundant `output` field from tool-call parts.
 *
 * When a client tool executes, the StreamProcessor sets *both*
 * `output` on the tool-call part and adds a separate tool-result part.
 * On the continuation request the server's `convertMessagesToModelMessages`
 * converts both into `{role:"tool"}` model messages — which Anthropic
 * rejects as duplicate tool_result blocks.
 *
 * Removing `output` from tool-call parts leaves only the tool-result
 * parts as the single source of truth.
 */
function stripDuplicateToolOutputs(messages: IncomingMessage[]) {
  return messages.map((msg) => {
    if (!msg.parts) {
      return msg;
    }
    return {
      ...msg,
      parts: msg.parts.map((part) => {
        if (part.type === 'tool-call' && 'output' in part) {
          const { output: _output, ...rest } = part;
          return rest as typeof part;
        }
        return part;
      }),
    };
  });
}

export async function POST(request: Request) {
  if (!env.ANTHROPIC_API_KEY) {
    return new Response(JSON.stringify({ error: 'ANTHROPIC_API_KEY not configured' }), {
      status: 500,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  const { messages } = await request.json();

  const options = {
    adapter: adapters[provider](),
    messages: stripDuplicateToolOutputs(messages) as any,
    tools: [generateImageServer, setThemeDef],
    modelOptions: {},
  };

  if (provider === 'anthropic') {
    options.modelOptions = {
      ...options.modelOptions,
      thinking: {
        type: 'enabled',
        budget_tokens: 2048,
      },
    };
  }

  if (provider === 'ollama') {
    options.modelOptions = {
      ...options.modelOptions,
      think: 'high',
      options: {
        num_ctx: 8192,
      },
    };
  }

  try {
    const stream = chat(options);

    return toServerSentEventsResponse(stream);
  } catch (error) {
    return new Response(
      JSON.stringify({
        error: error instanceof Error ? error.message : 'An error occurred',
      }),
      { status: 500, headers: { 'Content-Type': 'application/json' } }
    );
  }
}
