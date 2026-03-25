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
   * Drawn as a single 6-point polygon path — no overlapping seams.
   * Straddles the crop border (centered on corner point).
   */
  private drawCornerHandles(
    ctx: CanvasRenderingContext2D,
    rx: number, ry: number, rw: number, rh: number,
    scale: number, _fillColor: string, strokeColor: string,
  ): void {
    const len = Math.min(18 / scale, rw / 5, rh / 5);
    const t = 5 / scale;
    const half = t / 2;

    // Each corner: the L straddles the corner point by half-thickness
    const corners = [
      { x: rx, y: ry, dx: 1, dy: 1 },
      { x: rx + rw, y: ry, dx: -1, dy: 1 },
      { x: rx, y: ry + rh, dx: 1, dy: -1 },
      { x: rx + rw, y: ry + rh, dx: -1, dy: -1 },
    ];

    for (const { x, y, dx, dy } of corners) {
      // 6-point L polygon. The L has outer edge at -half from corner,
      // inner edge at +half, arms extending len from corner.
      //
      // For dx=1,dy=1 (top-left), the points are:
      //   P0: outer-left of vertical arm bottom
      //   P1: outer-top-left corner
      //   P2: outer-right end of horizontal arm top
      //   P3: outer-right end of horizontal arm bottom
      //   P4: inner corner where arms meet
      //   P5: outer-bottom of vertical arm
      const outerX = x - dx * half;
      const outerY = y - dy * half;
      const innerX = x + dx * half;
      const innerY = y + dy * half;
      const hTip = x + dx * len;
      const vTip = y + dy * len;

      ctx.beginPath();
      ctx.moveTo(outerX, vTip);        // P0: vertical arm outer end
      ctx.lineTo(outerX, outerY);      // P1: outer corner
      ctx.lineTo(hTip, outerY);        // P2: horizontal arm outer end
      ctx.lineTo(hTip, innerY);        // P3: horizontal arm inner end
      ctx.lineTo(innerX, innerY);      // P4: inner corner
      ctx.lineTo(innerX, vTip);        // P5: vertical arm inner end
      ctx.closePath();

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
