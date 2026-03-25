import { MIN_CROP_SIZE } from './constants';
import type { CropRect, DragHandle } from './types';

/**
 * Calculate the minimum scale needed so a rotated image completely
 * fills the crop rect (no dead pixels). This is the core formula
 * from the "fill-to-frame" algorithm.
 *
 * @param cropW - Crop rect width in image pixels
 * @param cropH - Crop rect height in image pixels
 * @param imgW - Original image width
 * @param imgH - Original image height
 * @param angleRad - Total rotation angle in RADIANS
 */
export function computeMinScale(
  cropW: number,
  cropH: number,
  imgW: number,
  imgH: number,
  angleRad: number,
): number {
  const absCos = Math.abs(Math.cos(angleRad));
  const absSin = Math.abs(Math.sin(angleRad));

  // The crop rect projected onto the image's rotated axes
  const requiredW = cropW * absCos + cropH * absSin;
  const requiredH = cropW * absSin + cropH * absCos;

  return Math.max(requiredW / imgW, requiredH / imgH);
}

/**
 * Compute the maximum translation boundaries ("leash") for a given
 * rotation and scale. Prevents the user from panning the image so
 * far that dead pixels appear.
 *
 * Uses the "inverse projection" method: projects crop rect corners
 * into the image's unrotated coordinate space and ensures they stay
 * within the image bounds. This prevents the "corner poke" issue
 * where crop corners exit the rotated image edge.
 *
 * @returns { maxTx, maxTy } — translation is clamped to [-max, +max]
 */
export function computeTranslationBounds(
  cropW: number,
  cropH: number,
  imgW: number,
  imgH: number,
  scale: number,
  angleRad: number,
): { maxTx: number; maxTy: number } {
  const absCos = Math.abs(Math.cos(angleRad));
  const absSin = Math.abs(Math.sin(angleRad));

  // Half-dimensions of the scaled image (the boundary)
  const hIW = (imgW * scale) / 2;
  const hIH = (imgH * scale) / 2;

  // Half-dimensions of the crop rectangle (the window)
  const hCW = cropW / 2;
  const hCH = cropH / 2;

  // Solve for maxTx (assuming ty = 0, worst-case corner)
  // Constraint: |tx*cos| + |hCW*cos + hCH*sin| <= hIW
  //             |tx*sin| + |hCW*sin + hCH*cos| <= hIH
  const txLimit1 = absCos > 1e-10 ? (hIW - (hCW * absCos + hCH * absSin)) / absCos : Infinity;
  const txLimit2 = absSin > 1e-10 ? (hIH - (hCW * absSin + hCH * absCos)) / absSin : Infinity;
  const maxTx = Math.max(0, Math.min(txLimit1, txLimit2));

  // Solve for maxTy (assuming tx = 0, worst-case corner)
  const tyLimit1 = absSin > 1e-10 ? (hIW - (hCW * absCos + hCH * absSin)) / absSin : Infinity;
  const tyLimit2 = absCos > 1e-10 ? (hIH - (hCW * absSin + hCH * absCos)) / absCos : Infinity;
  const maxTy = Math.max(0, Math.min(tyLimit1, tyLimit2));

  return { maxTx, maxTy };
}

/**
 * Clamp translation values to the allowed boundaries.
 */
export function clampTranslation(
  tx: number,
  ty: number,
  maxTx: number,
  maxTy: number,
): { tx: number; ty: number } {
  return {
    tx: Math.max(-maxTx, Math.min(tx, maxTx)),
    ty: Math.max(-maxTy, Math.min(ty, maxTy)),
  };
}

/**
 * Convert the crop-as-viewport state to a CropRect in image pixel space.
 * This is used for proto output — the server needs x, y, width, height
 * in the original image coordinate system.
 */
export function stateToImageCropRect(
  cropW: number,
  cropH: number,
  imgW: number,
  imgH: number,
  tx: number,
  ty: number,
  scale: number,
  angleRad: number,
): CropRect {
  // The crop rect center in the scaled coordinate system is at (0, 0).
  // The image center is offset by (-tx, -ty) from the crop center.
  // In unscaled image space, the offset is (-tx/scale, -ty/scale).
  // We also need to account for rotation by rotating the offset.

  const cosA = Math.cos(-angleRad);
  const sinA = Math.sin(-angleRad);

  // Inverse-rotate the translation to get image-space offset
  const imgOffsetX = (-tx / scale) * cosA - (-ty / scale) * sinA;
  const imgOffsetY = (-tx / scale) * sinA + (-ty / scale) * cosA;

  // The crop rect center in image space
  const cropCenterX = imgW / 2 + imgOffsetX;
  const cropCenterY = imgH / 2 + imgOffsetY;

  // The crop rect dimensions in image space (unscaled)
  const unscaledCropW = cropW / scale;
  const unscaledCropH = cropH / scale;

  return {
    x: Math.round(cropCenterX - unscaledCropW / 2),
    y: Math.round(cropCenterY - unscaledCropH / 2),
    width: Math.round(unscaledCropW),
    height: Math.round(unscaledCropH),
  };
}

/**
 * Apply an aspect ratio template. Returns new cropW/cropH that fit
 * within the current image at the given rotation and scale.
 */
export function applyCropTemplate(
  ratio: number | null,
  currentCropW: number,
  currentCropH: number,
  imgW: number,
  imgH: number,
): { cropW: number; cropH: number } {
  if (ratio === null) {
    return { cropW: currentCropW, cropH: currentCropH };
  }

  // Use the smaller dimension to determine the crop size
  if (imgW / imgH > ratio) {
    const h = imgH;
    const w = h * ratio;
    return { cropW: w, cropH: h };
  }
  const w = imgW;
  const h = w / ratio;
  return { cropW: w, cropH: h };
}

/**
 * Resize crop dimensions from a handle drag.
 * Returns new crop width/height, enforcing minimum size.
 */
export function resizeCropFromHandle(
  handle: DragHandle,
  deltaX: number,
  deltaY: number,
  currentCropW: number,
  currentCropH: number,
  aspectRatio: number | null,
): { cropW: number; cropH: number } {
  let w = currentCropW;
  let h = currentCropH;

  switch (handle) {
    case 'nw':
      w -= deltaX * 2;
      h -= deltaY * 2;
      break;
    case 'n':
      h -= deltaY * 2;
      break;
    case 'ne':
      w += deltaX * 2;
      h -= deltaY * 2;
      break;
    case 'w':
      w -= deltaX * 2;
      break;
    case 'e':
      w += deltaX * 2;
      break;
    case 'sw':
      w -= deltaX * 2;
      h += deltaY * 2;
      break;
    case 's':
      h += deltaY * 2;
      break;
    case 'se':
      w += deltaX * 2;
      h += deltaY * 2;
      break;
    case 'move':
      // Move doesn't resize
      return { cropW: currentCropW, cropH: currentCropH };
  }

  // Enforce minimum
  w = Math.max(w, MIN_CROP_SIZE);
  h = Math.max(h, MIN_CROP_SIZE);

  // Enforce aspect ratio
  if (aspectRatio !== null) {
    const isHorizontal = ['w', 'e'].includes(handle);
    if (isHorizontal) {
      h = w / aspectRatio;
    } else {
      w = h * aspectRatio;
    }
  }

  return { cropW: w, cropH: h };
}

/**
 * Check if a proposed crop size is valid (the image at current scale
 * and rotation can fill it without dead pixels).
 */
export function isCropSizeValid(
  newCropW: number,
  newCropH: number,
  imgW: number,
  imgH: number,
  scale: number,
  angleRad: number,
): boolean {
  const absCos = Math.abs(Math.cos(angleRad));
  const absSin = Math.abs(Math.sin(angleRad));
  const maxW = imgW * scale * absCos + imgH * scale * absSin;
  const maxH = imgW * scale * absSin + imgH * scale * absCos;
  return newCropW <= maxW && newCropH <= maxH;
}
