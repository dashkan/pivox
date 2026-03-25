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

/**
 * The editable parameters tracked in undo history.
 *
 * Uses the "crop-as-viewport" model: the crop rect is a fixed window,
 * the image transforms (scale, rotate, translate) behind it.
 *
 * - cropWidth/cropHeight define the viewport in image pixels
 * - tx/ty is the image translation (how far the image is panned)
 * - scale is the rendering scale (>= minScale for current rotation)
 * - rotation is 0/90/180/270 quarter turns
 * - straighten is -45..45 fine adjustment
 */
export interface ImageEditorEditState {
  cropWidth: number;
  cropHeight: number;
  rotation: 0 | 90 | 180 | 270;
  straighten: number;
  /** Image scale factor. Always >= minScale to prevent dead pixels. */
  scale: number;
  /** Image X translation in image-pixel units (relative to crop center). */
  tx: number;
  /** Image Y translation in image-pixel units (relative to crop center). */
  ty: number;
  flipHorizontal: boolean;
  flipVertical: boolean;
  activeTemplate: CropTemplate | null;
  resizeMode: ResizeMode;
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
  /** Viewport zoom level as a percentage (100 = fit). */
  zoom: number;
  /** Whether zoom is auto-fit or manually set. */
  zoomMode: ZoomMode;
  /** Pan offset in CSS pixels when viewport-zoomed past fit. */
  panOffset: { x: number; y: number };
  /** Whether the user is currently panning the viewport. */
  isPanning: boolean;
  /** Current editor mode. */
  mode: EditorMode;
}

/* ------------------------------------------------------------------ */
/*  Engine options                                                    */
/* ------------------------------------------------------------------ */

export interface ImageEditorEngineOptions {
  /** Initial image source — URL or base64 data URI. */
  src?: string;
  /** Initial crop dimensions (defaults to full image). */
  initialCropWidth?: number;
  initialCropHeight?: number;
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
  /** Themed colors for crop overlay rendering. */
  colors?: CropColors;
}

/** Colors used for crop overlay rendering. */
export interface CropColors {
  canvas: string;
  border: string;
  handle: string;
  grid: string;
  overlay: string;
}
