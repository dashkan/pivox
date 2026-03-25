'use client';

import { createContext, use } from 'react';
import type { ImageEditorContextValue } from './image-editor.types';

export const ImageEditorContext =
  createContext<ImageEditorContextValue | null>(null);

export function useImageEditorContext() {
  const ctx = use(ImageEditorContext);
  if (!ctx) {
    throw new Error(
      'ImageEditor subcomponents must be used within an ImageEditor.Provider',
    );
  }
  return ctx;
}
