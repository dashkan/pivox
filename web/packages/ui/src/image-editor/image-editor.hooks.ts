'use client';

import { ImageEditorEngine } from '@pivox/image-editor';
import { useCallback, useEffect, useMemo, useState } from 'react';

import type {
  ImageEditorActions,
  ImageEditorContextValue,
  ImageEditorMeta,
  KeyboardShortcutMap,
} from './image-editor.types';
import type {
  CropTemplate,
  ImageEditorEditState,
  ImageEditorState,
} from '@pivox/image-editor';

/* ------------------------------------------------------------------ */
/*  Options                                                           */
/* ------------------------------------------------------------------ */

export interface UseImageEditorOptions {
  /** Initial image source — URL or base64 data URI. */
  src?: string;
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

  // Create engine once via useState's lazy initializer. setVersion is stable
  // across renders so it's safe to capture in the engine's onChange closure
  // at construction time (engine constructor wires it before any action can
  // fire). engineOptions are captured at first render only, matching the
  // previous behavior of `if (!engineRef.current)` lazy init.
  //
  // Disabling react-hooks/exhaustive-deps on the initial-options closure:
  // the engine is intentionally constructed once. Updating it on option
  // changes is the engine's job via its own setters, not React's.
  const [engine] = useState(() => {
    const e = new ImageEditorEngine(engineOptions);
    e.onChange = () => {
      setVersion((v) => v + 1);
    };
    return e;
  });

  // Sync the engine's edit-state callback with the latest onChange prop on
  // every render. Doing this in an effect (instead of during render) keeps
  // the rule-of-refs happy and avoids the read-during-render concern.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/immutability -- `engine` is held in useState for identity stability only; it's an imperative resource with its own internal state, not React state. Mutating its callback fields is the engine's intended API.
    engine.onEditChange = onChange
      ? (e) => {
          onChange(e);
        }
      : null;
  }, [engine, onChange]);

  // Tear down the engine on unmount.
  useEffect(() => {
    return () => {
      engine.destroy();
    };
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

  // Read state directly from engine (version counter ensures freshness)
  const state = engine.state as ImageEditorState;

  // Actions — just delegate to engine methods
  const actions: ImageEditorActions = useMemo(
    () => ({
      loadImage: (src) => {
        engine.loadImage(src);
      },
      setResizeMode: (mode) => {
        engine.setResizeMode(mode);
      },
      rotateClockwise: () => {
        engine.rotateClockwise();
      },
      rotateCounterClockwise: () => {
        engine.rotateCounterClockwise();
      },
      setStraighten: (degrees) => {
        engine.setStraighten(degrees);
      },
      commitStraighten: () => {
        engine.commitStraighten();
      },
      toggleFlipHorizontal: () => {
        engine.toggleFlipHorizontal();
      },
      toggleFlipVertical: () => {
        engine.toggleFlipVertical();
      },
      applyTemplate: (template) => {
        engine.applyTemplate(template);
      },
      reset: () => {
        engine.reset();
      },
      undo: () => {
        engine.undo();
      },
      redo: () => {
        engine.redo();
      },
      zoomIn: () => {
        engine.zoomIn();
      },
      zoomOut: () => {
        engine.zoomOut();
      },
      zoomToFit: () => {
        engine.zoomToFit();
      },
      setZoom: (level) => {
        engine.setZoom(level);
      },
      enterCropMode: () => {
        engine.enterCropMode();
      },
      exitCropMode: () => {
        engine.exitCropMode();
      },
    }),
    [engine],
  );

  const meta: ImageEditorMeta = useMemo(
    () => ({ containerRef, shortcuts }),
    [containerRef, shortcuts],
  );

  return { state, actions, meta };
}
