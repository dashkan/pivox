import { FREE_TEMPLATE, MAX_HISTORY, ZOOM_MAX, ZOOM_MIN, ZOOM_STEP } from './constants';
import { applyCropTemplate, clampCropRect, resizeCropRect } from './crop-math';
import {
  canvasToImage,
  computeRotationZoom,
  computeViewportTransform,
  createInitialEditState,
  extractEditState,
  hitTestHandles,
  isEditStateDirty,
} from './transforms';
import { CropOverlayRenderer } from './renderer';
import type {
  CropColors,
  CropRect,
  CropTemplate,
  DragHandle,
  ImageEditorEditState,
  ImageEditorEngineOptions,
  ImageEditorState,
  ResizeMode,
} from './types';

/* ------------------------------------------------------------------ */
/*  Undo history                                                      */
/* ------------------------------------------------------------------ */

interface UndoHistory {
  past: Array<ImageEditorEditState>;
  future: Array<ImageEditorEditState>;
}

function pushHistory(
  history: UndoHistory,
  current: ImageEditorEditState,
  maxHistory: number,
): UndoHistory {
  const past = [...history.past, current];
  if (past.length > maxHistory) past.shift();
  return { past, future: [] };
}

/* ------------------------------------------------------------------ */
/*  Cursor map                                                        */
/* ------------------------------------------------------------------ */

const CURSOR_MAP: Record<DragHandle, string> = {
  nw: 'nwse-resize', se: 'nwse-resize',
  ne: 'nesw-resize', sw: 'nesw-resize',
  n: 'ns-resize', s: 'ns-resize',
  e: 'ew-resize', w: 'ew-resize',
  move: 'move',
};

/* ------------------------------------------------------------------ */
/*  Engine                                                            */
/* ------------------------------------------------------------------ */

const CANVAS_PADDING = 16;

/**
 * Framework-agnostic image editor engine.
 *
 * Manages state, undo/redo history, image loading, canvas rendering,
 * and pointer interactions. Create with options, then mount on a
 * container element.
 *
 * ```ts
 * const engine = new ImageEditorEngine({ src: 'photo.jpg' });
 * engine.mount(containerEl);
 * engine.onChange = (state) => updateUI(state);
 * // later:
 * engine.destroy();
 * ```
 */
export class ImageEditorEngine {
  // ── DOM ──────────────────────────────────────────────────────────

  private container: HTMLElement | null = null;
  private canvas: HTMLCanvasElement | null = null;
  private ctx: CanvasRenderingContext2D | null = null;
  private mounted = false;

  // ── State ────────────────────────────────────────────────────────

  private _state: ImageEditorState;
  private history: UndoHistory = { past: [], future: [] };
  private initialEditState: ImageEditorEditState;
  private preDragEditState: ImageEditorEditState | null = null;
  private preStraightenEditState: ImageEditorEditState | null = null;
  private maxHistory: number;

  // ── Image ────────────────────────────────────────────────────────

  private image: HTMLImageElement | null = null;
  private loadCancelled = false;

  // ── Drag / Pan ───────────────────────────────────────────────────

  private dragOrigin: {
    handle: DragHandle;
    pointerX: number;
    pointerY: number;
    originalRect: CropRect;
    aspectRatio: number | null;
    imageWidth: number;
    imageHeight: number;
  } | null = null;

  private panOrigin: {
    pointerX: number;
    pointerY: number;
    originalOffset: { x: number; y: number };
  } | null = null;

  // ── Viewport ─────────────────────────────────────────────────────

  private scale = 1;
  private offset = { x: 0, y: 0 };

  // ── Rendering ────────────────────────────────────────────────────

  private rafId = 0;
  private dirty = true;
  private renderer = new CropOverlayRenderer();
  private colors: CropColors | null = null;
  private userColors: CropColors | null = null;

  // ── Observers ────────────────────────────────────────────────────

  private resizeObserver: ResizeObserver | null = null;
  private themeObserver: MutationObserver | null = null;

  // ── Callbacks ────────────────────────────────────────────────────

  onChange: ((state: ImageEditorState) => void) | null;
  onEditChange: ((editState: ImageEditorEditState) => void) | null;

  // ── Bound event handlers ─────────────────────────────────────────

  private boundPointerDown = this.onPointerDown.bind(this);
  private boundPointerMove = this.onPointerMove.bind(this);
  private boundPointerUp = this.onPointerUp.bind(this);

  /* ---------------------------------------------------------------- */
  /*  Constructor                                                     */
  /* ---------------------------------------------------------------- */

  constructor(options: ImageEditorEngineOptions = {}) {
    this.maxHistory = options.maxHistory ?? MAX_HISTORY;
    this.onChange = options.onChange ?? null;
    this.onEditChange = options.onEditChange ?? null;
    this.userColors = options.colors ?? null;

    // Templates
    const userTemplates = (options.templates ?? []).filter(
      (t) => t.label !== FREE_TEMPLATE.label,
    );
    const allTemplates = [FREE_TEMPLATE, ...userTemplates];
    const defaultTemplate = options.defaultTemplate ?? FREE_TEMPLATE;

    // Initial state
    this.initialEditState = createInitialEditState(0, 0, options.initialCrop);
    this.initialEditState.activeTemplate = defaultTemplate;

    this._state = {
      ...this.initialEditState,
      src: options.src ?? '',
      imageStatus: options.src ? 'loading' : 'idle',
      imageError: null,
      naturalWidth: 0,
      naturalHeight: 0,
      templates: allTemplates,
      isDragging: false,
      activeHandle: null,
      canUndo: false,
      canRedo: false,
      isDirty: false,
      zoom: 100,
      zoomMode: 'fit',
      panOffset: { x: 0, y: 0 },
      isPanning: false,
      mode: 'view',
    };

    // Load initial image (can happen before mount)
    if (options.src) {
      this.loadImage(options.src);
    }
  }

  /* ---------------------------------------------------------------- */
  /*  Public API — Mount / Unmount                                    */
  /* ---------------------------------------------------------------- */

  /** Mount the engine onto a container element. Creates the canvas. */
  mount(container: HTMLElement): void {
    if (this.mounted) this.unmount();

    this.container = container;
    this.canvas = document.createElement('canvas');
    this.canvas.style.cssText = 'position:absolute;inset:0;width:100%;height:100%';
    container.appendChild(this.canvas);

    const ctx = this.canvas.getContext('2d');
    if (!ctx) throw new Error('Canvas 2D context not available');
    this.ctx = ctx;

    // Read colors from CSS (or use user-provided)
    this.colors = this.userColors ?? this.readColorsFromCSS();

    // Pointer events
    this.canvas.addEventListener('pointerdown', this.boundPointerDown);
    this.canvas.addEventListener('pointermove', this.boundPointerMove);
    this.canvas.addEventListener('pointerup', this.boundPointerUp);
    this.canvas.addEventListener('pointercancel', this.boundPointerUp);

    // Observers
    this.resizeObserver = new ResizeObserver(() => this.markDirty());
    this.resizeObserver.observe(container);

    this.themeObserver = new MutationObserver(() => {
      if (!this.userColors) {
        this.colors = this.readColorsFromCSS();
      }
      this.markDirty();
    });
    this.themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    });

    this.mounted = true;
    this.scheduleRender();
  }

  /** Unmount from the container. Removes canvas and stops rendering. */
  unmount(): void {
    if (!this.mounted) return;

    cancelAnimationFrame(this.rafId);
    this.resizeObserver?.disconnect();
    this.themeObserver?.disconnect();

    if (this.canvas) {
      this.canvas.removeEventListener('pointerdown', this.boundPointerDown);
      this.canvas.removeEventListener('pointermove', this.boundPointerMove);
      this.canvas.removeEventListener('pointerup', this.boundPointerUp);
      this.canvas.removeEventListener('pointercancel', this.boundPointerUp);
      this.canvas.remove();
    }

    this.container = null;
    this.canvas = null;
    this.ctx = null;
    this.resizeObserver = null;
    this.themeObserver = null;
    this.mounted = false;
  }

  /** Unmount and clean up all resources. */
  destroy(): void {
    this.loadCancelled = true;
    this.unmount();
  }

  get isMounted(): boolean {
    return this.mounted;
  }

  /* ---------------------------------------------------------------- */
  /*  Public API — State                                              */
  /* ---------------------------------------------------------------- */

  get state(): Readonly<ImageEditorState> {
    return this._state;
  }

  /* ---------------------------------------------------------------- */
  /*  Public API — Actions                                            */
  /* ---------------------------------------------------------------- */

  loadImage(src: string): void {
    this.loadCancelled = true;
    this.updateState({ src, imageStatus: 'loading', imageError: null });

    this.loadCancelled = false;
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => {
      if (this.loadCancelled) return;
      this.image = img;
      const editState = createInitialEditState(img.naturalWidth, img.naturalHeight);
      editState.activeTemplate = this._state.activeTemplate;
      this.initialEditState = editState;
      this.history = { past: [], future: [] };
      this.preDragEditState = null;
      this.preStraightenEditState = null;
      this.updateState({
        ...editState,
        src,
        imageStatus: 'loaded',
        imageError: null,
        naturalWidth: img.naturalWidth,
        naturalHeight: img.naturalHeight,
        canUndo: false,
        canRedo: false,
        isDirty: false,
        isDragging: false,
        activeHandle: null,
        zoom: 100,
        zoomMode: 'fit',
        panOffset: { x: 0, y: 0 },
        isPanning: false,
        mode: 'view',
      });
    };
    img.onerror = () => {
      if (this.loadCancelled) return;
      this.updateState({
        imageStatus: 'error',
        imageError: `Failed to load image: ${src}`,
      });
    };
    img.src = src;
  }

  setCropRect(rect: CropRect): void {
    const clamped = clampCropRect(rect, this._state.naturalWidth, this._state.naturalHeight);
    this.pushHistoryAndUpdate({ cropRect: clamped });
  }

  setResizeMode(mode: ResizeMode): void {
    this.pushHistoryAndUpdate({ resizeMode: mode });
  }

  rotateClockwise(): void {
    const rotation = ((this._state.rotation + 90) % 360) as 0 | 90 | 180 | 270;
    const { naturalWidth, naturalHeight, cropRect } = this._state;
    const newCrop = clampCropRect(
      { x: cropRect.y, y: naturalWidth - cropRect.x - cropRect.width, width: cropRect.height, height: cropRect.width },
      naturalHeight, naturalWidth,
    );
    this.pushHistoryAndUpdate({ rotation, cropRect: newCrop });
  }

  rotateCounterClockwise(): void {
    const rotation = ((this._state.rotation + 270) % 360) as 0 | 90 | 180 | 270;
    const { naturalWidth, naturalHeight, cropRect } = this._state;
    const newCrop = clampCropRect(
      { x: naturalHeight - cropRect.y - cropRect.height, y: cropRect.x, width: cropRect.height, height: cropRect.width },
      naturalHeight, naturalWidth,
    );
    this.pushHistoryAndUpdate({ rotation, cropRect: newCrop });
  }

  setStraighten(degrees: number): void {
    const clamped = Math.max(-45, Math.min(45, degrees));
    if (!this.preStraightenEditState) {
      this.preStraightenEditState = extractEditState(this._state);
    }
    this.updateState({ straighten: clamped });
  }

  commitStraighten(): void {
    if (!this.preStraightenEditState) return;
    const pre = this.preStraightenEditState;
    this.preStraightenEditState = null;
    this.history = pushHistory(this.history, pre, this.maxHistory);
    const editState = extractEditState(this._state);
    this.updateState({
      canUndo: true,
      canRedo: false,
      isDirty: isEditStateDirty(editState, this.initialEditState),
    });
  }

  toggleFlipHorizontal(): void {
    this.pushHistoryAndUpdate({ flipHorizontal: !this._state.flipHorizontal });
  }

  toggleFlipVertical(): void {
    this.pushHistoryAndUpdate({ flipVertical: !this._state.flipVertical });
  }

  applyTemplate(template: CropTemplate | null): void {
    if (!template) {
      this.pushHistoryAndUpdate({ activeTemplate: null });
      return;
    }
    const newCrop = applyCropTemplate(
      template, this._state.cropRect,
      this._state.naturalWidth, this._state.naturalHeight,
    );
    this.pushHistoryAndUpdate({ activeTemplate: template, cropRect: newCrop });
  }

  reset(): void {
    this.pushHistoryAndUpdate({ ...this.initialEditState });
  }

  undo(): void {
    if (this.history.past.length === 0) return;
    const past = [...this.history.past];
    const previous = past.pop()!;
    const current = extractEditState(this._state);
    this.history = { past, future: [current, ...this.history.future] };
    this.updateState({
      ...previous,
      canUndo: past.length > 0,
      canRedo: true,
      isDirty: isEditStateDirty(previous, this.initialEditState),
    });
  }

  redo(): void {
    if (this.history.future.length === 0) return;
    const future = [...this.history.future];
    const next = future.shift()!;
    const current = extractEditState(this._state);
    this.history = { past: [...this.history.past, current], future };
    this.updateState({
      ...next,
      canUndo: true,
      canRedo: future.length > 0,
      isDirty: isEditStateDirty(next, this.initialEditState),
    });
  }

  zoomIn(): void {
    this.updateState({
      zoom: Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, Math.round(this._state.zoom + ZOOM_STEP))),
      zoomMode: 'manual',
    });
  }

  zoomOut(): void {
    this.updateState({
      zoom: Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, Math.round(this._state.zoom - ZOOM_STEP))),
      zoomMode: 'manual',
    });
  }

  zoomToFit(): void {
    this.updateState({ zoom: 100, zoomMode: 'fit', panOffset: { x: 0, y: 0 } });
  }

  setZoom(level: number): void {
    this.updateState({
      zoom: Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, Math.round(level))),
      zoomMode: 'manual',
    });
  }

  enterCropMode(): void {
    this.updateState({ mode: 'crop' });
  }

  exitCropMode(): void {
    this.updateState({ mode: 'view' });
  }

  setColors(colors: CropColors): void {
    this.userColors = colors;
    this.colors = colors;
    this.markDirty();
  }

  /* ---------------------------------------------------------------- */
  /*  Internal — State management                                     */
  /* ---------------------------------------------------------------- */

  private updateState(partial: Partial<ImageEditorState>): void {
    const prev = this._state;
    this._state = { ...prev, ...partial };
    this.markDirty();
    this.onChange?.(this._state);

    if (this.onEditChange) {
      const prevEdit = extractEditState(prev);
      const nextEdit = extractEditState(this._state);
      if (isEditStateDirty(nextEdit, prevEdit)) {
        this.onEditChange(nextEdit);
      }
    }
  }

  private pushHistoryAndUpdate(partial: Partial<ImageEditorState>): void {
    const currentEdit = extractEditState(this._state);
    this.history = pushHistory(this.history, currentEdit, this.maxHistory);
    const newState = { ...this._state, ...partial };
    const newEdit = extractEditState(newState);
    this.updateState({
      ...partial,
      canUndo: true,
      canRedo: false,
      isDirty: isEditStateDirty(newEdit, this.initialEditState),
    });
  }

  /* ---------------------------------------------------------------- */
  /*  Internal — Pointer events                                       */
  /* ---------------------------------------------------------------- */

  private onPointerDown(e: PointerEvent): void {
    if (this._state.imageStatus !== 'loaded' || this._state.mode !== 'crop') return;

    if (e.button === 1 || (e.button === 0 && e.altKey)) {
      if (this._state.zoomMode === 'manual' && this._state.zoom > 100) {
        e.preventDefault();
        this.canvas?.setPointerCapture(e.pointerId);
        this.panOrigin = {
          pointerX: e.clientX,
          pointerY: e.clientY,
          originalOffset: { ...this._state.panOffset },
        };
        this.updateState({ isPanning: true });
        return;
      }
    }

    const rect = this.canvas?.getBoundingClientRect();
    if (!rect) return;
    const imagePoint = canvasToImage(e.clientX, e.clientY, rect, this.scale, this.offset);
    const hitRadius = 12 / this.scale;
    const handle = hitTestHandles(imagePoint.x, imagePoint.y, this._state.cropRect, hitRadius);

    if (handle) {
      e.preventDefault();
      this.canvas?.setPointerCapture(e.pointerId);
      this.preDragEditState = extractEditState(this._state);
      this.dragOrigin = {
        handle,
        pointerX: imagePoint.x,
        pointerY: imagePoint.y,
        originalRect: { ...this._state.cropRect },
        aspectRatio: this._state.activeTemplate?.ratio ?? null,
        imageWidth: this._state.naturalWidth,
        imageHeight: this._state.naturalHeight,
      };
      this.updateState({ isDragging: true, activeHandle: handle });
    }
  }

  private onPointerMove(e: PointerEvent): void {
    if (this._state.isPanning && this.panOrigin) {
      e.preventDefault();
      this.updateState({
        panOffset: {
          x: this.panOrigin.originalOffset.x + (e.clientX - this.panOrigin.pointerX),
          y: this.panOrigin.originalOffset.y + (e.clientY - this.panOrigin.pointerY),
        },
      });
      return;
    }

    if (this.dragOrigin) {
      e.preventDefault();
      const rect = this.canvas?.getBoundingClientRect();
      if (!rect) return;
      const imagePoint = canvasToImage(e.clientX, e.clientY, rect, this.scale, this.offset);
      const newRect = resizeCropRect(
        this.dragOrigin.originalRect,
        this.dragOrigin.handle,
        imagePoint.x - this.dragOrigin.pointerX,
        imagePoint.y - this.dragOrigin.pointerY,
        this.dragOrigin.imageWidth, this.dragOrigin.imageHeight,
        this.dragOrigin.aspectRatio,
      );
      this._state = { ...this._state, cropRect: newRect };
      this.markDirty();
      this.onChange?.(this._state);
      return;
    }

    if (this._state.imageStatus !== 'loaded' || this._state.mode !== 'crop') return;
    const rect = this.canvas?.getBoundingClientRect();
    if (!rect || !this.canvas) return;
    const imagePoint = canvasToImage(e.clientX, e.clientY, rect, this.scale, this.offset);
    const handle = hitTestHandles(imagePoint.x, imagePoint.y, this._state.cropRect, 12 / this.scale);
    this.canvas.style.cursor = handle ? CURSOR_MAP[handle] : 'default';
  }

  private onPointerUp(e: PointerEvent): void {
    this.canvas?.releasePointerCapture(e.pointerId);

    if (this._state.isPanning) {
      this.panOrigin = null;
      this.updateState({ isPanning: false });
      return;
    }

    if (this.dragOrigin) {
      const preDrag = this.preDragEditState ?? this.initialEditState;
      this.dragOrigin = null;
      this.preDragEditState = null;
      this.history = pushHistory(this.history, preDrag, this.maxHistory);
      const editState = extractEditState(this._state);
      this.updateState({
        isDragging: false,
        activeHandle: null,
        canUndo: true,
        canRedo: false,
        isDirty: isEditStateDirty(editState, this.initialEditState),
      });
    }
  }

  /* ---------------------------------------------------------------- */
  /*  Internal — Rendering                                            */
  /* ---------------------------------------------------------------- */

  private markDirty(): void {
    this.dirty = true;
  }

  private scheduleRender(): void {
    const loop = () => {
      if (this.dirty) {
        this.dirty = false;
        this.render();
      }
      this.rafId = requestAnimationFrame(loop);
    };
    this.rafId = requestAnimationFrame(loop);
  }

  private render(): void {
    const { canvas, ctx, image, container, colors } = this;
    const state = this._state;
    if (!canvas || !ctx || !image || !container || !colors || state.imageStatus !== 'loaded') return;

    const dpr = window.devicePixelRatio || 1;
    const containerWidth = container.clientWidth;
    const containerHeight = container.clientHeight;

    canvas.width = containerWidth * dpr;
    canvas.height = containerHeight * dpr;
    canvas.style.width = `${containerWidth}px`;
    canvas.style.height = `${containerHeight}px`;
    ctx.scale(dpr, dpr);

    ctx.fillStyle = colors.canvas;
    ctx.fillRect(0, 0, containerWidth, containerHeight);

    const { naturalWidth, naturalHeight, cropRect } = state;
    const isCropMode = state.mode === 'crop';
    const effectiveZoom = state.zoomMode === 'fit' ? 100 : state.zoom;
    const pad = isCropMode ? CANVAS_PADDING : 0;
    const drawWidth = containerWidth - pad * 2;
    const drawHeight = containerHeight - pad * 2;

    if (isCropMode) {
      const vt = computeViewportTransform(drawWidth, drawHeight, naturalWidth, naturalHeight, effectiveZoom, state.panOffset);
      const adjOffsetX = vt.offsetX + pad;
      const adjOffsetY = vt.offsetY + pad;
      this.scale = vt.scale;
      this.offset = { x: adjOffsetX, y: adjOffsetY };

      ctx.save();
      ctx.translate(adjOffsetX, adjOffsetY);
      ctx.scale(vt.scale, vt.scale);
      this.drawImage(ctx, true);
      ctx.restore();

      ctx.save();
      ctx.translate(adjOffsetX, adjOffsetY);
      ctx.scale(vt.scale, vt.scale);
      this.renderer.draw(ctx, state, vt.scale, colors);
      ctx.restore();

    } else {
      const cropFitScale = Math.min(drawWidth / cropRect.width, drawHeight / cropRect.height);
      const viewScale = cropFitScale * (effectiveZoom / 100);
      const cropScreenW = cropRect.width * viewScale;
      const cropScreenH = cropRect.height * viewScale;
      const cropOffsetX = (containerWidth - cropScreenW) / 2 + state.panOffset.x;
      const cropOffsetY = (containerHeight - cropScreenH) / 2 + state.panOffset.y;
      this.scale = viewScale;
      this.offset = { x: cropOffsetX, y: cropOffsetY };

      ctx.save();
      ctx.beginPath();
      ctx.rect(cropOffsetX, cropOffsetY, cropScreenW, cropScreenH);
      ctx.clip();
      ctx.translate(cropOffsetX - cropRect.x * viewScale, cropOffsetY - cropRect.y * viewScale);
      ctx.scale(viewScale, viewScale);
      this.drawImage(ctx, true);
      ctx.restore();
    }
  }

  private drawImage(ctx: CanvasRenderingContext2D, applyRotationZoom: boolean): void {
    const { naturalWidth, naturalHeight, straighten, rotation, flipHorizontal, flipVertical, cropRect } = this._state;
    const img = this.image;
    if (!img) return;

    let rotZoom = 1;
    if (applyRotationZoom) {
      rotZoom = computeRotationZoom(naturalWidth, naturalHeight, cropRect, straighten);
    }

    const cx = naturalWidth / 2;
    const cy = naturalHeight / 2;
    ctx.translate(cx, cy);
    ctx.rotate(((rotation + straighten) * Math.PI) / 180);
    ctx.scale(
      (flipHorizontal ? -1 : 1) * rotZoom,
      (flipVertical ? -1 : 1) * rotZoom,
    );
    ctx.translate(-cx, -cy);
    ctx.drawImage(img, 0, 0, naturalWidth, naturalHeight);
  }

  /* ---------------------------------------------------------------- */
  /*  Internal — CSS color reading                                    */
  /* ---------------------------------------------------------------- */

  private readColorsFromCSS(): CropColors {
    const el = this.container ?? document.documentElement;
    const styles = getComputedStyle(el);
    const read = (name: string, fallback: string) => {
      const value = styles.getPropertyValue(name).trim();
      if (!value) return fallback;
      if (value.startsWith('oklch')) return value;
      if (/^\d/.test(value)) return `oklch(${value})`;
      return value;
    };

    return {
      canvas: styles.backgroundColor || read('--image-editor-canvas', '#f0f0f0'),
      border: read('--image-editor-crop-border', '#3b82f6'),
      handle: read('--image-editor-crop-handle', '#3b82f6'),
      grid: read('--image-editor-crop-grid', 'rgba(120,120,120,0.4)'),
      overlay: read('--image-editor-crop-overlay', 'rgba(0,0,0,0.5)'),
    };
  }
}
