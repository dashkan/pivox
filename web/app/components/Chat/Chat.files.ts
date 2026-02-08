import type { ContentPart } from '@tanstack/ai';
import type { MultimodalContent } from '@tanstack/ai-client';
import type { FileAttachment } from './Chat.types';

export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result as string;
      resolve(result.split(',')[1]);
    };
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

export function createFileAttachment(file: File): FileAttachment {
  return {
    id: crypto.randomUUID(),
    file,
    name: file.name,
    type: file.type,
    size: file.size,
    previewUrl: file.type.startsWith('image/') ? URL.createObjectURL(file) : undefined,
  };
}

export function revokeFileAttachment(attachment: FileAttachment): void {
  if (attachment.previewUrl) {
    URL.revokeObjectURL(attachment.previewUrl);
  }
}

export function revokeAllFileAttachments(attachments: FileAttachment[]): void {
  attachments.forEach(revokeFileAttachment);
}

/**
 * Convert file attachments to native TanStack AI ContentPart[].
 */
export async function filesToContentParts(files: FileAttachment[]): Promise<ContentPart[]> {
  const parts: ContentPart[] = [];
  for (const f of files) {
    const base64 = await fileToBase64(f.file);
    if (f.type.startsWith('image/')) {
      parts.push({ type: 'image', source: { type: 'data', value: base64, mimeType: f.type } });
    } else if (f.type === 'application/pdf') {
      parts.push({ type: 'document', source: { type: 'data', value: base64, mimeType: f.type } });
    } else {
      const decoded = atob(base64);
      parts.push({ type: 'text', content: `[File: ${f.name}]\n${decoded}` });
    }
  }
  return parts;
}

/**
 * Sends a message with optional file attachments.
 * Returns `true` if a message was sent, `false` if skipped (empty input / loading).
 */
export async function submitMessage(params: {
  input: string;
  files: FileAttachment[];
  isLoading: boolean;
  sendMessage: (content: string | MultimodalContent) => Promise<void>;
}): Promise<boolean> {
  const { input, files, isLoading, sendMessage } = params;
  const hasText = input.trim() !== '';
  const hasFiles = files.length > 0;

  if ((!hasText && !hasFiles) || isLoading) {
    return false;
  }

  if (hasFiles) {
    const fileParts = await filesToContentParts(files);
    const content: ContentPart[] = [
      ...(hasText ? [{ type: 'text' as const, content: input }] : []),
      ...fileParts,
    ];
    await sendMessage({ content });
  } else {
    await sendMessage(input);
  }

  return true;
}

export function canSubmit(input: string, files: FileAttachment[], isLoading: boolean): boolean {
  return (input.trim() !== '' || files.length > 0) && !isLoading;
}
