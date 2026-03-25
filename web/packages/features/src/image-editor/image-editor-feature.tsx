'use client';

import { ImageEditor } from '@pivox/ui/image-editor';
import { useImageEditorFeature } from './use-image-editor';
import type { UseImageEditorFeatureOptions } from './use-image-editor';

export function ImageEditorFeature({
  children,
  ...options
}: UseImageEditorFeatureOptions & {
  children: React.ReactNode;
}) {
  const value = useImageEditorFeature(options);

  return <ImageEditor.Provider value={value}>{children}</ImageEditor.Provider>;
}
