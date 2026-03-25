import { MAX_HISTORY, ZOOM_MAX, ZOOM_MIN, ZOOM_STEP } from './image-editor.constants';
import { applyCropTemplate, clampCropRect } from './image-editor.crop-math';
import {
  createInitialEditState,
  extractEditState,
  isEditStateDirty,
} from './image-editor.transforms';
import type {
  CropRect,
  CropTemplate,
  DragHandle,
  ImageEditorEditState,
  ImageEditorState,
  ResizeMode,
} from './image-editor.types';

/* ------------------------------------------------------------------ */
/*  Undo history                                                      */
/* ------------------------------------------------------------------ */

interface UndoHistory {
  past: Array<ImageEditorEditState>;
  future: Array<ImageEditorEditState>;
}

function pushHistory(
  history: UndoHistory,
  current: ImageEditorEditState,
  maxHistory: number,
): UndoHistory {
  const past = [...history.past, current];
  if (past.length > maxHistory) past.shift();
  return { past, future: [] };
}

/* ------------------------------------------------------------------ */
/*  Reducer state & actions                                           */
/* ------------------------------------------------------------------ */

export interface ReducerState {
  editor: ImageEditorState;
  history: UndoHistory;
  initialEditState: ImageEditorEditState;
  /** Snapshot captured at DRAG_START for correct undo on DRAG_END. */
  preDragEditState: ImageEditorEditState | null;
  /** Snapshot captured when straighten slider starts dragging. */
  preStraightenEditState: ImageEditorEditState | null;
}

export type Action =
  | { type: 'LOAD_IMAGE'; src: string }
  | { type: 'IMAGE_LOADED'; width: number; height: number; src: string }
  | { type: 'IMAGE_ERROR'; error: string }
  | { type: 'SET_CROP_RECT'; rect: CropRect }
  | { type: 'SET_RESIZE_MODE'; mode: ResizeMode }
  | { type: 'ROTATE_CLOCKWISE' }
  | { type: 'ROTATE_COUNTER_CLOCKWISE' }
  | { type: 'SET_STRAIGHTEN'; degrees: number }
  | { type: 'TOGGLE_FLIP_HORIZONTAL' }
  | { type: 'TOGGLE_FLIP_VERTICAL' }
  | { type: 'APPLY_TEMPLATE'; template: CropTemplate | null }
  | { type: 'DRAG_START'; handle: DragHandle; x: number; y: number }
  | { type: 'DRAG_MOVE'; rect: CropRect }
  | { type: 'DRAG_END' }
  | { type: 'UNDO' }
  | { type: 'REDO' }
  | { type: 'RESET' }
  | { type: 'ZOOM_IN' }
  | { type: 'ZOOM_OUT' }
  | { type: 'ZOOM_TO_FIT' }
  | { type: 'SET_ZOOM'; level: number }
  | { type: 'PAN_START' }
  | { type: 'PAN_MOVE'; offset: { x: number; y: number } }
  | { type: 'PAN_END' }
  | { type: 'ENTER_CROP_MODE' }
  | { type: 'EXIT_CROP_MODE' }
  | { type: 'SET_STRAIGHTEN_PREVIEW'; degrees: number }
  | { type: 'COMMIT_STRAIGHTEN' };

/* ------------------------------------------------------------------ */
/*  Helpers                                                           */
/* ------------------------------------------------------------------ */

function withHistory(
  prev: ReducerState,
  next: Partial<ImageEditorState>,
): ReducerState {
  const currentEdit = extractEditState(prev.editor);
  const newEditor = { ...prev.editor, ...next };
  const newEditState = extractEditState(newEditor);

  return {
    ...prev,
    editor: {
      ...newEditor,
      canUndo: true,
      canRedo: false,
      isDirty: isEditStateDirty(newEditState, prev.initialEditState),
    },
    history: pushHistory(prev.history, currentEdit, MAX_HISTORY),
  };
}

function clampZoom(level: number): number {
  return Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, Math.round(level)));
}

/* ------------------------------------------------------------------ */
/*  Reducer                                                           */
/* ------------------------------------------------------------------ */

export function reducer(state: ReducerState, action: Action): ReducerState {
  switch (action.type) {
    case 'LOAD_IMAGE':
      return {
        ...state,
        editor: {
          ...state.editor,
          src: action.src,
          imageStatus: 'loading',
          imageError: null,
        },
      };

    case 'IMAGE_LOADED': {
      const editState = createInitialEditState(action.width, action.height);
      // Preserve the activeTemplate from current state (set by defaultTemplate option)
      editState.activeTemplate = state.editor.activeTemplate;
      return {
        editor: {
          ...state.editor,
          ...editState,
          src: action.src,
          imageStatus: 'loaded',
          imageError: null,
          naturalWidth: action.width,
          naturalHeight: action.height,
          canUndo: false,
          canRedo: false,
          isDirty: false,
          isDragging: false,
          activeHandle: null,
          zoom: 100,
          zoomMode: 'fit',
          panOffset: { x: 0, y: 0 },
          isPanning: false,
          mode: 'view',
        },
        history: { past: [], future: [] },
        initialEditState: editState,
        preDragEditState: null,
        preStraightenEditState: null,
      };
    }

    case 'IMAGE_ERROR':
      return {
        ...state,
        editor: {
          ...state.editor,
          imageStatus: 'error',
          imageError: action.error,
        },
      };

    case 'SET_CROP_RECT': {
      const rect = clampCropRect(
        action.rect,
        state.editor.naturalWidth,
        state.editor.naturalHeight,
      );
      return withHistory(state, { cropRect: rect });
    }

    case 'SET_RESIZE_MODE':
      return withHistory(state, { resizeMode: action.mode });

    case 'ROTATE_CLOCKWISE': {
      const rotation = ((state.editor.rotation + 90) % 360) as
        | 0
        | 90
        | 180
        | 270;
      const { naturalWidth, naturalHeight, cropRect } = state.editor;
      const newCrop = clampCropRect(
        {
          x: cropRect.y,
          y: naturalWidth - cropRect.x - cropRect.width,
          width: cropRect.height,
          height: cropRect.width,
        },
        naturalHeight,
        naturalWidth,
      );
      return withHistory(state, { rotation, cropRect: newCrop });
    }

    case 'ROTATE_COUNTER_CLOCKWISE': {
      const rotation = ((state.editor.rotation + 270) % 360) as
        | 0
        | 90
        | 180
        | 270;
      const { naturalWidth, naturalHeight, cropRect } = state.editor;
      const newCrop = clampCropRect(
        {
          x: naturalHeight - cropRect.y - cropRect.height,
          y: cropRect.x,
          width: cropRect.height,
          height: cropRect.width,
        },
        naturalHeight,
        naturalWidth,
      );
      return withHistory(state, { rotation, cropRect: newCrop });
    }

    case 'SET_STRAIGHTEN': {
      const degrees = Math.max(-45, Math.min(45, action.degrees));
      return withHistory(state, { straighten: degrees });
    }

    case 'TOGGLE_FLIP_HORIZONTAL':
      return withHistory(state, {
        flipHorizontal: !state.editor.flipHorizontal,
      });

    case 'TOGGLE_FLIP_VERTICAL':
      return withHistory(state, {
        flipVertical: !state.editor.flipVertical,
      });

    case 'APPLY_TEMPLATE': {
      const template = action.template;
      if (!template) {
        return withHistory(state, { activeTemplate: null });
      }
      const newCrop = applyCropTemplate(
        template,
        state.editor.cropRect,
        state.editor.naturalWidth,
        state.editor.naturalHeight,
      );
      return withHistory(state, {
        activeTemplate: template,
        cropRect: newCrop,
      });
    }

    case 'DRAG_START':
      return {
        ...state,
        preDragEditState: extractEditState(state.editor),
        editor: {
          ...state.editor,
          isDragging: true,
          activeHandle: action.handle,
        },
      };

    case 'DRAG_MOVE':
      return {
        ...state,
        editor: { ...state.editor, cropRect: action.rect },
      };

    case 'DRAG_END': {
      const editState = extractEditState(state.editor);
      const preDrag = state.preDragEditState ?? state.initialEditState;
      return {
        ...state,
        preDragEditState: null,
        editor: {
          ...state.editor,
          isDragging: false,
          activeHandle: null,
          canUndo: true,
          canRedo: false,
          isDirty: isEditStateDirty(editState, state.initialEditState),
        },
        history: pushHistory(state.history, preDrag, MAX_HISTORY),
      };
    }

    case 'UNDO': {
      if (state.history.past.length === 0) return state;
      const past = [...state.history.past];
      const previous = past.pop()!;
      const currentEdit = extractEditState(state.editor);
      return {
        ...state,
        editor: {
          ...state.editor,
          ...previous,
          canUndo: past.length > 0,
          canRedo: true,
          isDirty: isEditStateDirty(previous, state.initialEditState),
        },
        history: {
          past,
          future: [currentEdit, ...state.history.future],
        },
      };
    }

    case 'REDO': {
      if (state.history.future.length === 0) return state;
      const future = [...state.history.future];
      const next = future.shift()!;
      const currentEdit = extractEditState(state.editor);
      return {
        ...state,
        editor: {
          ...state.editor,
          ...next,
          canUndo: true,
          canRedo: future.length > 0,
          isDirty: isEditStateDirty(next, state.initialEditState),
        },
        history: {
          past: [...state.history.past, currentEdit],
          future,
        },
      };
    }

    // Reset is undoable — it's just another action that pushes to history
    case 'RESET':
      return withHistory(state, { ...state.initialEditState });

    /* ── Zoom ──────────────────────────────────────────────────────── */

    case 'ZOOM_IN':
      return {
        ...state,
        editor: {
          ...state.editor,
          zoom: clampZoom(state.editor.zoom + ZOOM_STEP),
          zoomMode: 'manual',
        },
      };

    case 'ZOOM_OUT':
      return {
        ...state,
        editor: {
          ...state.editor,
          zoom: clampZoom(state.editor.zoom - ZOOM_STEP),
          zoomMode: 'manual',
        },
      };

    case 'ZOOM_TO_FIT':
      return {
        ...state,
        editor: {
          ...state.editor,
          zoom: 100,
          zoomMode: 'fit',
          panOffset: { x: 0, y: 0 },
        },
      };

    case 'SET_ZOOM':
      return {
        ...state,
        editor: {
          ...state.editor,
          zoom: clampZoom(action.level),
          zoomMode: 'manual',
        },
      };

    /* ── Pan ───────────────────────────────────────────────────────── */

    case 'PAN_START':
      return {
        ...state,
        editor: { ...state.editor, isPanning: true },
      };

    case 'PAN_MOVE':
      return {
        ...state,
        editor: { ...state.editor, panOffset: action.offset },
      };

    case 'PAN_END':
      return {
        ...state,
        editor: { ...state.editor, isPanning: false },
      };

    /* ── Mode ──────────────────────────────────────────────────────── */

    case 'ENTER_CROP_MODE':
      return {
        ...state,
        editor: { ...state.editor, mode: 'crop' },
      };

    case 'EXIT_CROP_MODE':
      return {
        ...state,
        editor: { ...state.editor, mode: 'view' },
      };

    /* ── Straighten preview (no history until commit) ──────────────── */

    case 'SET_STRAIGHTEN_PREVIEW': {
      const degrees = Math.max(-45, Math.min(45, action.degrees));
      // Capture pre-straighten state on first preview
      const preStraighten = state.preStraightenEditState ?? extractEditState(state.editor);
      return {
        ...state,
        preStraightenEditState: preStraighten,
        editor: { ...state.editor, straighten: degrees },
      };
    }

    case 'COMMIT_STRAIGHTEN': {
      if (!state.preStraightenEditState) return state;
      const editState = extractEditState(state.editor);
      return {
        ...state,
        preStraightenEditState: null,
        editor: {
          ...state.editor,
          canUndo: true,
          canRedo: false,
          isDirty: isEditStateDirty(editState, state.initialEditState),
        },
        history: pushHistory(state.history, state.preStraightenEditState, MAX_HISTORY),
      };
    }

    default:
      return state;
  }
}
