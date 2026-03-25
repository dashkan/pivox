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
    // In the new model, crop rect is at (0, 0) with cropWidth x cropHeight
    const rx = 0;
    const ry = 0;
    const rw = state.cropWidth;
    const rh = state.cropHeight;

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
   * Corner handles: white-filled L-shaped brackets with blue border.
   * Drawn as a single continuous path — no overlapping seams.
   */
  private drawCornerHandles(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    scale: number, _fillColor: string, strokeColor: string,
  ): void {
    const len = Math.min(26 / scale, rw / 4, rh / 4);
    const t = 5 / scale; // thickness — just a bit thicker than edge handles
    const r = 2.5 / scale; // corner radius for rounded outer tips

    // Each corner: [cx, cy, horizontal direction, vertical direction]
    const corners = [
      { cx: rx, cy: ry, dx: 1, dy: 1 },
      { cx: rx + rw, cy: ry, dx: -1, dy: 1 },
      { cx: rx, cy: ry + rh, dx: 1, dy: -1 },
      { cx: rx + rw, cy: ry + rh, dx: -1, dy: -1 },
    ];

    for (const { cx, cy, dx, dy } of corners) {
      // Build an L-shaped path as a single polygon.
      // The L has a horizontal arm and a vertical arm meeting at the corner.
      // Outer edges of the arms are rounded, inner corner is square.
      //
      // For top-left (dx=1, dy=1):
      //   Horizontal arm goes right, vertical arm goes down.
      //   The L occupies: [cx, cy] to [cx+len, cy+t] horizontally
      //                   [cx, cy] to [cx+t, cy+len] vertically

      const hEnd = cx + dx * len; // end of horizontal arm
      const vEnd = cy + dy * len; // end of vertical arm

      ctx.beginPath();

      if (dx === 1 && dy === 1) {
        // Top-left: horizontal right, vertical down
        ctx.moveTo(cx, cy + t);           // inner bottom of vertical arm start
        ctx.lineTo(cx, cy + r);           // up to rounded corner
        ctx.quadraticCurveTo(cx, cy, cx + r, cy); // round top-left
        ctx.lineTo(hEnd - r, cy);         // across top of horizontal arm
        ctx.quadraticCurveTo(hEnd, cy, hEnd, cy + r); // round right end
        ctx.lineTo(hEnd, cy + t - r);
        ctx.quadraticCurveTo(hEnd, cy + t, hEnd - r, cy + t); // round right-bottom
        ctx.lineTo(cx + t, cy + t);       // inner corner (square)
        ctx.lineTo(cx + t, vEnd - r);
        ctx.quadraticCurveTo(cx + t, vEnd, cx + t - r, vEnd); // round bottom end
        ctx.lineTo(cx + r, vEnd);
        ctx.quadraticCurveTo(cx, vEnd, cx, vEnd - r); // round bottom-left
        ctx.closePath();
      } else if (dx === -1 && dy === 1) {
        // Top-right: horizontal left, vertical down
        ctx.moveTo(cx, cy + t);
        ctx.lineTo(cx, cy + r);
        ctx.quadraticCurveTo(cx, cy, cx - r, cy);
        ctx.lineTo(hEnd + r, cy);
        ctx.quadraticCurveTo(hEnd, cy, hEnd, cy + r);
        ctx.lineTo(hEnd, cy + t - r);
        ctx.quadraticCurveTo(hEnd, cy + t, hEnd + r, cy + t);
        ctx.lineTo(cx - t, cy + t);
        ctx.lineTo(cx - t, vEnd - r);
        ctx.quadraticCurveTo(cx - t, vEnd, cx - t + r, vEnd);
        ctx.lineTo(cx - r, vEnd);
        ctx.quadraticCurveTo(cx, vEnd, cx, vEnd - r);
        ctx.closePath();
      } else if (dx === 1 && dy === -1) {
        // Bottom-left: horizontal right, vertical up
        ctx.moveTo(cx, cy - t);
        ctx.lineTo(cx, cy - r);
        ctx.quadraticCurveTo(cx, cy, cx + r, cy);
        ctx.lineTo(hEnd - r, cy);
        ctx.quadraticCurveTo(hEnd, cy, hEnd, cy - r);
        ctx.lineTo(hEnd, cy - t + r);
        ctx.quadraticCurveTo(hEnd, cy - t, hEnd - r, cy - t);
        ctx.lineTo(cx + t, cy - t);
        ctx.lineTo(cx + t, vEnd + r);
        ctx.quadraticCurveTo(cx + t, vEnd, cx + t - r, vEnd);
        ctx.lineTo(cx + r, vEnd);
        ctx.quadraticCurveTo(cx, vEnd, cx, vEnd + r);
        ctx.closePath();
      } else {
        // Bottom-right (dx=-1, dy=-1): horizontal left, vertical up
        ctx.moveTo(cx, cy - t);
        ctx.lineTo(cx, cy - r);
        ctx.quadraticCurveTo(cx, cy, cx - r, cy);
        ctx.lineTo(hEnd + r, cy);
        ctx.quadraticCurveTo(hEnd, cy, hEnd, cy - r);
        ctx.lineTo(hEnd, cy - t + r);
        ctx.quadraticCurveTo(hEnd, cy - t, hEnd + r, cy - t);
        ctx.lineTo(cx - t, cy - t);
        ctx.lineTo(cx - t, vEnd + r);
        ctx.quadraticCurveTo(cx - t, vEnd, cx - t + r, vEnd);
        ctx.lineTo(cx - r, vEnd);
        ctx.quadraticCurveTo(cx, vEnd, cx, vEnd + r);
        ctx.closePath();
      }

      ctx.fillStyle = 'white';
      ctx.fill();
      ctx.strokeStyle = strokeColor;
      ctx.lineWidth = 1 / scale;
      ctx.stroke();
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
