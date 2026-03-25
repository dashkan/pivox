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

/* ------------------------------------------------------------------ */
/*  State                                                             */
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

/* ------------------------------------------------------------------ */
/*  Engine options                                                    */
/* ------------------------------------------------------------------ */

/** Options for creating an ImageEditorEngine. */
export interface ImageEditorEngineOptions {
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
  /** Called whenever state changes. */
  onChange?: (state: ImageEditorState) => void;
  /** Called whenever edit state changes (crop, rotation, etc.). */
  onEditChange?: (editState: ImageEditorEditState) => void;
  /**
   * CSS custom property reader for themed colors.
   * The engine calls this to resolve crop overlay colors.
   * If not provided, reads from the canvas container's computed style.
   */
  colors?: CropColors;
}

/** Colors used for crop overlay rendering. */
export interface CropColors {
  /** Canvas background color. */
  canvas: string;
  /** Crop border color. */
  border: string;
  /** Crop handle color. */
  handle: string;
  /** Grid line color. */
  grid: string;
  /** Overlay (dimmed area) color. */
  overlay: string;
}
