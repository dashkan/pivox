import {
  chat,
  toServerSentEventsResponse,
  type ContentPart,
  type ModelMessage,
} from '@tanstack/ai';
import { anthropicText } from '@tanstack/ai-anthropic';
import { generateImageServer } from '@/ai/tools/image';
import { env } from '../../../env';

const adapters = {
  anthropic: () => anthropicText('claude-sonnet-4-5'),
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
          source: { type: 'data', value: file.data },
          metadata: { mediaType: file.type },
        });
      } else if (file.type === 'application/pdf') {
        // Don't include mediaType in metadata — the adapter hardcodes
        // media_type:'application/pdf' in the source, and spreading mediaType
        // onto the document block causes an Anthropic API rejection.
        content.push({
          type: 'document',
          source: { type: 'data', value: file.data },
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
      adapter: adapters.anthropic(),
      // Mixed UIMessages (pass-through) and ModelMessages (with files) —
      // the Anthropic adapter's constrained generics can't express this union
      messages: transformMessages(messages) as any,
      tools: [generateImageServer],
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
