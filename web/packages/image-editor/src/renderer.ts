import type { CropColors, ImageEditorState } from './types';

/**
 * Draws the crop overlay, grid, and handles onto a canvas context.
 *
 * The `draw` method expects the context translated/scaled to image-space
 * for the crop rect drawing. The `drawOverlay` method takes raw screen-space
 * coordinates since the overlay must NOT rotate with the image.
 */
export class CropOverlayRenderer {
  /**
   * Draw the full-canvas dim overlay with a cutout for the crop rect.
   * Called in SCREEN SPACE (no rotation transform applied).
   */
  drawScreenOverlay(
    ctx: CanvasRenderingContext2D,
    containerWidth: number,
    containerHeight: number,
    cropScreenX: number,
    cropScreenY: number,
    cropScreenW: number,
    cropScreenH: number,
    overlayColor: string,
  ): void {
    ctx.fillStyle = overlayColor;
    // Top
    ctx.fillRect(0, 0, containerWidth, cropScreenY);
    // Bottom
    ctx.fillRect(0, cropScreenY + cropScreenH, containerWidth, containerHeight - cropScreenY - cropScreenH);
    // Left
    ctx.fillRect(0, cropScreenY, cropScreenX, cropScreenH);
    // Right
    ctx.fillRect(cropScreenX + cropScreenW, cropScreenY, containerWidth - cropScreenX - cropScreenW, cropScreenH);
  }

  /**
   * Draw the crop border, grid, and handles.
   * Called in IMAGE SPACE (after translate + scale to image coords).
   */
  drawControls(
    ctx: CanvasRenderingContext2D,
    state: ImageEditorState,
    scale: number,
    colors: CropColors,
  ): void {
    const { cropRect } = state;
    const { x: rx, y: ry, width: rw, height: rh } = cropRect;

    this.drawBorder(ctx, rx, ry, rw, rh, scale, colors.border);
    this.drawGrid(ctx, rx, ry, rw, rh, scale, colors.grid);
    this.drawCornerHandles(ctx, rx, ry, rw, rh, scale, colors.handle, colors.border);
    this.drawEdgeHandles(ctx, rx, ry, rw, rh, scale, colors.handle, colors.border);
  }

  private drawBorder(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    scale: number, color: string,
  ): void {
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.5 / scale;
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

  /**
   * Corner handles: thick white-filled rounded rectangles with blue border.
   * L-shaped brackets with 3 outer edges rounded, inner corner square.
   */
  private drawCornerHandles(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    scale: number, fillColor: string, strokeColor: string,
  ): void {
    const len = Math.min(30 / scale, rw / 4, rh / 4);
    const thickness = 6 / scale;
    const radius = 3 / scale;

    const corners = [
      // [x, y, dirX, dirY] — corner position and which direction arms extend
      { cx: rx, cy: ry, dx: 1, dy: 1 },          // top-left
      { cx: rx + rw, cy: ry, dx: -1, dy: 1 },     // top-right
      { cx: rx, cy: ry + rh, dx: 1, dy: -1 },     // bottom-left
      { cx: rx + rw, cy: ry + rh, dx: -1, dy: -1 }, // bottom-right
    ];

    for (const { cx, cy, dx, dy } of corners) {
      ctx.save();

      // Horizontal arm: from corner outward
      const hBarX = dx > 0 ? cx : cx - len;
      const hBarY = cy - thickness / 2;

      ctx.beginPath();
      this.drawRoundedRect(ctx, hBarX, hBarY, len, thickness, radius);
      ctx.fillStyle = 'white';
      ctx.fill();
      ctx.strokeStyle = strokeColor;
      ctx.lineWidth = 1.5 / scale;
      ctx.stroke();

      // Vertical arm: from corner outward
      const vBarX = cx - thickness / 2;
      const vBarY = dy > 0 ? cy : cy - len;

      ctx.beginPath();
      this.drawRoundedRect(ctx, vBarX, vBarY, thickness, len, radius);
      ctx.fillStyle = 'white';
      ctx.fill();
      ctx.strokeStyle = strokeColor;
      ctx.lineWidth = 1.5 / scale;
      ctx.stroke();

      ctx.restore();
    }
  }

  /**
   * Edge handles: shorter white-filled rounded pills with blue border.
   */
  private drawEdgeHandles(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    scale: number, _fillColor: string, strokeColor: string,
  ): void {
    const barLen = Math.min(22 / scale, rw / 5, rh / 5);
    const thickness = 4 / scale;
    const radius = thickness / 2; // fully rounded ends (pill shape)

    const edges = [
      // Horizontal edges (top, bottom)
      { x: rx + rw / 2 - barLen / 2, y: ry - thickness / 2, w: barLen, h: thickness },
      { x: rx + rw / 2 - barLen / 2, y: ry + rh - thickness / 2, w: barLen, h: thickness },
      // Vertical edges (left, right)
      { x: rx - thickness / 2, y: ry + rh / 2 - barLen / 2, w: thickness, h: barLen },
      { x: rx + rw - thickness / 2, y: ry + rh / 2 - barLen / 2, w: thickness, h: barLen },
    ];

    for (const { x, y, w, h } of edges) {
      ctx.beginPath();
      this.drawRoundedRect(ctx, x, y, w, h, radius);
      ctx.fillStyle = 'white';
      ctx.fill();
      ctx.strokeStyle = strokeColor;
      ctx.lineWidth = 1 / scale;
      ctx.stroke();
    }
  }

  private drawRoundedRect(
    ctx: CanvasRenderingContext2D,
    x: number, y: number, w: number, h: number,
    r: number,
  ): void {
    r = Math.min(r, w / 2, h / 2);
    ctx.moveTo(x + r, y);
    ctx.lineTo(x + w - r, y);
    ctx.quadraticCurveTo(x + w, y, x + w, y + r);
    ctx.lineTo(x + w, y + h - r);
    ctx.quadraticCurveTo(x + w, y + h, x + w - r, y + h);
    ctx.lineTo(x + r, y + h);
    ctx.quadraticCurveTo(x, y + h, x, y + h - r);
    ctx.lineTo(x, y + r);
    ctx.quadraticCurveTo(x, y, x + r, y);
    ctx.closePath();
  }
}
