'use client';

import { useCallback, useEffect, useMemo, useReducer, useRef } from 'react';
import { FREE_TEMPLATE } from './image-editor.constants';
import { resizeCropRect } from './image-editor.crop-math';
import {
  canvasToImage,
  createInitialEditState,
  extractEditState,
  isEditStateDirty,
} from './image-editor.transforms';
import { reducer } from './image-editor.reducer';
import { useCanvasRenderer } from './image-editor.canvas';
import type { ReducerState } from './image-editor.reducer';
import type {
  CropRect,
  CropTemplate,
  DragHandle,
  ImageEditorContextValue,
  ImageEditorEditState,
  ImageEditorMeta,
  KeyboardShortcutMap,
  ResizeMode,
} from './image-editor.types';

/* ------------------------------------------------------------------ */
/*  Options                                                           */
/* ------------------------------------------------------------------ */

export interface UseImageEditorOptions {
  /** Initial image source — URL or base64 data URI. */
  src?: string;
  /** Initial crop (defaults to full image). */
  initialCrop?: Partial<CropRect>;
  /**
   * Aspect ratio templates to show in the picker.
   * "Free" is always available and does not need to be included.
   * Defaults to empty (only Free available).
   */
  templates?: Array<CropTemplate>;
  /** Template to auto-select when image loads (defaults to Free). */
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
  const {
    src: initialSrc,
    templates = [],
    defaultTemplate = FREE_TEMPLATE,
    onChange,
    shortcuts = {},
  } = options;

  // Always include Free at the start, filter out any duplicate Free from user list
  const filteredTemplates = templates.filter((t) => t.label !== FREE_TEMPLATE.label);
  const allTemplates = [FREE_TEMPLATE, ...filteredTemplates];

  const initialEditState = createInitialEditState(0, 0, options.initialCrop);
  // Auto-select the default template
  initialEditState.activeTemplate = defaultTemplate;

  const initialState: ReducerState = {
    editor: {
      ...initialEditState,
      src: initialSrc ?? '',
      imageStatus: initialSrc ? 'loading' : 'idle',
      imageError: null,
      naturalWidth: 0,
      naturalHeight: 0,
      templates: allTemplates,
      isDragging: false,
      activeHandle: null,
      canUndo: false,
      canRedo: false,
      isDirty: false,
      zoom: 100,
      zoomMode: 'fit',
      panOffset: { x: 0, y: 0 },
      isPanning: false,
      mode: 'view',
    },
    history: { past: [], future: [] },
    initialEditState,
    preDragEditState: null,
    preStraightenEditState: null,
  };

  const [reducerState, dispatch] = useReducer(reducer, initialState);
  const { editor: state } = reducerState;

  // ── Refs ──────────────────────────────────────────────────────────

  const rootRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const imageRef = useRef<HTMLImageElement | null>(null);
  const scaleRef = useRef(1);
  const offsetRef = useRef({ x: 0, y: 0 });
  const fitScaleRef = useRef(1);

  // Drag origin ref — stores everything needed for drag so onDragMove
  // doesn't depend on state (avoids stale closure on first move).
  const dragOriginRef = useRef<{
    handle: DragHandle;
    pointerX: number;
    pointerY: number;
    originalRect: CropRect;
    aspectRatio: number | null;
    imageWidth: number;
    imageHeight: number;
  } | null>(null);

  // Pan origin ref
  const panOriginRef = useRef<{
    pointerX: number;
    pointerY: number;
    originalOffset: { x: number; y: number };
  } | null>(null);

  // Image load cancellation
  const loadCancelRef = useRef<(() => void) | null>(null);

  // ── Image loading ─────────────────────────────────────────────────

  const loadImageInternal = useCallback((src: string) => {
    loadCancelRef.current?.();
    dispatch({ type: 'LOAD_IMAGE', src });

    let cancelled = false;
    loadCancelRef.current = () => { cancelled = true; };

    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => {
      if (cancelled) return;
      imageRef.current = img;
      dispatch({
        type: 'IMAGE_LOADED',
        width: img.naturalWidth,
        height: img.naturalHeight,
        src,
      });
    };
    img.onerror = () => {
      if (cancelled) return;
      dispatch({ type: 'IMAGE_ERROR', error: `Failed to load image: ${src}` });
    };
    img.src = src;
  }, []);

  // Load initial src
  useEffect(() => {
    if (initialSrc) {
      loadImageInternal(initialSrc);
    }
    return () => { loadCancelRef.current?.(); };

  }, []);

  // ── onChange callback ─────────────────────────────────────────────

  const prevEditRef = useRef<ImageEditorEditState | null>(null);
  useEffect(() => {
    if (!onChange) return;
    const currentEdit = extractEditState(state);
    if (prevEditRef.current !== null) {
      const prev = prevEditRef.current;
      if (isEditStateDirty(currentEdit, prev)) {
        onChange(currentEdit);
      }
    }
    prevEditRef.current = currentEdit;
  }, [state, onChange]);

  // ── Canvas renderer ───────────────────────────────────────────────

  useCanvasRenderer(state, canvasRef, rootRef, imageRef, scaleRef, offsetRef, fitScaleRef);

  // ── Actions ───────────────────────────────────────────────────────

  const actions = useMemo(
    () => ({
      loadImage: loadImageInternal,

      setCropRect: (rect: CropRect) =>
        dispatch({ type: 'SET_CROP_RECT', rect }),

      setResizeMode: (mode: ResizeMode) =>
        dispatch({ type: 'SET_RESIZE_MODE', mode }),

      rotateClockwise: () => dispatch({ type: 'ROTATE_CLOCKWISE' }),
      rotateCounterClockwise: () => dispatch({ type: 'ROTATE_COUNTER_CLOCKWISE' }),

      setStraighten: (degrees: number) =>
        dispatch({ type: 'SET_STRAIGHTEN_PREVIEW', degrees }),

      commitStraighten: () =>
        dispatch({ type: 'COMMIT_STRAIGHTEN' }),

      toggleFlipHorizontal: () => dispatch({ type: 'TOGGLE_FLIP_HORIZONTAL' }),
      toggleFlipVertical: () => dispatch({ type: 'TOGGLE_FLIP_VERTICAL' }),

      applyTemplate: (template: CropTemplate | null) =>
        dispatch({ type: 'APPLY_TEMPLATE', template }),

      reset: () => dispatch({ type: 'RESET' }),
      undo: () => dispatch({ type: 'UNDO' }),
      redo: () => dispatch({ type: 'REDO' }),

      zoomIn: () => dispatch({ type: 'ZOOM_IN' }),
      zoomOut: () => dispatch({ type: 'ZOOM_OUT' }),
      zoomToFit: () => dispatch({ type: 'ZOOM_TO_FIT' }),
      setZoom: (level: number) => dispatch({ type: 'SET_ZOOM', level }),

      enterCropMode: () => dispatch({ type: 'ENTER_CROP_MODE' }),
      exitCropMode: () => dispatch({ type: 'EXIT_CROP_MODE' }),

      onDragStart: (handle: DragHandle, x: number, y: number) => {
        const canvas = canvasRef.current;
        if (!canvas) return;
        const canvasRect = canvas.getBoundingClientRect();
        const imagePoint = canvasToImage(
          x, y, canvasRect, scaleRef.current, offsetRef.current,
        );
        dragOriginRef.current = {
          handle,
          pointerX: imagePoint.x,
          pointerY: imagePoint.y,
          originalRect: { ...state.cropRect },
          aspectRatio: state.activeTemplate?.ratio ?? null,
          imageWidth: state.naturalWidth,
          imageHeight: state.naturalHeight,
        };
        dispatch({ type: 'DRAG_START', handle, x, y });
      },

      onDragMove: (x: number, y: number) => {
        const canvas = canvasRef.current;
        const origin = dragOriginRef.current;
        if (!canvas || !origin) return;
        const canvasRect = canvas.getBoundingClientRect();
        const imagePoint = canvasToImage(
          x, y, canvasRect, scaleRef.current, offsetRef.current,
        );
        const deltaX = imagePoint.x - origin.pointerX;
        const deltaY = imagePoint.y - origin.pointerY;
        const newRect = resizeCropRect(
          origin.originalRect,
          origin.handle,
          deltaX,
          deltaY,
          origin.imageWidth,
          origin.imageHeight,
          origin.aspectRatio,
        );
        dispatch({ type: 'DRAG_MOVE', rect: newRect });
      },

      onDragEnd: () => {
        dragOriginRef.current = null;
        dispatch({ type: 'DRAG_END' });
      },

      onPanStart: (x: number, y: number) => {
        panOriginRef.current = {
          pointerX: x,
          pointerY: y,
          originalOffset: { ...state.panOffset },
        };
        dispatch({ type: 'PAN_START' });
      },

      onPanMove: (x: number, y: number) => {
        const origin = panOriginRef.current;
        if (!origin) return;
        const dx = x - origin.pointerX;
        const dy = y - origin.pointerY;
        dispatch({
          type: 'PAN_MOVE',
          offset: {
            x: origin.originalOffset.x + dx,
            y: origin.originalOffset.y + dy,
          },
        });
      },

      onPanEnd: () => {
        panOriginRef.current = null;
        dispatch({ type: 'PAN_END' });
      },
    }),
    [loadImageInternal, state.cropRect, state.naturalWidth, state.naturalHeight, state.activeTemplate, state.panOffset],
  );

  // ── Meta (uses getters for always-current ref values) ─────────────

  const meta: ImageEditorMeta = useMemo(
    () => ({
      rootRef,
      canvasRef,
      get scale() { return scaleRef.current; },
      get canvasOffset() { return offsetRef.current; },
      shortcuts,
    }),

    [shortcuts],
  );

  return { state, actions, meta };
}
