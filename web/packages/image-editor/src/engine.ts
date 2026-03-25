import { FREE_TEMPLATE, MAX_HISTORY, ZOOM_MAX, ZOOM_MIN, ZOOM_STEP } from './constants';
import {
  applyCropTemplate,
  clampTranslation,
  computeMinScale,
  computeTranslationBounds,
  resizeCropFromHandle,
  stateToImageCropRect,
} from './crop-math';
import { CropOverlayRenderer } from './renderer';
import type {
  CropColors,
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
/*  State helpers                                                     */
/* ------------------------------------------------------------------ */

function extractEditState(state: ImageEditorState): ImageEditorEditState {
  return {
    cropWidth: state.cropWidth,
    cropHeight: state.cropHeight,
    rotation: state.rotation,
    straighten: state.straighten,
    scale: state.scale,
    tx: state.tx,
    ty: state.ty,
    flipHorizontal: state.flipHorizontal,
    flipVertical: state.flipVertical,
    activeTemplate: state.activeTemplate,
    resizeMode: state.resizeMode,
  };
}

function isEditStateDirty(
  current: ImageEditorEditState,
  initial: ImageEditorEditState,
): boolean {
  return (
    current.cropWidth !== initial.cropWidth ||
    current.cropHeight !== initial.cropHeight ||
    current.rotation !== initial.rotation ||
    current.straighten !== initial.straighten ||
    current.scale !== initial.scale ||
    current.tx !== initial.tx ||
    current.ty !== initial.ty ||
    current.flipHorizontal !== initial.flipHorizontal ||
    current.flipVertical !== initial.flipVertical ||
    current.activeTemplate !== initial.activeTemplate ||
    current.resizeMode !== initial.resizeMode
  );
}

function totalAngleRad(state: { rotation: number; straighten: number }): number {
  return (state.rotation + state.straighten) * Math.PI / 180;
}

/* ------------------------------------------------------------------ */
/*  Cursor map                                                        */
/* ------------------------------------------------------------------ */

const CURSOR_MAP: Record<DragHandle, string> = {
  nw: 'nwse-resize', se: 'nwse-resize',
  ne: 'nesw-resize', sw: 'nesw-resize',
  n: 'ns-resize', s: 'ns-resize',
  e: 'ew-resize', w: 'ew-resize',
  move: 'all-scroll',
};

/* ------------------------------------------------------------------ */
/*  Handle hit-testing                                                */
/* ------------------------------------------------------------------ */

function hitTestHandles(
  px: number,
  py: number,
  cropW: number,
  cropH: number,
  hitRadius: number,
): DragHandle | null {
  const hw = cropW / 2;
  const hh = cropH / 2;

  // Handle positions relative to crop center (0, 0)
  const handles: Array<{ handle: DragHandle; x: number; y: number }> = [
    { handle: 'nw', x: -hw, y: -hh },
    { handle: 'n', x: 0, y: -hh },
    { handle: 'ne', x: hw, y: -hh },
    { handle: 'w', x: -hw, y: 0 },
    { handle: 'e', x: hw, y: 0 },
    { handle: 'sw', x: -hw, y: hh },
    { handle: 's', x: 0, y: hh },
    { handle: 'se', x: hw, y: hh },
  ];

  for (const { handle, x, y } of handles) {
    const dx = px - x;
    const dy = py - y;
    if (dx * dx + dy * dy <= hitRadius * hitRadius) {
      return handle;
    }
  }

  // Inside crop rect = move
  if (Math.abs(px) <= hw && Math.abs(py) <= hh) {
    return 'move';
  }

  return null;
}

/* ------------------------------------------------------------------ */
/*  Canvas padding                                                    */
/* ------------------------------------------------------------------ */

const CANVAS_PADDING = 16;

/* ------------------------------------------------------------------ */
/*  Engine                                                            */
/* ------------------------------------------------------------------ */

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
  private pendingSrc: string | null = null;

  // ── Drag ─────────────────────────────────────────────────────────

  private dragOrigin: {
    handle: DragHandle;
    screenX: number;
    screenY: number;
    originalEditState: ImageEditorEditState;
  } | null = null;

  // ── Viewport pan (zoomed past fit) ───────────────────────────────

  private panOrigin: {
    pointerX: number;
    pointerY: number;
    originalOffset: { x: number; y: number };
  } | null = null;

  // ── Rendering ────────────────────────────────────────────────────

  private rafId = 0;
  private dirty = true;
  private renderer = new CropOverlayRenderer();
  private colors: CropColors | null = null;
  private userColors: CropColors | null = null;

  // ── Viewport scale (for screen↔crop coordinate mapping) ─────────

  private viewportScale = 1;
  private viewportOffsetX = 0;
  private viewportOffsetY = 0;

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

    const userTemplates = (options.templates ?? []).filter(
      (t) => t.label !== FREE_TEMPLATE.label,
    );
    const allTemplates = [FREE_TEMPLATE, ...userTemplates];
    const defaultTemplate = options.defaultTemplate ?? FREE_TEMPLATE;

    this.initialEditState = {
      cropWidth: options.initialCropWidth ?? 0,
      cropHeight: options.initialCropHeight ?? 0,
      rotation: 0,
      straighten: 0,
      scale: 1,
      tx: 0,
      ty: 0,
      flipHorizontal: false,
      flipVertical: false,
      activeTemplate: defaultTemplate,
      resizeMode: 'crop',
    };

    this._state = {
      ...this.initialEditState,
      src: options.src ?? '',
      imageStatus: 'idle',
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

    this.pendingSrc = options.src ?? null;
  }

  /* ---------------------------------------------------------------- */
  /*  Mount / Unmount                                                 */
  /* ---------------------------------------------------------------- */

  mount(container: HTMLElement): void {
    if (this.mounted) this.unmount();
    this.container = container;
    this.canvas = document.createElement('canvas');
    this.canvas.style.cssText = 'position:absolute;inset:0;width:100%;height:100%';
    container.appendChild(this.canvas);
    const ctx = this.canvas.getContext('2d');
    if (!ctx) throw new Error('Canvas 2D context not available');
    this.ctx = ctx;

    this.colors = this.userColors ?? this.readColorsFromCSS();

    this.canvas.addEventListener('pointerdown', this.boundPointerDown);
    this.canvas.addEventListener('pointermove', this.boundPointerMove);
    this.canvas.addEventListener('pointerup', this.boundPointerUp);
    this.canvas.addEventListener('pointercancel', this.boundPointerUp);

    this.resizeObserver = new ResizeObserver(() => this.markDirty());
    this.resizeObserver.observe(container);

    this.themeObserver = new MutationObserver(() => {
      if (!this.userColors) this.colors = this.readColorsFromCSS();
      this.markDirty();
    });
    this.themeObserver.observe(document.documentElement, {
      attributes: true, attributeFilter: ['class'],
    });

    this.mounted = true;
    this.scheduleRender();

    if (this.pendingSrc) {
      const src = this.pendingSrc;
      this.pendingSrc = null;
      this.loadImage(src);
    }
  }

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
    this.mounted = false;
  }

  destroy(): void {
    this.loadCancelled = true;
    this.unmount();
  }

  get isMounted(): boolean { return this.mounted; }

  /* ---------------------------------------------------------------- */
  /*  State                                                           */
  /* ---------------------------------------------------------------- */

  get state(): Readonly<ImageEditorState> { return this._state; }

  /**
   * Get the crop rect in original image pixel coordinates.
   * Use this for proto/API output.
   */
  getCropRect() {
    const angle = totalAngleRad(this._state);
    return stateToImageCropRect(
      this._state.cropWidth, this._state.cropHeight,
      this._state.naturalWidth, this._state.naturalHeight,
      this._state.tx, this._state.ty,
      this._state.scale, angle,
    );
  }

  /* ---------------------------------------------------------------- */
  /*  Actions                                                         */
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
      const w = img.naturalWidth;
      const h = img.naturalHeight;
      const editState: ImageEditorEditState = {
        cropWidth: w,
        cropHeight: h,
        rotation: 0,
        straighten: 0,
        scale: 1,
        tx: 0,
        ty: 0,
        flipHorizontal: false,
        flipVertical: false,
        activeTemplate: this._state.activeTemplate,
        resizeMode: 'crop',
      };
      this.initialEditState = editState;
      this.history = { past: [], future: [] };
      this.updateState({
        ...editState,
        src,
        imageStatus: 'loaded',
        imageError: null,
        naturalWidth: w,
        naturalHeight: h,
        canUndo: false, canRedo: false, isDirty: false,
        isDragging: false, activeHandle: null,
        zoom: 100, zoomMode: 'fit',
        panOffset: { x: 0, y: 0 }, isPanning: false,
        mode: 'view',
      });
    };
    img.onerror = () => {
      if (this.loadCancelled) return;
      this.updateState({ imageStatus: 'error', imageError: `Failed to load: ${src}` });
    };
    img.src = src;
  }

  setResizeMode(mode: ResizeMode): void {
    this.pushHistoryAndUpdate({ resizeMode: mode });
  }

  rotateClockwise(): void {
    const rotation = ((this._state.rotation + 90) % 360) as 0 | 90 | 180 | 270;
    this.applyRotationChange(rotation, this._state.straighten);
  }

  rotateCounterClockwise(): void {
    const rotation = ((this._state.rotation + 270) % 360) as 0 | 90 | 180 | 270;
    this.applyRotationChange(rotation, this._state.straighten);
  }

  setStraighten(degrees: number): void {
    const clamped = Math.max(-45, Math.min(45, degrees));
    if (!this.preStraightenEditState) {
      this.preStraightenEditState = extractEditState(this._state);
    }
    this.applyRotationChange(this._state.rotation, clamped, false);
  }

  commitStraighten(): void {
    if (!this.preStraightenEditState) return;
    const pre = this.preStraightenEditState;
    this.preStraightenEditState = null;
    this.history = pushHistory(this.history, pre, this.maxHistory);
    this.updateState({
      canUndo: true, canRedo: false,
      isDirty: isEditStateDirty(extractEditState(this._state), this.initialEditState),
    });
  }

  toggleFlipHorizontal(): void {
    this.pushHistoryAndUpdate({ flipHorizontal: !this._state.flipHorizontal });
  }

  toggleFlipVertical(): void {
    this.pushHistoryAndUpdate({ flipVertical: !this._state.flipVertical });
  }

  applyTemplate(template: CropTemplate | null): void {
    if (!template || template.ratio === null) {
      this.pushHistoryAndUpdate({ activeTemplate: template });
      return;
    }
    const { cropW, cropH } = applyCropTemplate(
      template.ratio,
      this._state.cropWidth, this._state.cropHeight,
      this._state.naturalWidth, this._state.naturalHeight,
    );
    // Recompute scale and clamp translation for new crop size
    const angle = totalAngleRad(this._state);
    const minScale = computeMinScale(cropW, cropH, this._state.naturalWidth, this._state.naturalHeight, angle);
    const scale = Math.max(this._state.scale, minScale);
    const { maxTx, maxTy } = computeTranslationBounds(cropW, cropH, this._state.naturalWidth, this._state.naturalHeight, scale, angle);
    const { tx, ty } = clampTranslation(this._state.tx, this._state.ty, maxTx, maxTy);
    this.pushHistoryAndUpdate({ activeTemplate: template, cropWidth: cropW, cropHeight: cropH, scale, tx, ty });
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
      canUndo: past.length > 0, canRedo: true,
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
      canUndo: true, canRedo: future.length > 0,
      isDirty: isEditStateDirty(next, this.initialEditState),
    });
  }

  zoomIn(): void { this.updateState({ zoom: Math.min(ZOOM_MAX, this._state.zoom + ZOOM_STEP), zoomMode: 'manual' }); }
  zoomOut(): void { this.updateState({ zoom: Math.max(ZOOM_MIN, this._state.zoom - ZOOM_STEP), zoomMode: 'manual' }); }
  zoomToFit(): void { this.updateState({ zoom: 100, zoomMode: 'fit', panOffset: { x: 0, y: 0 } }); }
  setZoom(level: number): void { this.updateState({ zoom: Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, level)), zoomMode: 'manual' }); }

  enterCropMode(): void { this.updateState({ mode: 'crop' }); }
  exitCropMode(): void { this.updateState({ mode: 'view' }); }

  setColors(colors: CropColors): void {
    this.userColors = colors;
    this.colors = colors;
    this.markDirty();
  }

  /* ---------------------------------------------------------------- */
  /*  Internal — Rotation with auto-scale and clamp                   */
  /* ---------------------------------------------------------------- */

  private applyRotationChange(
    rotation: 0 | 90 | 180 | 270,
    straighten: number,
    pushToHistory = true,
  ): void {
    const angle = (rotation + straighten) * Math.PI / 180;
    const { naturalWidth: iw, naturalHeight: ih, cropWidth: cw, cropHeight: ch } = this._state;

    // Enforce minimum scale for new rotation
    const minScale = computeMinScale(cw, ch, iw, ih, angle);
    const scale = Math.max(this._state.scale, minScale);

    // Re-clamp translation
    const { maxTx, maxTy } = computeTranslationBounds(cw, ch, iw, ih, scale, angle);
    const { tx, ty } = clampTranslation(this._state.tx, this._state.ty, maxTx, maxTy);

    if (pushToHistory) {
      this.pushHistoryAndUpdate({ rotation, straighten, scale, tx, ty });
    } else {
      this.updateState({ rotation, straighten, scale, tx, ty });
    }
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
      canUndo: true, canRedo: false,
      isDirty: isEditStateDirty(newEdit, this.initialEditState),
    });
  }

  /* ---------------------------------------------------------------- */
  /*  Internal — Pointer events                                       */
  /* ---------------------------------------------------------------- */

  private screenToCropSpace(e: PointerEvent): { x: number; y: number } | null {
    if (!this.canvas) return null;
    const rect = this.canvas.getBoundingClientRect();
    // Convert screen pixel to crop-centered coordinates
    const screenX = e.clientX - rect.left;
    const screenY = e.clientY - rect.top;
    const cropX = (screenX - this.viewportOffsetX) / this.viewportScale - this._state.cropWidth / 2;
    const cropY = (screenY - this.viewportOffsetY) / this.viewportScale - this._state.cropHeight / 2;
    return { x: cropX, y: cropY };
  }

  private onPointerDown(e: PointerEvent): void {
    if (this._state.imageStatus !== 'loaded' || this._state.mode !== 'crop') return;

    // Alt+click → viewport pan when zoomed
    if (e.button === 1 || (e.button === 0 && e.altKey)) {
      if (this._state.zoomMode === 'manual' && this._state.zoom > 100) {
        e.preventDefault();
        this.canvas?.setPointerCapture(e.pointerId);
        this.panOrigin = {
          pointerX: e.clientX, pointerY: e.clientY,
          originalOffset: { ...this._state.panOffset },
        };
        this.updateState({ isPanning: true });
        return;
      }
    }

    const cropPoint = this.screenToCropSpace(e);
    if (!cropPoint) return;

    const hitRadius = 16 / this.viewportScale;
    const handle = hitTestHandles(cropPoint.x, cropPoint.y, this._state.cropWidth, this._state.cropHeight, hitRadius);

    if (handle) {
      e.preventDefault();
      this.canvas?.setPointerCapture(e.pointerId);
      this.preDragEditState = extractEditState(this._state);
      this.dragOrigin = {
        handle,
        screenX: e.clientX,
        screenY: e.clientY,
        originalEditState: extractEditState(this._state),
      };
      this.updateState({ isDragging: true, activeHandle: handle });
    }
  }

  private onPointerMove(e: PointerEvent): void {
    // Viewport pan
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

    // Drag (move image or resize crop)
    if (this.dragOrigin) {
      e.preventDefault();
      const dx = (e.clientX - this.dragOrigin.screenX) / this.viewportScale;
      const dy = (e.clientY - this.dragOrigin.screenY) / this.viewportScale;
      const orig = this.dragOrigin.originalEditState;
      const angle = totalAngleRad(this._state);

      if (this.dragOrigin.handle === 'move') {
        // Move the image: update tx, ty, then clamp
        const newTx = orig.tx + dx;
        const newTy = orig.ty + dy;
        const { maxTx, maxTy } = computeTranslationBounds(
          this._state.cropWidth, this._state.cropHeight,
          this._state.naturalWidth, this._state.naturalHeight,
          this._state.scale, angle,
        );
        const clamped = clampTranslation(newTx, newTy, maxTx, maxTy);
        this._state = { ...this._state, tx: clamped.tx, ty: clamped.ty };
      } else {
        // Resize crop — clamp to maximum valid size instead of rejecting
        let { cropW, cropH } = resizeCropFromHandle(
          this.dragOrigin.handle, dx, dy,
          orig.cropWidth, orig.cropHeight,
          orig.activeTemplate?.ratio ?? null,
        );

        // Clamp crop size to what the image can fill at current scale+rotation
        const absCos = Math.abs(Math.cos(angle));
        const absSin = Math.abs(Math.sin(angle));
        const iw = this._state.naturalWidth;
        const ih = this._state.naturalHeight;
        const maxW = iw * this._state.scale * absCos + ih * this._state.scale * absSin;
        const maxH = iw * this._state.scale * absSin + ih * this._state.scale * absCos;
        cropW = Math.min(cropW, maxW);
        cropH = Math.min(cropH, maxH);

        // If aspect ratio is locked, re-enforce it after clamping
        const ratio = orig.activeTemplate?.ratio ?? null;
        if (ratio !== null) {
          if (cropW / cropH > ratio) {
            cropW = cropH * ratio;
          } else {
            cropH = cropW / ratio;
          }
        }

        // Recompute scale + clamp translation
        const minScale = computeMinScale(cropW, cropH, iw, ih, angle);
        const scale = Math.max(this._state.scale, minScale);
        const { maxTx, maxTy } = computeTranslationBounds(cropW, cropH, iw, ih, scale, angle);
        const clamped = clampTranslation(this._state.tx, this._state.ty, maxTx, maxTy);
        this._state = { ...this._state, cropWidth: cropW, cropHeight: cropH, scale, tx: clamped.tx, ty: clamped.ty };
      }

      this.markDirty();
      this.onChange?.(this._state);
      return;
    }

    // Hover cursor
    if (this._state.imageStatus !== 'loaded' || this._state.mode !== 'crop') return;
    const cropPoint = this.screenToCropSpace(e);
    if (!cropPoint || !this.canvas) return;
    const handle = hitTestHandles(cropPoint.x, cropPoint.y, this._state.cropWidth, this._state.cropHeight, 16 / this.viewportScale);
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
        isDragging: false, activeHandle: null,
        canUndo: true, canRedo: false,
        isDirty: isEditStateDirty(editState, this.initialEditState),
      });
    }
  }

  /* ---------------------------------------------------------------- */
  /*  Internal — Rendering                                            */
  /* ---------------------------------------------------------------- */

  private markDirty(): void { this.dirty = true; }

  private scheduleRender(): void {
    const loop = () => {
      if (this.dirty) { this.dirty = false; this.render(); }
      this.rafId = requestAnimationFrame(loop);
    };
    this.rafId = requestAnimationFrame(loop);
  }

  private render(): void {
    const { canvas, ctx, image, container, colors } = this;
    const state = this._state;
    if (!canvas || !ctx || !image || !container || !colors || state.imageStatus !== 'loaded') return;

    const dpr = window.devicePixelRatio || 1;
    const containerW = container.clientWidth;
    const containerH = container.clientHeight;

    canvas.width = containerW * dpr;
    canvas.height = containerH * dpr;
    canvas.style.width = `${containerW}px`;
    canvas.style.height = `${containerH}px`;
    ctx.scale(dpr, dpr);

    ctx.fillStyle = colors.canvas;
    ctx.fillRect(0, 0, containerW, containerH);

    const { cropWidth: cw, cropHeight: ch, tx, ty, rotation, straighten, flipHorizontal, flipVertical, scale: imgScale } = state;
    const isCropMode = state.mode === 'crop';
    const effectiveZoom = state.zoomMode === 'fit' ? 100 : state.zoom;

    // Compute viewport scale: how many screen pixels per crop pixel
    // Same padding in both modes so the image stays in the same position
    const pad = CANVAS_PADDING;
    const drawW = containerW - pad * 2;
    const drawH = containerH - pad * 2;
    const vpScale = Math.min(drawW / cw, drawH / ch) * (effectiveZoom / 100);
    const cropScreenW = cw * vpScale;
    const cropScreenH = ch * vpScale;
    const cropScreenX = (containerW - cropScreenW) / 2 + (state.panOffset.x || 0);
    const cropScreenY = (containerH - cropScreenH) / 2 + (state.panOffset.y || 0);

    this.viewportScale = vpScale;
    this.viewportOffsetX = cropScreenX;
    this.viewportOffsetY = cropScreenY;

    const angle = (rotation + straighten) * Math.PI / 180;

    if (isCropMode) {
      // ── CROP MODE ───────────────────────────────────────────────
      // Draw the image centered in the crop viewport with transforms
      ctx.save();
      ctx.translate(cropScreenX + cropScreenW / 2 + tx * vpScale, cropScreenY + cropScreenH / 2 + ty * vpScale);
      ctx.rotate(angle);
      ctx.scale(
        (flipHorizontal ? -1 : 1) * imgScale * vpScale,
        (flipVertical ? -1 : 1) * imgScale * vpScale,
      );
      ctx.drawImage(image, -image.width / 2, -image.height / 2);
      ctx.restore();

      // Overlay (screen space, NOT rotated)
      this.renderer.drawScreenOverlay(ctx, containerW, containerH, cropScreenX, cropScreenY, cropScreenW, cropScreenH, colors.overlay);

      // Handles (in crop viewport space)
      ctx.save();
      ctx.translate(cropScreenX, cropScreenY);
      ctx.scale(vpScale, vpScale);
      this.renderer.drawControls(ctx, state, vpScale, colors);
      ctx.restore();

    } else {
      // ── VIEW MODE ───────────────────────────────────────────────
      ctx.save();
      ctx.beginPath();
      ctx.rect(cropScreenX, cropScreenY, cropScreenW, cropScreenH);
      ctx.clip();
      ctx.translate(cropScreenX + cropScreenW / 2 + tx * vpScale, cropScreenY + cropScreenH / 2 + ty * vpScale);
      ctx.rotate(angle);
      ctx.scale(
        (flipHorizontal ? -1 : 1) * imgScale * vpScale,
        (flipVertical ? -1 : 1) * imgScale * vpScale,
      );
      ctx.drawImage(image, -image.width / 2, -image.height / 2);
      ctx.restore();
    }
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
