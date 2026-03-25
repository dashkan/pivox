// Re-export all types from the vanilla engine
import type { CropRect, CropTemplate, ImageEditorState, ResizeMode } from '@pivox/image-editor';

export type {
  CropColors,
  CropRect,
  CropTemplate,
  DragHandle,
  EditorMode,
  ImageEditorEditState,
  ImageEditorEngineOptions,
  ImageEditorState,
  ResizeMode,
  ZoomMode,
} from '@pivox/image-editor';

/* ------------------------------------------------------------------ */
/*  React-specific types                                              */
/* ------------------------------------------------------------------ */

/** Display strings for keyboard shortcut hints shown in tooltips. */
export interface KeyboardShortcutMap {
  undo: string;
  redo: string;
  rotateClockwise: string;
  rotateCounterClockwise: string;
  flipHorizontal: string;
  flipVertical: string;
  zoomIn: string;
  zoomOut: string;
  zoomToFit: string;
  reset: string;
}

/** Actions exposed to React subcomponents via context. */
export interface ImageEditorActions {
  loadImage: (src: string) => void;
  setCropRect: (rect: CropRect) => void;
  setResizeMode: (mode: ResizeMode) => void;
  rotateClockwise: () => void;
  rotateCounterClockwise: () => void;
  setStraighten: (degrees: number) => void;
  commitStraighten: () => void;
  toggleFlipHorizontal: () => void;
  toggleFlipVertical: () => void;
  applyTemplate: (template: CropTemplate | null) => void;
  reset: () => void;
  undo: () => void;
  redo: () => void;
  zoomIn: () => void;
  zoomOut: () => void;
  zoomToFit: () => void;
  setZoom: (level: number) => void;
  enterCropMode: () => void;
  exitCropMode: () => void;
}

/** Meta values exposed to React subcomponents via context. */
export interface ImageEditorMeta {
  /** Ref callback to attach to the canvas container element. */
  containerRef: (el: HTMLDivElement | null) => void;
  /** Optional keyboard shortcut display strings for tooltips. */
  shortcuts: Partial<KeyboardShortcutMap>;
}

/** The full context value passed to ImageEditor.Provider. */
export interface ImageEditorContextValue {
  state: ImageEditorState;
  actions: ImageEditorActions;
  meta: ImageEditorMeta;
}
