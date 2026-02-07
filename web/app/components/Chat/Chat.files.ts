import type { FileAttachment, SerializedFile } from './Chat.types';

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

export async function serializeFiles(files: FileAttachment[]): Promise<SerializedFile[]> {
  return Promise.all(
    files.map(async (f) => ({
      name: f.name,
      type: f.type,
      size: f.size,
      data: await fileToBase64(f.file),
    }))
  );
}

/**
 * Sends a message with optional file attachments.
 * Returns `true` if a message was sent, `false` if skipped (empty input / loading).
 */
export async function submitMessage(params: {
  input: string;
  files: FileAttachment[];
  isLoading: boolean;
  sendMessage: (content: string) => Promise<void>;
  append: (message: any) => Promise<void>;
}): Promise<boolean> {
  const { input, files, isLoading, sendMessage, append } = params;
  const hasText = input.trim() !== '';
  const hasFiles = files.length > 0;

  if ((!hasText && !hasFiles) || isLoading) {
    return false;
  }

  if (hasFiles) {
    const serializedFiles = await serializeFiles(files);

    await append({
      role: 'user' as const,
      parts: hasText ? [{ type: 'text' as const, content: input }] : [],
      _files: serializedFiles,
    });
  } else {
    await sendMessage(input);
  }

  return true;
}

export function canSubmit(input: string, files: FileAttachment[], isLoading: boolean): boolean {
  return (input.trim() !== '' || files.length > 0) && !isLoading;
}
