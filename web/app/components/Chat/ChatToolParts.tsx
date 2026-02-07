import { Code, Image, Text } from '@mantine/core';
import type { ToolPartRendererProps, ToolPartsMap } from './Chat.types';

export function GenericToolResult({ toolName, resultPart }: ToolPartRendererProps) {
  if (!resultPart) {
    return null;
  }

  if (resultPart.error) {
    return (
      <Text size="sm" c="red">
        {toolName} failed: {resultPart.error}
      </Text>
    );
  }

  return <Code block>{resultPart.content}</Code>;
}

export function ImageToolResult({ resultPart }: ToolPartRendererProps) {
  if (!resultPart) {
    return null;
  }

  const data = JSON.parse(resultPart.content) as { url: string; alt: string };

  return <Image src={data.url} alt={data.alt} radius="md" maw={400} />;
}

export const defaultToolParts: ToolPartsMap = {
  generate_image: ImageToolResult,
};
