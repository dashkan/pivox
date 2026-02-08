import {
  chat,
  toServerSentEventsResponse,
  type ContentPart,
  type ModelMessage,
} from '@tanstack/ai';
import { anthropicText } from '@tanstack/ai-anthropic';
import { ollamaText } from '@tanstack/ai-ollama';
import { generateImageServer } from '@/ai/tools/image';
import { setThemeDef } from '@/ai/tools/theme';
import { env } from '@/env';

type Provider = 'anthropic' | 'ollama';

const provider: Provider = 'ollama';

const adapters = {
  anthropic: () => anthropicText('claude-sonnet-4-5'),
  ollama: () => ollamaText('glm-4.7-flash'),
};

interface SerializedFile {
  name: string;
  type: string;
  size: number;
  data: string;
}

interface IncomingMessage {
  role: 'user' | 'assistant' | 'system';
  parts?: Array<{ type: string; content?: string }>;
  _files?: SerializedFile[];
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

/**
 * Transform incoming messages so that any message carrying `_files`
 * becomes a proper ModelMessage with ContentPart[] (image / document / text).
 * Messages without `_files` pass through as-is (UIMessages).
 */
function transformMessages(messages: IncomingMessage[]) {
  return messages.map((msg): IncomingMessage | ModelMessage => {
    if (!msg._files || msg._files.length === 0) {
      return msg;
    }

    const content: ContentPart[] = [];

    if (msg.parts) {
      for (const part of msg.parts) {
        if (part.type === 'text' && part.content) {
          content.push({ type: 'text', content: part.content });
        }
      }
    }

    for (const file of msg._files) {
      if (file.type.startsWith('image/')) {
        content.push({
          type: 'image',
          source: { type: 'data', value: file.data, mimeType: file.type },
        });
      } else if (file.type === 'application/pdf') {
        content.push({
          type: 'document',
          source: { type: 'data', value: file.data, mimeType: file.type },
        });
      } else {
        const decoded = Buffer.from(file.data, 'base64').toString('utf-8');
        content.push({ type: 'text', content: `[File: ${file.name}]\n${decoded}` });
      }
    }

    return { role: msg.role, content } as ModelMessage;
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

  try {
    const stream = chat({
      adapter: adapters[provider](),
      // Mixed UIMessages (pass-through) and ModelMessages (with files) —
      // the Anthropic adapter's constrained generics can't express this union
      messages: transformMessages(stripDuplicateToolOutputs(messages)) as any,
      tools: [generateImageServer, setThemeDef],
      modelOptions: {
        thinking: {
          type: 'enabled',
          budget_tokens: 2048,
        },
      },
    });

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
