/* ------------------------------------------------------------------ */
/*  Geometry primitives                                               */
/* ------------------------------------------------------------------ */

/** Pixel-space rectangle. Origin is top-left of the source image. */
export interface CropRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** Resize strategy when crop doesn't match target dimensions. */
export type ResizeMode = 'crop' | 'cover' | 'fit';

/** A named aspect-ratio preset. `null` ratio means freeform. */
export interface CropTemplate {
  label: string;
  /** width / height, or null for freeform. */
  ratio: number | null;
}

/** Which edge/corner the user is dragging, or 'move' for the whole rect. */
export type DragHandle =
  | 'nw'
  | 'n'
  | 'ne'
  | 'w'
  | 'e'
  | 'sw'
  | 's'
  | 'se'
  | 'move';

/** Zoom mode: 'fit' auto-fits to container, 'manual' uses explicit level. */
export type ZoomMode = 'fit' | 'manual';

/** Editor mode: 'view' shows the cropped result, 'crop' shows the editing UI. */
export type EditorMode = 'view' | 'crop';

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

/* ------------------------------------------------------------------ */
/*  Context value                                                     */
/* ------------------------------------------------------------------ */

/** The editable parameters tracked in undo history. */
export interface ImageEditorEditState {
  cropRect: CropRect;
  resizeMode: ResizeMode;
  rotation: 0 | 90 | 180 | 270;
  straighten: number;
  flipHorizontal: boolean;
  flipVertical: boolean;
  activeTemplate: CropTemplate | null;
}

export interface ImageEditorState extends ImageEditorEditState {
  /** Image source — URL string or base64 data URI. */
  src: string;
  /** Loading state for image fetch. */
  imageStatus: 'idle' | 'loading' | 'loaded' | 'error';
  /** Error message if image failed to load. */
  imageError: string | null;
  /** Natural pixel dimensions (set once image loads). */
  naturalWidth: number;
  naturalHeight: number;
  /** Available crop templates. */
  templates: Array<CropTemplate>;
  /** Drag interaction state. */
  isDragging: boolean;
  activeHandle: DragHandle | null;
  /** Undo/redo availability. */
  canUndo: boolean;
  canRedo: boolean;
  /** True if any edit differs from initial state. */
  isDirty: boolean;
  /** Zoom level as a percentage (100 = 100%). */
  zoom: number;
  /** Whether zoom is auto-fit or manually set. */
  zoomMode: ZoomMode;
  /** Pan offset in CSS pixels when zoomed past fit. */
  panOffset: { x: number; y: number };
  /** Whether the user is currently panning. */
  isPanning: boolean;
  /** Current editor mode. */
  mode: EditorMode;
}

export interface ImageEditorActions {
  /** Load a new image (URL or base64 data URI). */
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
  /** Zoom controls. */
  zoomIn: () => void;
  zoomOut: () => void;
  zoomToFit: () => void;
  setZoom: (level: number) => void;
  /** Mode controls. */
  enterCropMode: () => void;
  exitCropMode: () => void;
  /** Drag handle lifecycle — used by Canvas internally. */
  onDragStart: (handle: DragHandle, x: number, y: number) => void;
  onDragMove: (x: number, y: number) => void;
  onDragEnd: () => void;
  /** Pan lifecycle — used by Canvas internally when zoomed. */
  onPanStart: (x: number, y: number) => void;
  onPanMove: (x: number, y: number) => void;
  onPanEnd: () => void;
}

export interface ImageEditorMeta {
  /** Ref to the outer container div (for measuring viewport size). */
  rootRef: React.RefObject<HTMLDivElement | null>;
  /** Ref to the HTML5 canvas element. */
  canvasRef: React.RefObject<HTMLCanvasElement | null>;
  /** Conversion factor: image pixels to CSS pixels on the canvas. */
  scale: number;
  /** Offset of the image origin within the canvas element. */
  canvasOffset: { x: number; y: number };
  /** Optional keyboard shortcut display strings for tooltips. */
  shortcuts: Partial<KeyboardShortcutMap>;
}

export interface ImageEditorContextValue {
  state: ImageEditorState;
  actions: ImageEditorActions;
  meta: ImageEditorMeta;
}


