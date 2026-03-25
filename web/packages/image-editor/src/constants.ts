import type { CropTemplate } from './types';

/** The built-in Free template (always available). */
export const FREE_TEMPLATE: CropTemplate = { label: 'Free', ratio: null };

/** Minimum crop size in pixels. */
export const MIN_CROP_SIZE = 10;

/** Zoom step for zoom in/out buttons (percentage points). */
export const ZOOM_STEP = 25;

/** Minimum zoom level (percentage). */
export const ZOOM_MIN = 10;

/** Maximum zoom level (percentage). */
export const ZOOM_MAX = 800;

/** Default maximum undo history depth. */
export const MAX_HISTORY = 50;

/** Default crop templates (broadcast set). Free is always built-in. */
export const DEFAULT_CROP_TEMPLATES: Array<CropTemplate> = [
  { label: '16:9', ratio: 16 / 9 },
  { label: '4:3', ratio: 4 / 3 },
  { label: '1:1', ratio: 1 },
  { label: '9:16', ratio: 9 / 16 },
  { label: '3:4', ratio: 3 / 4 },
  { label: '21:9', ratio: 21 / 9 },
  { label: '2.39:1', ratio: 2.39 },
  { label: '1.85:1', ratio: 1.85 },
  { label: '2:1', ratio: 2 },
];
