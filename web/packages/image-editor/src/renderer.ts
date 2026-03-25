import type { CropColors, ImageEditorState } from './types';

/**
 * Draws the crop overlay, grid, and handles onto a canvas context.
 * Expects the context to already be translated/scaled to image-space.
 */
export class CropOverlayRenderer {
  draw(
    ctx: CanvasRenderingContext2D,
    state: ImageEditorState,
    scale: number,
    colors: CropColors,
  ): void {
    const { cropRect, naturalWidth, naturalHeight } = state;
    const { x: rx, y: ry, width: rw, height: rh } = cropRect;

    this.drawOverlay(ctx, rx, ry, rw, rh, naturalWidth, naturalHeight, colors.overlay);
    this.drawBorder(ctx, rx, ry, rw, rh, scale, colors.border);
    this.drawGrid(ctx, rx, ry, rw, rh, scale, colors.grid);
    this.drawCornerHandles(ctx, rx, ry, rw, rh, scale, colors.handle);
    this.drawEdgeHandles(ctx, rx, ry, rw, rh, scale, colors.handle);
  }

  private drawOverlay(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    naturalWidth: number, naturalHeight: number,
    color: string,
  ): void {
    ctx.fillStyle = color;
    ctx.fillRect(0, 0, naturalWidth, ry);
    ctx.fillRect(0, ry + rh, naturalWidth, naturalHeight - ry - rh);
    ctx.fillRect(0, ry, rx, rh);
    ctx.fillRect(rx + rw, ry, naturalWidth - rx - rw, rh);
  }

  private drawBorder(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    scale: number, color: string,
  ): void {
    ctx.strokeStyle = color;
    ctx.lineWidth = 2 / scale;
    ctx.strokeRect(rx, ry, rw, rh);
  }

  private drawGrid(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    scale: number, color: string,
  ): void {
    ctx.strokeStyle = color;
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
  }

  private drawCornerHandles(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    scale: number, color: string,
  ): void {
    const bracketLen = Math.min(28 / scale, rw / 4, rh / 4);
    ctx.strokeStyle = color;
    ctx.lineWidth = 5 / scale;
    ctx.lineCap = 'round';

    // Top-left
    ctx.beginPath();
    ctx.moveTo(rx, ry + bracketLen);
    ctx.lineTo(rx, ry);
    ctx.lineTo(rx + bracketLen, ry);
    ctx.stroke();

    // Top-right
    ctx.beginPath();
    ctx.moveTo(rx + rw - bracketLen, ry);
    ctx.lineTo(rx + rw, ry);
    ctx.lineTo(rx + rw, ry + bracketLen);
    ctx.stroke();

    // Bottom-left
    ctx.beginPath();
    ctx.moveTo(rx, ry + rh - bracketLen);
    ctx.lineTo(rx, ry + rh);
    ctx.lineTo(rx + bracketLen, ry + rh);
    ctx.stroke();

    // Bottom-right
    ctx.beginPath();
    ctx.moveTo(rx + rw - bracketLen, ry + rh);
    ctx.lineTo(rx + rw, ry + rh);
    ctx.lineTo(rx + rw, ry + rh - bracketLen);
    ctx.stroke();
  }

  private drawEdgeHandles(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    scale: number, color: string,
  ): void {
    const edgeBarLen = Math.min(18 / scale, rw / 5, rh / 5);
    ctx.strokeStyle = color;
    ctx.lineWidth = 4 / scale;
    ctx.lineCap = 'round';

    // Top
    ctx.beginPath();
    ctx.moveTo(rx + rw / 2 - edgeBarLen, ry);
    ctx.lineTo(rx + rw / 2 + edgeBarLen, ry);
    ctx.stroke();

    // Bottom
    ctx.beginPath();
    ctx.moveTo(rx + rw / 2 - edgeBarLen, ry + rh);
    ctx.lineTo(rx + rw / 2 + edgeBarLen, ry + rh);
    ctx.stroke();

    // Left
    ctx.beginPath();
    ctx.moveTo(rx, ry + rh / 2 - edgeBarLen);
    ctx.lineTo(rx, ry + rh / 2 + edgeBarLen);
    ctx.stroke();

    // Right
    ctx.beginPath();
    ctx.moveTo(rx + rw, ry + rh / 2 - edgeBarLen);
    ctx.lineTo(rx + rw, ry + rh / 2 + edgeBarLen);
    ctx.stroke();
  }
}
