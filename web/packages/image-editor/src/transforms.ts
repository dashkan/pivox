import type {
  CropRect,
  DragHandle,
  ImageEditorEditState,
  ImageEditorState,
} from './types';

/* ------------------------------------------------------------------ */
/*  Initial edit state                                                */
/* ------------------------------------------------------------------ */

export function createInitialEditState(
  naturalWidth: number,
  naturalHeight: number,
  initialCrop?: Partial<CropRect>,
): ImageEditorEditState {
  return {
    cropRect: {
      x: initialCrop?.x ?? 0,
      y: initialCrop?.y ?? 0,
      width: initialCrop?.width ?? naturalWidth,
      height: initialCrop?.height ?? naturalHeight,
    },
    resizeMode: 'crop',
    rotation: 0,
    straighten: 0,
    flipHorizontal: false,
    flipVertical: false,
    activeTemplate: null,
  };
}

/* ------------------------------------------------------------------ */
/*  Coordinate transforms                                             */
/* ------------------------------------------------------------------ */

/** Convert canvas-space pointer coordinates to image-pixel space. */
export function canvasToImage(
  pointerX: number,
  pointerY: number,
  canvasRect: DOMRect,
  scale: number,
  offset: { x: number; y: number },
): { x: number; y: number } {
  return {
    x: (pointerX - canvasRect.left - offset.x) / scale,
    y: (pointerY - canvasRect.top - offset.y) / scale,
  };
}

/** Calculate the scale and offset to fit an image within a container. */
export function computeViewportTransform(
  containerWidth: number,
  containerHeight: number,
  imageWidth: number,
  imageHeight: number,
  zoomPercent: number = 100,
  panOffset: { x: number; y: number } = { x: 0, y: 0 },
): { scale: number; offsetX: number; offsetY: number; fitScale: number } {
  if (imageWidth <= 0 || imageHeight <= 0) {
    return { scale: 1, offsetX: 0, offsetY: 0, fitScale: 1 };
  }

  const scaleX = containerWidth / imageWidth;
  const scaleY = containerHeight / imageHeight;
  const fitScale = Math.min(scaleX, scaleY);
  const scale = fitScale * (zoomPercent / 100);

  const offsetX = (containerWidth - imageWidth * scale) / 2 + panOffset.x;
  const offsetY = (containerHeight - imageHeight * scale) / 2 + panOffset.y;

  return { scale, offsetX, offsetY, fitScale };
}

/* ------------------------------------------------------------------ */
/*  Rotation zoom                                                     */
/* ------------------------------------------------------------------ */

/**
 * Calculate the minimum scale factor needed so that a rotated image
 * completely fills a crop rectangle (no empty pixels).
 *
 * When an image is rotated by `angleDeg` around its center, the image
 * must be scaled up so every pixel in the crop rect is covered.
 * Returns the multiplier (>= 1.0).
 *
 * Uses a corner-based approach: checks all 4 corners of the crop rect,
 * inverse-rotates them back to unrotated image space, and computes the
 * minimum scale so all corners fall within the image bounds.
 */
export function computeRotationZoom(
  imageWidth: number,
  imageHeight: number,
  cropRect: CropRect,
  angleDeg: number,
): number {
  const rad = angleDeg * Math.PI / 180;
  if (Math.abs(rad) < 0.0001) return 1;

  const cosA = Math.cos(rad);
  const sinA = Math.sin(rad);
  const icx = imageWidth / 2;
  const icy = imageHeight / 2;

  // The 4 corners of the crop rect
  const corners = [
    { x: cropRect.x, y: cropRect.y },
    { x: cropRect.x + cropRect.width, y: cropRect.y },
    { x: cropRect.x, y: cropRect.y + cropRect.height },
    { x: cropRect.x + cropRect.width, y: cropRect.y + cropRect.height },
  ];

  // Inverse-rotate each corner around the image center. The zoom must
  // ensure all inverse-rotated points fall within [-icx*s, icx*s] x [-icy*s, icy*s].
  let maxScale = 1;

  for (const corner of corners) {
    const dx = corner.x - icx;
    const dy = corner.y - icy;
    const ux = dx * cosA + dy * sinA;
    const uy = -dx * sinA + dy * cosA;

    if (icx > 0) maxScale = Math.max(maxScale, Math.abs(ux) / icx);
    if (icy > 0) maxScale = Math.max(maxScale, Math.abs(uy) / icy);
  }

  return maxScale;
}

/**
 * Compute the effective image dimensions available for crop rect
 * movement after rotation. The rotated+zoomed image covers a certain
 * area in the original coordinate system; the crop rect can roam
 * within that area.
 *
 * Returns effective width and height that can be passed to clampCropRect.
 */
export function computeEffectiveBounds(
  imageWidth: number,
  imageHeight: number,
  cropWidth: number,
  cropHeight: number,
  angleDeg: number,
): { width: number; height: number } {
  // Normalize to [0, 360)
  const normalized = ((angleDeg % 360) + 360) % 360;

  // For exact 90° multiples, the image dimensions swap
  const is90 = Math.abs(normalized - 90) < 0.01 || Math.abs(normalized - 270) < 0.01;
  const is180 = Math.abs(normalized - 180) < 0.01;

  if (is90) {
    // 90° or 270°: image W/H swap
    return { width: imageHeight, height: imageWidth };
  }
  if (is180 || Math.abs(normalized) < 0.01) {
    return { width: imageWidth, height: imageHeight };
  }

  // For non-right angles, compute the rotation zoom for a centered crop,
  // then use that zoom to expand the effective bounds
  const centerCrop: CropRect = {
    x: (imageWidth - cropWidth) / 2,
    y: (imageHeight - cropHeight) / 2,
    width: cropWidth,
    height: cropHeight,
  };
  const zoom = computeRotationZoom(imageWidth, imageHeight, centerCrop, angleDeg);

  return {
    width: imageWidth * zoom,
    height: imageHeight * zoom,
  };
}

/* ------------------------------------------------------------------ */
/*  Handle hit-testing                                                */
/* ------------------------------------------------------------------ */

/** Get the 8 handle positions for a crop rect (in image-pixel space). */
export function getHandlePositions(
  rect: CropRect,
): Array<{ handle: DragHandle; x: number; y: number }> {
  const { x, y, width, height } = rect;
  const mx = x + width / 2;
  const my = y + height / 2;

  return [
    { handle: 'nw', x, y },
    { handle: 'n', x: mx, y },
    { handle: 'ne', x: x + width, y },
    { handle: 'w', x, y: my },
    { handle: 'e', x: x + width, y: my },
    { handle: 'sw', x, y: y + height },
    { handle: 's', x: mx, y: y + height },
    { handle: 'se', x: x + width, y: y + height },
  ];
}

/** Hit-test a point against crop handles. Returns the handle or null. */
export function hitTestHandles(
  px: number,
  py: number,
  rect: CropRect,
  hitRadius: number,
): DragHandle | null {
  const handles = getHandlePositions(rect);

  for (const { handle, x, y } of handles) {
    const dx = px - x;
    const dy = py - y;
    if (dx * dx + dy * dy <= hitRadius * hitRadius) {
      return handle;
    }
  }

  if (
    px >= rect.x &&
    px <= rect.x + rect.width &&
    py >= rect.y &&
    py <= rect.y + rect.height
  ) {
    return 'move';
  }

  return null;
}

/* ------------------------------------------------------------------ */
/*  Dirty state comparison                                            */
/* ------------------------------------------------------------------ */

export function isEditStateDirty(
  current: ImageEditorEditState,
  initial: ImageEditorEditState,
): boolean {
  return (
    current.cropRect.x !== initial.cropRect.x ||
    current.cropRect.y !== initial.cropRect.y ||
    current.cropRect.width !== initial.cropRect.width ||
    current.cropRect.height !== initial.cropRect.height ||
    current.resizeMode !== initial.resizeMode ||
    current.rotation !== initial.rotation ||
    current.straighten !== initial.straighten ||
    current.flipHorizontal !== initial.flipHorizontal ||
    current.flipVertical !== initial.flipVertical ||
    current.activeTemplate !== initial.activeTemplate
  );
}

/** Extract the ImageEditorEditState subset from full state. */
export function extractEditState(
  state: ImageEditorState,
): ImageEditorEditState {
  return {
    cropRect: state.cropRect,
    resizeMode: state.resizeMode,
    rotation: state.rotation,
    straighten: state.straighten,
    flipHorizontal: state.flipHorizontal,
    flipVertical: state.flipVertical,
    activeTemplate: state.activeTemplate,
  };
}

/* ------------------------------------------------------------------ */
/*  Cursor mapping                                                    */
/* ------------------------------------------------------------------ */

export function handleToCursor(handle: DragHandle | null): string {
  switch (handle) {
    case 'nw':
    case 'se':
      return 'nwse-resize';
    case 'ne':
    case 'sw':
      return 'nesw-resize';
    case 'n':
    case 's':
      return 'ns-resize';
    case 'e':
    case 'w':
      return 'ew-resize';
    case 'move':
      return 'move';
    default:
      return 'default';
  }
}
