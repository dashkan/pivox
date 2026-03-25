import { MIN_CROP_SIZE } from './image-editor.constants';
import type { CropRect, CropTemplate, DragHandle } from './image-editor.types';

/** Clamp a crop rect within image bounds, enforcing minimum size. */
export function clampCropRect(
  rect: CropRect,
  imageWidth: number,
  imageHeight: number,
): CropRect {
  let { x, y, width, height } = rect;

  width = Math.max(width, MIN_CROP_SIZE);
  height = Math.max(height, MIN_CROP_SIZE);
  width = Math.min(width, imageWidth);
  height = Math.min(height, imageHeight);
  x = Math.max(0, Math.min(x, imageWidth - width));
  y = Math.max(0, Math.min(y, imageHeight - height));

  return { x, y, width, height };
}

/** Apply an aspect ratio template to a crop rect, maximizing area. */
export function applyCropTemplate(
  template: CropTemplate,
  currentRect: CropRect,
  imageWidth: number,
  imageHeight: number,
): CropRect {
  if (template.ratio === null) return currentRect;

  const ratio = template.ratio;
  const centerX = currentRect.x + currentRect.width / 2;
  const centerY = currentRect.y + currentRect.height / 2;

  let width: number;
  let height: number;

  if (imageWidth / imageHeight > ratio) {
    height = imageHeight;
    width = height * ratio;
  } else {
    width = imageWidth;
    height = width / ratio;
  }

  let x = centerX - width / 2;
  let y = centerY - height / 2;
  x = Math.max(0, Math.min(x, imageWidth - width));
  y = Math.max(0, Math.min(y, imageHeight - height));

  return { x, y, width, height };
}

/** Resize a crop rect from a specific handle, optionally enforcing aspect ratio. */
export function resizeCropRect(
  originalRect: CropRect,
  handle: DragHandle,
  deltaX: number,
  deltaY: number,
  imageWidth: number,
  imageHeight: number,
  aspectRatio: number | null,
): CropRect {
  let { x, y, width, height } = originalRect;

  switch (handle) {
    case 'move':
      x += deltaX;
      y += deltaY;
      break;
    case 'nw':
      x += deltaX;
      y += deltaY;
      width -= deltaX;
      height -= deltaY;
      break;
    case 'n':
      y += deltaY;
      height -= deltaY;
      break;
    case 'ne':
      y += deltaY;
      width += deltaX;
      height -= deltaY;
      break;
    case 'w':
      x += deltaX;
      width -= deltaX;
      break;
    case 'e':
      width += deltaX;
      break;
    case 'sw':
      x += deltaX;
      width -= deltaX;
      height += deltaY;
      break;
    case 's':
      height += deltaY;
      break;
    case 'se':
      width += deltaX;
      height += deltaY;
      break;
  }

  // Enforce aspect ratio
  if (aspectRatio !== null && handle !== 'move') {
    const isCorner = ['nw', 'ne', 'sw', 'se'].includes(handle);
    const isHorizontal = ['w', 'e'].includes(handle);

    if (isCorner) {
      const candidateWidth = height * aspectRatio;
      const candidateHeight = width / aspectRatio;
      if (candidateWidth <= imageWidth) {
        width = candidateWidth;
      } else {
        height = candidateHeight;
      }
    } else if (isHorizontal) {
      height = width / aspectRatio;
    } else {
      width = height * aspectRatio;
    }

    // Keep the anchor corner fixed after aspect-ratio correction.
    // nw: anchor = bottom-right -> recalc x and y
    // ne: anchor = bottom-left -> recalc y only
    // sw: anchor = top-right -> recalc x only
    // se: anchor = top-left -> no recalc
    // w: recalc x, n: recalc y, e/s: no recalc
    if (handle === 'nw' || handle === 'sw' || handle === 'w') {
      x = originalRect.x + originalRect.width - width;
    }
    if (handle === 'nw' || handle === 'ne' || handle === 'n') {
      y = originalRect.y + originalRect.height - height;
    }
  }

  return clampCropRect({ x, y, width, height }, imageWidth, imageHeight);
}
