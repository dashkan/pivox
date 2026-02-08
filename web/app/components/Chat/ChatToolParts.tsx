import { useState } from 'react';
import { IconCheck, IconCopy, IconDownload } from '@tabler/icons-react';
import { ActionIcon, Card, Code, Group, Image, Text, Tooltip } from '@mantine/core';
import type { ToolPartRendererProps, ToolPartsMap } from './Chat.types';
import classes from './Chat.module.css';

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

type ImageResult =
  | { url: string; alt: string; data?: never }
  | { data: string; mimeType: string; alt: string; url?: never };

function base64ToBlob(base64: string, mimeType: string): Blob {
  const bytes = atob(base64);
  const buffer = new Uint8Array(bytes.length);
  for (let i = 0; i < bytes.length; i++) {
    buffer[i] = bytes.charCodeAt(i);
  }
  return new Blob([buffer], { type: mimeType });
}

async function toBlobPng(blob: Blob): Promise<Blob> {
  if (blob.type === 'image/png') {
    return blob;
  }
  const bitmap = await createImageBitmap(blob);
  const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
  canvas.getContext('2d')!.drawImage(bitmap, 0, 0);
  return canvas.convertToBlob({ type: 'image/png' });
}

async function getImageBlob(img: ImageResult): Promise<Blob> {
  if (img.data) {
    return base64ToBlob(img.data, img.mimeType);
  }
  return fetch(img.url!).then((r) => r.blob());
}

export function ImageToolResult({ resultPart }: ToolPartRendererProps) {
  const [copied, setCopied] = useState(false);

  if (!resultPart) {
    return null;
  }

  const img = JSON.parse(resultPart.content) as ImageResult;
  const src = img.data ? `data:${img.mimeType};base64,${img.data}` : img.url;

  const handleCopy = async () => {
    const blob = await getImageBlob(img);
    const png = await toBlobPng(blob);
    await navigator.clipboard.write([new ClipboardItem({ 'image/png': png })]);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleDownload = async () => {
    const blob = await getImageBlob(img);
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = img.alt || 'image';
    a.click();
    URL.revokeObjectURL(a.href);
  };

  return (
    <Card className={classes.imageCard} padding={0} radius="md" maw={400}>
      <Image src={src} alt={img.alt} radius="md" />
      <Group className={classes.imageActions} gap={4}>
        <Tooltip label={copied ? 'Copied' : 'Copy image'}>
          <ActionIcon
            variant="filled"
            color={copied ? 'teal' : 'dark'}
            size="sm"
            onClick={handleCopy}
            style={{ opacity: 0.8 }}
          >
            {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
          </ActionIcon>
        </Tooltip>
        <Tooltip label="Download">
          <ActionIcon
            variant="filled"
            color="dark"
            size="sm"
            onClick={handleDownload}
            style={{ opacity: 0.8 }}
          >
            <IconDownload size={14} />
          </ActionIcon>
        </Tooltip>
      </Group>
    </Card>
  );
}

export const defaultToolParts: ToolPartsMap = {
  generate_image: ImageToolResult,
};
