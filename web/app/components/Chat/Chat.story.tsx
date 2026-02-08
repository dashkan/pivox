import { useMemo } from 'react';
import { IconCloud, IconSun, IconTemperature } from '@tabler/icons-react';
import { Card, Group, Stack, Text, ThemeIcon } from '@mantine/core';
import { toolDefinition, type StreamChunk } from '@tanstack/ai';
import type { ConnectionAdapter, UIMessage } from '@tanstack/ai-react';
import { z } from 'zod';
import { Chat } from './Chat';
import type { ToolPartRendererProps } from './Chat.types';

export default {
  title: 'Chat',
};

const delay = (ms: number) => new Promise((r) => setTimeout(r, ms));

function createMockConnection(): ConnectionAdapter {
  return {
    async *connect(): AsyncIterable<StreamChunk> {
      const ts = Date.now();
      const runId = crypto.randomUUID();
      const messageId = crypto.randomUUID();
      const stepId = crypto.randomUUID();

      yield { type: 'RUN_STARTED', runId, timestamp: ts } as StreamChunk;

      // Thinking step
      yield { type: 'STEP_STARTED', stepId, timestamp: ts } as StreamChunk;
      const thinkingChunks = [
        'Let me think about this... ',
        'The user is asking a question. ',
        'I should provide a helpful and detailed response.',
      ];
      let thinkingAccum = '';
      for (const chunk of thinkingChunks) {
        await delay(80);
        thinkingAccum += chunk;
        yield { type: 'STEP_FINISHED', stepId, delta: chunk, content: thinkingAccum, timestamp: Date.now() } as StreamChunk;
      }

      // Text response
      yield { type: 'TEXT_MESSAGE_START', messageId, role: 'assistant', timestamp: Date.now() } as StreamChunk;
      const words = 'Paris is the capital and largest city of France. It is known for the Eiffel Tower, the Louvre Museum, and Notre-Dame Cathedral.'.split(' ');
      let textAccum = '';
      for (const word of words) {
        await delay(40);
        const delta = (textAccum ? ' ' : '') + word;
        textAccum += delta;
        yield { type: 'TEXT_MESSAGE_CONTENT', messageId, delta, content: textAccum, timestamp: Date.now() } as StreamChunk;
      }
      yield { type: 'TEXT_MESSAGE_END', messageId, timestamp: Date.now() } as StreamChunk;

      yield { type: 'RUN_FINISHED', runId, finishReason: 'stop', timestamp: Date.now() } as StreamChunk;
    },
  };
}

const mockConnection = createMockConnection();

export const Usage = () => {
  return (
    <Chat.Provider connection={mockConnection}>
      <Chat.Frame>
        <Chat.Header />
        <Chat.EmptyState />
        <Chat.MessageList />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </Chat.Provider>
  );
};

const sampleMessages: UIMessage[] = [
  {
    id: '1',
    role: 'user',
    parts: [{ type: 'text', content: 'What is the capital of France?' }],
  },
  {
    id: '2',
    role: 'assistant',
    parts: [
      { type: 'thinking', content: 'The user is asking about France\'s capital. That\'s Paris — a straightforward geography question.' },
      { type: 'text', content: 'The capital of France is Paris.' },
    ],
  },
  {
    id: '3',
    role: 'user',
    parts: [{ type: 'text', content: 'Tell me more about it.' }],
  },
  {
    id: '4',
    role: 'assistant',
    parts: [
      { type: 'thinking', content: 'They want more detail about Paris. I\'ll mention population, landmarks, and cultural significance.' },
      { type: 'text', content: 'Paris is the largest city in France with a population of over 2 million. It is known for landmarks like the Eiffel Tower, the Louvre Museum, and Notre-Dame Cathedral.' },
    ],
  },
];

export const WithInitialMessages = () => {
  return (
    <Chat.Provider connection={mockConnection} initialMessages={sampleMessages}>
      <Chat.Frame>
        <Chat.Header />
        <Chat.EmptyState />
        <Chat.MessageList />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </Chat.Provider>
  );
};

// -- Weather tool renderer --

const conditionIcons: Record<string, typeof IconSun> = {
  sunny: IconSun,
  cloudy: IconCloud,
};

function WeatherToolResult({ resultPart }: ToolPartRendererProps) {
  if (!resultPart) {
    return null;
  }

  const { temperature, condition, unit } = JSON.parse(resultPart.content) as {
    temperature: number;
    condition: string;
    unit: 'C' | 'F';
  };
  const keyword = Object.keys(conditionIcons).find((k) => condition.includes(k));
  const Icon = keyword ? conditionIcons[keyword] : IconTemperature;

  return (
    <Card withBorder radius="md" padding="sm" w={220}>
      <Group gap="sm" wrap="nowrap">
        <ThemeIcon variant="light" size="lg" radius="xl" color="blue">
          <Icon size={20} />
        </ThemeIcon>
        <Stack gap={2}>
          <Text size="sm" fw={500}>
            {temperature}&deg;{unit}
          </Text>
          <Text size="xs" c="dimmed" tt="capitalize">
            {condition}
          </Text>
        </Stack>
      </Group>
    </Card>
  );
}

// -- Client tool story --

const cities = ['Paris', 'Tokyo', 'New York', 'Sydney', 'Cairo', 'Reykjavik', 'Mumbai', 'Rio de Janeiro'];
const conditions = ['sunny', 'cloudy', 'partly cloudy', 'rainy', 'windy'];
const pick = <T,>(arr: T[]): T => arr[Math.floor(Math.random() * arr.length)];

const getWeatherDef = toolDefinition({
  name: 'get_weather',
  description: 'Get current weather for a city',
  inputSchema: z.object({ city: z.string() }),
  outputSchema: z.object({ temperature: z.number(), condition: z.string(), unit: z.enum(['C', 'F']) }),
});

function lastMessageIsToolResult(messages: unknown[]): boolean {
  const last = messages[messages.length - 1] as Record<string, unknown> | undefined;
  if (!last) {
    return false;
  }
  // ModelMessage format: tool results have role 'tool'
  if (last.role === 'tool') {
    return true;
  }
  // UIMessage format: parts may contain tool-result
  if (Array.isArray(last.parts)) {
    return (last.parts as Array<{ type: string }>).some((p) => p.type === 'tool-result');
  }
  return false;
}

async function* streamText(text: string): AsyncIterable<StreamChunk> {
  const ts = Date.now();
  const runId = crypto.randomUUID();
  const messageId = crypto.randomUUID();

  yield { type: 'RUN_STARTED', runId, timestamp: ts } as StreamChunk;
  yield { type: 'TEXT_MESSAGE_START', messageId, role: 'assistant', timestamp: ts } as StreamChunk;
  const words = text.split(' ');
  let accum = '';
  for (const word of words) {
    await delay(40);
    const delta = (accum ? ' ' : '') + word;
    accum += delta;
    yield { type: 'TEXT_MESSAGE_CONTENT', messageId, delta, content: accum, timestamp: Date.now() } as StreamChunk;
  }
  yield { type: 'TEXT_MESSAGE_END', messageId, timestamp: Date.now() } as StreamChunk;
  yield { type: 'RUN_FINISHED', runId, finishReason: 'stop', timestamp: Date.now() } as StreamChunk;
}

async function* streamToolCall(toolName: string, args: Record<string, unknown>): AsyncIterable<StreamChunk> {
  const ts = Date.now();
  const runId = crypto.randomUUID();
  const toolCallId = crypto.randomUUID();

  yield { type: 'RUN_STARTED', runId, timestamp: ts } as StreamChunk;
  yield { type: 'TOOL_CALL_START', toolCallId, toolName, timestamp: ts } as StreamChunk;
  const argsJson = JSON.stringify(args);
  await delay(100);
  yield { type: 'TOOL_CALL_ARGS', toolCallId, delta: argsJson, args: argsJson, timestamp: Date.now() } as StreamChunk;
  yield { type: 'TOOL_CALL_END', toolCallId, toolName, input: args, timestamp: Date.now() } as StreamChunk;
  // StreamProcessor only triggers onToolCall (client tool execution) via CUSTOM events
  yield { type: 'CUSTOM', name: 'tool-input-available', data: { toolCallId, toolName, input: args }, timestamp: Date.now() } as StreamChunk;
  yield { type: 'RUN_FINISHED', runId, finishReason: 'tool_calls', timestamp: Date.now() } as StreamChunk;
}

const weatherToolConnection: ConnectionAdapter = {
  async *connect(messages) {
    if (lastMessageIsToolResult(messages)) {
      yield* streamText('There you go! I just fetched the latest weather for you.');
    } else {
      yield* streamToolCall('get_weather', { city: pick(cities) });
    }
  },
};

const toolCallMessages: UIMessage[] = [
  {
    id: '1',
    role: 'user',
    parts: [{ type: 'text', content: 'What is the weather in London?' }],
  },
  {
    id: '2',
    role: 'assistant',
    parts: [
      { type: 'tool-call', id: 'tc_1', name: 'get_weather', arguments: '{"city":"London"}', state: 'input-complete' },
      { type: 'tool-result', toolCallId: 'tc_1', content: '{"temperature":15,"condition":"cloudy in London"}', state: 'complete' },
    ],
  },
  {
    id: '3',
    role: 'assistant',
    parts: [
      { type: 'text', content: 'The weather in London is currently cloudy at 15\u00B0C.' },
    ],
  },
];

export const WithClientTool = () => {
  const tools = useMemo(
    () => [
      getWeatherDef.client(({ city }) => {
        const unit = pick(['C', 'F'] as const);
        const temp = unit === 'C' ? Math.round(Math.random() * 40 - 5) : Math.round(Math.random() * 72 + 23);
        return { temperature: temp, condition: `${pick(conditions)} in ${city}`, unit };
      }),
    ],
    []
  );

  return (
    <Chat.Provider connection={weatherToolConnection} tools={tools} initialMessages={toolCallMessages}>
      <Chat.Frame>
        <Chat.Header />
        <Chat.EmptyState />
        <Chat.MessageList toolParts={{ get_weather: WeatherToolResult }} />
        <Chat.ErrorAlert />
        <Chat.Input />
      </Chat.Frame>
    </Chat.Provider>
  );
};
