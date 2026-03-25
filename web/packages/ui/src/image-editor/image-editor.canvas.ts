'use client';

import { useEffect, useLayoutEffect, useRef } from 'react';
import { computeRotationZoom, computeViewportTransform } from './image-editor.transforms';
import type { ImageEditorState } from './image-editor.types';

/** Canvas padding in CSS pixels so handles aren't clipped at edges. */
const CANVAS_PADDING = 16;

/* ------------------------------------------------------------------ */
/*  Helpers                                                           */
/* ------------------------------------------------------------------ */

function resolveCssColor(value: string, fallback: string): string {
  if (!value) return fallback;
  if (value.startsWith('oklch')) return value;
  if (/^\d/.test(value)) return `oklch(${value})`;
  return value;
}

/**
 * Read crop colors from CSS custom properties defined in colors.css.
 * Uses the raw variables (--image-editor-*) from :root, not the
 * Tailwind --color-* theme tokens which are compiled at build time.
 */
function readCropColors(rootEl: Element) {
  const styles = getComputedStyle(rootEl);
  const read = (name: string) => {
    const value = styles.getPropertyValue(name).trim();
    return value ? resolveCssColor(value, '') : '';
  };
  return {
    border: read('--image-editor-crop-border'),
    handle: read('--image-editor-crop-handle'),
    grid: read('--image-editor-crop-grid'),
    overlay: read('--image-editor-crop-overlay'),
  };
}

/* ------------------------------------------------------------------ */
/*  Crop overlay drawing                                              */
/* ------------------------------------------------------------------ */

function drawCropOverlay(
  ctx: CanvasRenderingContext2D,
  state: ImageEditorState,
  scale: number,
  colors: { border: string; handle: string; grid: string; overlay: string },
) {
  const { cropRect, naturalWidth, naturalHeight } = state;
  const { x: rx, y: ry, width: rw, height: rh } = cropRect;

  // Dim overlay outside crop
  ctx.fillStyle = colors.overlay;
  ctx.fillRect(0, 0, naturalWidth, ry);
  ctx.fillRect(0, ry + rh, naturalWidth, naturalHeight - ry - rh);
  ctx.fillRect(0, ry, rx, rh);
  ctx.fillRect(rx + rw, ry, naturalWidth - rx - rw, rh);

  // Crop border — blue
  ctx.strokeStyle = colors.border;
  ctx.lineWidth = 2 / scale;
  ctx.strokeRect(rx, ry, rw, rh);

  // 3×3 grid inside crop rect
  ctx.strokeStyle = colors.grid;
  ctx.lineWidth = 1 / scale;
  for (let i = 1; i <= 2; i++) {
    const gx = rx + (rw * i) / 3;
    const gy = ry + (rh * i) / 3;
    ctx.beginPath();
    ctx.moveTo(gx, ry);
    ctx.lineTo(gx, ry + rh);
    ctx.stroke();
    ctx.beginPath();
    ctx.moveTo(rx, gy);
    ctx.lineTo(rx + rw, gy);
    ctx.stroke();
  }

  // Corner L-bracket handles — thick, blue
  const bracketLen = Math.min(28 / scale, rw / 4, rh / 4);
  ctx.strokeStyle = colors.handle;
  ctx.lineWidth = 5 / scale;
  ctx.lineCap = 'round';

  ctx.beginPath();
  ctx.moveTo(rx, ry + bracketLen);
  ctx.lineTo(rx, ry);
  ctx.lineTo(rx + bracketLen, ry);
  ctx.stroke();

  ctx.beginPath();
  ctx.moveTo(rx + rw - bracketLen, ry);
  ctx.lineTo(rx + rw, ry);
  ctx.lineTo(rx + rw, ry + bracketLen);
  ctx.stroke();

  ctx.beginPath();
  ctx.moveTo(rx, ry + rh - bracketLen);
  ctx.lineTo(rx, ry + rh);
  ctx.lineTo(rx + bracketLen, ry + rh);
  ctx.stroke();

  ctx.beginPath();
  ctx.moveTo(rx + rw - bracketLen, ry + rh);
  ctx.lineTo(rx + rw, ry + rh);
  ctx.lineTo(rx + rw, ry + rh - bracketLen);
  ctx.stroke();

  // Edge midpoint bars
  const edgeBarLen = Math.min(18 / scale, rw / 5, rh / 5);
  ctx.lineWidth = 4 / scale;

  ctx.beginPath();
  ctx.moveTo(rx + rw / 2 - edgeBarLen, ry);
  ctx.lineTo(rx + rw / 2 + edgeBarLen, ry);
  ctx.stroke();

  ctx.beginPath();
  ctx.moveTo(rx + rw / 2 - edgeBarLen, ry + rh);
  ctx.lineTo(rx + rw / 2 + edgeBarLen, ry + rh);
  ctx.stroke();

  ctx.beginPath();
  ctx.moveTo(rx, ry + rh / 2 - edgeBarLen);
  ctx.lineTo(rx, ry + rh / 2 + edgeBarLen);
  ctx.stroke();

  ctx.beginPath();
  ctx.moveTo(rx + rw, ry + rh / 2 - edgeBarLen);
  ctx.lineTo(rx + rw, ry + rh / 2 + edgeBarLen);
  ctx.stroke();
}

/* ------------------------------------------------------------------ */
/*  Image drawing with rotation/flip + rotation zoom                  */
/* ------------------------------------------------------------------ */

function drawImage(
  ctx: CanvasRenderingContext2D,
  img: HTMLImageElement,
  state: ImageEditorState,
  applyRotationZoom: boolean = false,
) {
  const { naturalWidth, naturalHeight, straighten } = state;
  const totalAngle = state.rotation + straighten;

  let rotZoom = 1;
  if (applyRotationZoom) {
    rotZoom = computeRotationZoom(
      naturalWidth, naturalHeight,
      state.cropRect,
      straighten,
    );
  }

  const cx = naturalWidth / 2;
  const cy = naturalHeight / 2;
  ctx.translate(cx, cy);
  ctx.rotate((totalAngle * Math.PI) / 180);
  ctx.scale(
    (state.flipHorizontal ? -1 : 1) * rotZoom,
    (state.flipVertical ? -1 : 1) * rotZoom,
  );
  ctx.translate(-cx, -cy);
  ctx.drawImage(img, 0, 0, naturalWidth, naturalHeight);
}

/* ------------------------------------------------------------------ */
/*  Hook                                                              */
/* ------------------------------------------------------------------ */

export function useCanvasRenderer(
  state: ImageEditorState,
  canvasRef: React.RefObject<HTMLCanvasElement | null>,
  rootRef: React.RefObject<HTMLDivElement | null>,
  imageRef: React.RefObject<HTMLImageElement | null>,
  scaleRef: React.RefObject<number>,
  offsetRef: React.RefObject<{ x: number; y: number }>,
  fitScaleRef: React.RefObject<number>,
) {
  const renderFrameRef = useRef<() => void>(() => {});

  function renderFrame() {
    const canvas = canvasRef.current;
    const root = rootRef.current;
    const img = imageRef.current;
    if (!canvas || !root || !img || state.imageStatus !== 'loaded') return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    const containerWidth = root.clientWidth;
    const containerHeight = root.clientHeight;

    canvas.width = containerWidth * dpr;
    canvas.height = containerHeight * dpr;
    canvas.style.width = `${containerWidth}px`;
    canvas.style.height = `${containerHeight}px`;
    ctx.scale(dpr, dpr);

    // Read canvas background from CSS custom property defined in colors.css.
    // Uses the raw variable from :root (not Tailwind --color-* theme tokens).
    const rootStyles = getComputedStyle(root);
    const canvasBg = rootStyles.getPropertyValue('--image-editor-canvas').trim();
    ctx.fillStyle = canvasBg ? resolveCssColor(canvasBg, '') : rootStyles.backgroundColor;
    ctx.fillRect(0, 0, containerWidth, containerHeight);

    const { naturalWidth, naturalHeight, zoom, zoomMode, panOffset, cropRect } = state;
    const isCropMode = state.mode === 'crop';
    const effectiveZoom = zoomMode === 'fit' ? 100 : zoom;

    // Padding so handles at edges aren't clipped
    const pad = isCropMode ? CANVAS_PADDING : 0;
    const drawWidth = containerWidth - pad * 2;
    const drawHeight = containerHeight - pad * 2;

    if (isCropMode) {
      // CROP MODE: full image with padding + overlay + handles
      const { scale, offsetX, offsetY, fitScale } = computeViewportTransform(
        drawWidth, drawHeight,
        naturalWidth, naturalHeight,
        effectiveZoom, panOffset,
      );

      const adjOffsetX = offsetX + pad;
      const adjOffsetY = offsetY + pad;

      (scaleRef as React.MutableRefObject<number>).current = scale;
      (offsetRef as React.MutableRefObject<{ x: number; y: number }>).current = { x: adjOffsetX, y: adjOffsetY };
      (fitScaleRef as React.MutableRefObject<number>).current = fitScale;

      // Draw image — no rotation zoom in crop mode (overlay must align with image)
      ctx.save();
      ctx.translate(adjOffsetX, adjOffsetY);
      ctx.scale(scale, scale);
      drawImage(ctx, img, state, true);
      ctx.restore();

      // Draw crop overlay + handles
      const cropColors = readCropColors(root);
      ctx.save();
      ctx.translate(adjOffsetX, adjOffsetY);
      ctx.scale(scale, scale);
      drawCropOverlay(ctx, state, scale, cropColors);
      ctx.restore();

    } else {
      // VIEW MODE: render the crop result.
      // Use the same full-image transform as crop mode so the rotation zoom
      // is identical, then reposition the viewport so the crop rect is centered
      // and clip everything outside it.

      // Compute scale so the crop rect fills the container
      const cropFitScaleX = drawWidth / cropRect.width;
      const cropFitScaleY = drawHeight / cropRect.height;
      const cropFitScale = Math.min(cropFitScaleX, cropFitScaleY);
      const viewScale = cropFitScale * (effectiveZoom / 100);

      // Center the crop rect in the container
      const cropScreenW = cropRect.width * viewScale;
      const cropScreenH = cropRect.height * viewScale;
      const cropOffsetX = (containerWidth - cropScreenW) / 2 + panOffset.x;
      const cropOffsetY = (containerHeight - cropScreenH) / 2 + panOffset.y;

      (scaleRef as React.MutableRefObject<number>).current = viewScale;
      (offsetRef as React.MutableRefObject<{ x: number; y: number }>).current = { x: cropOffsetX, y: cropOffsetY };
      (fitScaleRef as React.MutableRefObject<number>).current = cropFitScale;

      ctx.save();
      // Clip to the crop rect on screen
      ctx.beginPath();
      ctx.rect(cropOffsetX, cropOffsetY, cropScreenW, cropScreenH);
      ctx.clip();

      // Transform: position the full image so the crop rect lands
      // inside the clip area, then draw with rotation+zoom
      ctx.translate(cropOffsetX - cropRect.x * viewScale, cropOffsetY - cropRect.y * viewScale);
      ctx.scale(viewScale, viewScale);
      drawImage(ctx, img, state, true);
      ctx.restore();
    }
  }

  renderFrameRef.current = renderFrame;

  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const resizeObserver = new ResizeObserver(() => { renderFrameRef.current(); });
    resizeObserver.observe(root);

    // Re-render when theme changes (dark class on <html>)
    const themeObserver = new MutationObserver(() => { renderFrameRef.current(); });
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    });

    return () => {
      resizeObserver.disconnect();
      themeObserver.disconnect();
    };
  }, [state.imageStatus]);

  useEffect(() => {
    renderFrame();
  }, [state]);
}
