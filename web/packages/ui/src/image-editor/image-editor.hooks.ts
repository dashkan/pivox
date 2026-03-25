'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ImageEditorEngine } from '@pivox/image-editor';
import type { CropRect, CropTemplate, ImageEditorEditState, ImageEditorState } from '@pivox/image-editor';
import type { ImageEditorActions, ImageEditorContextValue, ImageEditorMeta, KeyboardShortcutMap } from './image-editor.types';

/* ------------------------------------------------------------------ */
/*  Options                                                           */
/* ------------------------------------------------------------------ */

export interface UseImageEditorOptions {
  /** Initial image source — URL or base64 data URI. */
  src?: string;
  /** Initial crop (defaults to full image). */
  initialCrop?: Partial<CropRect>;
  /** Aspect ratio templates (Free is always built-in). */
  templates?: Array<CropTemplate>;
  /** Template to auto-select when image loads. */
  defaultTemplate?: CropTemplate;
  /** Max undo history depth (default: 50). */
  maxHistory?: number;
  /** Called whenever edit state changes. */
  onChange?: (state: ImageEditorEditState) => void;
  /** Keyboard shortcut display strings for tooltips. */
  shortcuts?: Partial<KeyboardShortcutMap>;
}

/* ------------------------------------------------------------------ */
/*  Hook                                                              */
/* ------------------------------------------------------------------ */

export function useImageEditorState(
  options: UseImageEditorOptions = {},
): ImageEditorContextValue {
  const { shortcuts = {}, onChange, ...engineOptions } = options;

  // Create engine once
  const engineRef = useRef<ImageEditorEngine | null>(null);
  if (!engineRef.current) {
    engineRef.current = new ImageEditorEngine(engineOptions);
  }
  const engine = engineRef.current;

  // Sync state from engine → React
  const [state, setState] = useState<ImageEditorState>(() => engine.state);

  useEffect(() => {
    engine.onChange = setState;
    engine.onEditChange = onChange ?? null;
    return () => {
      engine.onChange = null;
      engine.onEditChange = null;
    };
  }, [engine, onChange]);

  // Clean up on unmount
  useEffect(() => {
    return () => engine.destroy();
  }, [engine]);

  // Ref callback for the canvas container — mounts/unmounts the engine
  const containerRef = useCallback(
    (el: HTMLDivElement | null) => {
      if (el) {
        engine.mount(el);
      } else {
        engine.unmount();
      }
    },
    [engine],
  );

  // Actions — just delegate to engine methods (no stale closures!)
  const actions: ImageEditorActions = useMemo(
    () => ({
      loadImage: (src) => engine.loadImage(src),
      setCropRect: (rect) => engine.setCropRect(rect),
      setResizeMode: (mode) => engine.setResizeMode(mode),
      rotateClockwise: () => engine.rotateClockwise(),
      rotateCounterClockwise: () => engine.rotateCounterClockwise(),
      setStraighten: (degrees) => engine.setStraighten(degrees),
      commitStraighten: () => engine.commitStraighten(),
      toggleFlipHorizontal: () => engine.toggleFlipHorizontal(),
      toggleFlipVertical: () => engine.toggleFlipVertical(),
      applyTemplate: (template) => engine.applyTemplate(template),
      reset: () => engine.reset(),
      undo: () => engine.undo(),
      redo: () => engine.redo(),
      zoomIn: () => engine.zoomIn(),
      zoomOut: () => engine.zoomOut(),
      zoomToFit: () => engine.zoomToFit(),
      setZoom: (level) => engine.setZoom(level),
      enterCropMode: () => engine.enterCropMode(),
      exitCropMode: () => engine.exitCropMode(),
    }),
    [engine],
  );

  const meta: ImageEditorMeta = useMemo(
    () => ({ containerRef, shortcuts }),
    [containerRef, shortcuts],
  );

  return { state, actions, meta };
}
