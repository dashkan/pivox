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

  // Force re-render counter — incremented by engine onChange
  const [, setVersion] = useState(0);

  // Create engine once, wire up onChange immediately
  const engineRef = useRef<ImageEditorEngine | null>(null);
  if (!engineRef.current) {
    const engine = new ImageEditorEngine(engineOptions);
    // Wire up onChange BEFORE any actions can be called.
    // This increments a version counter to trigger React re-renders.
    engine.onChange = () => setVersion((v) => v + 1);
    engineRef.current = engine;
  }
  const engine = engineRef.current;

  // Keep onEditChange in sync
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  engine.onEditChange = onChangeRef.current
    ? (e) => onChangeRef.current?.(e)
    : null;

  // Clean up on unmount
  useEffect(() => {
    return () => {
      engineRef.current?.destroy();
      engineRef.current = null;
    };
  }, []);

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

  // Read state directly from engine (version counter ensures freshness)
  const state = engine.state as ImageEditorState;

  // Actions — just delegate to engine methods
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
