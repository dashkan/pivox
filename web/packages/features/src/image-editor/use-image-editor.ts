'use client';

import { useEffect, useMemo } from 'react';
import {
  useImageEditorState,
} from '@pivox/ui/image-editor';
import type {
  CropRect,
  CropTemplate,
  ImageEditorContextValue,
  ImageEditorEditState,
  KeyboardShortcutMap,
} from '@pivox/ui/image-editor';

/* ------------------------------------------------------------------ */
/*  Platform detection                                                */
/* ------------------------------------------------------------------ */

function isMacPlatform(): boolean {
  if (typeof navigator === 'undefined') return false;
  const uaData = (navigator as { userAgentData?: { platform?: string } })
    .userAgentData;
  if (uaData?.platform) {
    return uaData.platform === 'macOS';
  }
  return /Mac|iPhone|iPad|iPod/.test(navigator.userAgent);
}

function buildShortcuts(isMac: boolean): KeyboardShortcutMap {
  const mod = isMac ? '⌘' : 'Ctrl+';
  return {
    undo: `${mod}Z`,
    redo: isMac ? '⌘⇧Z' : 'Ctrl+Y',
    rotateClockwise: ']',
    rotateCounterClockwise: '[',
    flipHorizontal: 'H',
    flipVertical: 'V',
    zoomIn: `${mod}+`,
    zoomOut: `${mod}-`,
    zoomToFit: `${mod}0`,
    reset: isMac ? '⌘⇧R' : 'Ctrl+Shift+R',
  };
}

/* ------------------------------------------------------------------ */
/*  Keyboard event handler                                            */
/* ------------------------------------------------------------------ */

function createKeyHandler(
  actions: ImageEditorContextValue['actions'],
  isMac: boolean,
) {
  return (event: KeyboardEvent) => {
    const mod = isMac ? event.metaKey : event.ctrlKey;
    const shift = event.shiftKey;
    const key = event.key.toLowerCase();

    // Ignore if focused on an input element
    const target = event.target as HTMLElement;
    if (
      target.tagName === 'INPUT' ||
      target.tagName === 'TEXTAREA' ||
      target.isContentEditable
    ) {
      return;
    }

    // Undo: Cmd/Ctrl+Z (without Shift)
    if (mod && !shift && key === 'z') {
      event.preventDefault();
      actions.undo();
      return;
    }

    // Redo: Cmd+Shift+Z (Mac) or Ctrl+Y (Windows)
    if (isMac && mod && shift && key === 'z') {
      event.preventDefault();
      actions.redo();
      return;
    }
    if (!isMac && mod && key === 'y') {
      event.preventDefault();
      actions.redo();
      return;
    }

    // Rotate: [ and ]
    if (!mod && key === '[') {
      event.preventDefault();
      actions.rotateCounterClockwise();
      return;
    }
    if (!mod && key === ']') {
      event.preventDefault();
      actions.rotateClockwise();
      return;
    }

    // Flip: H and V
    if (!mod && !shift && key === 'h') {
      event.preventDefault();
      actions.toggleFlipHorizontal();
      return;
    }
    if (!mod && !shift && key === 'v') {
      event.preventDefault();
      actions.toggleFlipVertical();
      return;
    }

    // Zoom: Cmd/Ctrl + / -
    if (mod && (key === '=' || key === '+')) {
      event.preventDefault();
      actions.zoomIn();
      return;
    }
    if (mod && key === '-') {
      event.preventDefault();
      actions.zoomOut();
      return;
    }
    if (mod && key === '0') {
      event.preventDefault();
      actions.zoomToFit();
      return;
    }

    // Reset: Cmd/Ctrl+Shift+R
    if (mod && shift && key === 'r') {
      event.preventDefault();
      actions.reset();
      return;
    }
  };
}

/* ------------------------------------------------------------------ */
/*  Hook                                                              */
/* ------------------------------------------------------------------ */

export interface UseImageEditorFeatureOptions {
  /** Image source — URL or base64 data URI. */
  src?: string;
  /** Initial crop area. */
  initialCrop?: Partial<CropRect>;
  /** Override default crop templates. */
  templates?: Array<CropTemplate>;
  /** Max undo history depth (default: 50). */
  maxHistory?: number;
  /** Called whenever edit state changes. */
  onChange?: (state: ImageEditorEditState) => void;
  /** Override keyboard shortcuts display strings. */
  shortcuts?: Partial<KeyboardShortcutMap>;
  /** Disable keyboard shortcut handling (default: false). */
  disableKeyboardShortcuts?: boolean;
}

export function useImageEditorFeature(
  options: UseImageEditorFeatureOptions = {},
): ImageEditorContextValue {
  const {
    shortcuts: shortcutOverrides,
    disableKeyboardShortcuts = false,
    ...editorOptions
  } = options;

  const isMac = useMemo(() => isMacPlatform(), []);

  const shortcuts = useMemo(() => {
    const defaults = buildShortcuts(isMac);
    return shortcutOverrides
      ? { ...defaults, ...shortcutOverrides }
      : defaults;
  }, [isMac, shortcutOverrides]);

  const editorState = useImageEditorState({
    ...editorOptions,
    shortcuts,
  });

  // Register keyboard shortcuts
  useEffect(() => {
    if (disableKeyboardShortcuts) return;
    const handler = createKeyHandler(editorState.actions, isMac);
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [editorState.actions, isMac, disableKeyboardShortcuts]);

  return editorState;
}
