'use client';

import { cn } from '@pivox/primitives/utils';
import { Button } from '@pivox/primitives/button';
import { Kbd } from '@pivox/primitives/kbd';
import { Slider } from '@pivox/primitives/slider';
import { Toggle } from '@pivox/primitives/toggle';
import { Tabs, TabsList, TabsTrigger } from '@pivox/primitives/tabs';
import { Separator } from '@pivox/primitives/separator';
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@pivox/primitives/tooltip';
import {
  Crop,
  FlipHorizontal2,
  FlipVertical2,
  Minus,
  Plus,
  Redo2,
  RotateCcw as ResetIcon,
  RotateCcwSquare,
  RotateCwSquare,
  Undo2,
  X,
} from 'lucide-react';
import { ImageEditorContext, useImageEditorContext } from './image-editor.context';
import type { ResizeMode } from '@pivox/image-editor';
import type { ImageEditorContextValue } from './image-editor.types';

/* ------------------------------------------------------------------ */
/*  ShortcutHint — renders keyboard shortcut text in tooltips          */
/* ------------------------------------------------------------------ */

function ShortcutHint({ shortcut }: { shortcut?: string }) {
  if (!shortcut) return null;
  return <Kbd>{shortcut}</Kbd>;
}

/* ------------------------------------------------------------------ */
/*  Provider                                                          */
/* ------------------------------------------------------------------ */

function ImageEditorProvider({
  value,
  children,
}: {
  value: ImageEditorContextValue;
  children: React.ReactNode;
}) {
  return <ImageEditorContext value={value}>{children}</ImageEditorContext>;
}

/* ------------------------------------------------------------------ */
/*  Root                                                              */
/* ------------------------------------------------------------------ */

function ImageEditorRoot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div
      data-slot="image-editor"
      className={cn('flex h-full w-full flex-col', className)}
    >
      {children}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Canvas                                                            */
/* ------------------------------------------------------------------ */

function ImageEditorCanvas({ className }: { className?: string }) {
  const { state, meta } = useImageEditorContext();

  return (
    <div
      ref={meta.containerRef}
      data-slot="image-editor-canvas"
      className={cn(
        'relative flex-1 overflow-hidden bg-image-editor-canvas',
        className,
      )}
    >
      {state.imageStatus === 'loading' && (
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="text-sm text-muted-foreground">Loading image...</div>
        </div>
      )}
      {state.imageStatus === 'error' && (
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="text-sm text-destructive">
            {state.imageError ?? 'Failed to load image'}
          </div>
        </div>
      )}
      {/* Canvas element is created and managed by the engine */}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Toolbar                                                           */
/* ------------------------------------------------------------------ */

function ImageEditorToolbar({
  className,
  children,
}: {
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <div
      data-slot="image-editor-toolbar"
      className={cn(
        'flex items-center gap-1 border-b bg-background px-2 py-1',
        className,
      )}
    >
      {children}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  ResizeModePicker                                                  */
/* ------------------------------------------------------------------ */

const RESIZE_MODE_DESCRIPTIONS: Record<string, string> = {
  crop: 'Exact crop area, no scaling',
  cover: 'Scale to fill, crop overflow',
  fit: 'Scale to fit, may add bars',
};

function ImageEditorResizeModePicker({ className }: { className?: string }) {
  const { state, actions } = useImageEditorContext();

  if (state.mode !== 'crop') return null;

  return (
    <div className={className}>
      <Tabs
        value={state.resizeMode}
        onValueChange={(v) => actions.setResizeMode(v as ResizeMode)}
      >
        <TabsList>
          <TabsTrigger value="crop">Crop</TabsTrigger>
          <TabsTrigger value="cover">Cover</TabsTrigger>
          <TabsTrigger value="fit">Fit</TabsTrigger>
        </TabsList>
      </Tabs>
      <p className="mt-1.5 text-[11px] text-muted-foreground">
        {RESIZE_MODE_DESCRIPTIONS[state.resizeMode]}
      </p>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  TemplatePicker                                                    */
/* ------------------------------------------------------------------ */

/** Renders a filled rectangle icon showing the aspect ratio proportions. */
function AspectRatioIcon({ ratio }: { ratio: number | null }) {
  const maxSize = 20;
  let w: number;
  let h: number;

  if (ratio === null) {
    // Free — dashed rect
    return (
      <svg width={maxSize} height={maxSize} viewBox="0 0 20 20" className="shrink-0">
        <rect
          x="2" y="4" width="16" height="12" rx="1"
          fill="none" stroke="currentColor" strokeWidth="1.5" strokeDasharray="2 2"
        />
      </svg>
    );
  }

  if (ratio >= 1) {
    w = maxSize;
    h = Math.round(maxSize / ratio);
  } else {
    h = maxSize;
    w = Math.round(maxSize * ratio);
  }
  const x = Math.round((maxSize - w) / 2);
  const y = Math.round((maxSize - h) / 2);

  return (
    <svg width={maxSize} height={maxSize} viewBox="0 0 20 20" className="shrink-0">
      <rect x={x} y={y} width={w} height={h} rx="1" fill="currentColor" opacity="0.6" />
    </svg>
  );
}

function ImageEditorTemplatePicker({ className }: { className?: string }) {
  const { state, actions } = useImageEditorContext();

  if (state.mode !== 'crop') return null;

  return (
    <div
      data-slot="image-editor-template-picker"
      className={cn('grid grid-cols-2 gap-1', className)}
    >
      {state.templates.map((template) => {
        const isActive =
          state.activeTemplate !== null &&
          state.activeTemplate.label === template.label &&
          state.activeTemplate.ratio === template.ratio;
        return (
          <button
            key={template.label}
            type="button"
            onClick={() =>
              actions.applyTemplate(isActive ? null : template)
            }
            aria-pressed={isActive}
            className={cn(
              'flex items-center gap-2 rounded-md px-2 py-1.5 text-xs transition-colors',
              isActive
                ? 'bg-primary/10 text-primary ring-1 ring-primary/30'
                : 'text-muted-foreground hover:bg-muted hover:text-foreground',
            )}
          >
            <AspectRatioIcon ratio={template.ratio} />
            <span>{template.label}</span>
          </button>
        );
      })}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  RotateControls                                                    */
/* ------------------------------------------------------------------ */

function ImageEditorRotateControls({ className }: { className?: string }) {
  const { state, actions, meta } = useImageEditorContext();

  if (state.mode !== 'crop') return null;

  return (
    <div
      data-slot="image-editor-rotate-controls"
      className={cn('flex items-center gap-1', className)}
    >
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={actions.rotateCounterClockwise}
            aria-label="Rotate counter-clockwise"
          >
            <RotateCcwSquare />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          Rotate left
          <ShortcutHint shortcut={meta.shortcuts.rotateCounterClockwise} />
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={actions.rotateClockwise}
            aria-label="Rotate clockwise"
          >
            <RotateCwSquare />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          Rotate right
          <ShortcutHint shortcut={meta.shortcuts.rotateClockwise} />
        </TooltipContent>
      </Tooltip>

      <Separator orientation="vertical" className="mx-1 h-5" />

      <div className="flex items-center gap-2">
        <span className="w-10 text-right font-mono text-xs text-muted-foreground">
          {state.straighten.toFixed(1)}°
        </span>
        <Slider
          value={[state.straighten]}
          min={-45}
          max={45}
          step={0.1}
          onValueChange={([v]) => {
            if (v !== undefined) actions.setStraighten(v);
          }}
          onValueCommit={() => actions.commitStraighten()}
          className="w-24"
          aria-label="Straighten angle"
        />
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  FlipControls                                                      */
/* ------------------------------------------------------------------ */

function ImageEditorFlipControls({ className }: { className?: string }) {
  const { state, actions, meta } = useImageEditorContext();

  if (state.mode !== 'crop') return null;

  return (
    <div
      data-slot="image-editor-flip-controls"
      className={cn('flex items-center gap-1', className)}
    >
      <Tooltip>
        <TooltipTrigger asChild>
          <Toggle
            pressed={state.flipHorizontal}
            onPressedChange={actions.toggleFlipHorizontal}
            size="sm"
            aria-label="Flip horizontal"
          >
            <FlipHorizontal2 />
          </Toggle>
        </TooltipTrigger>
        <TooltipContent>
          Flip horizontal
          <ShortcutHint shortcut={meta.shortcuts.flipHorizontal} />
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Toggle
            pressed={state.flipVertical}
            onPressedChange={actions.toggleFlipVertical}
            size="sm"
            aria-label="Flip vertical"
          >
            <FlipVertical2 />
          </Toggle>
        </TooltipTrigger>
        <TooltipContent>
          Flip vertical
          <ShortcutHint shortcut={meta.shortcuts.flipVertical} />
        </TooltipContent>
      </Tooltip>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  UndoRedoControls                                                  */
/* ------------------------------------------------------------------ */

function ImageEditorUndoRedoControls({ className }: { className?: string }) {
  const { state, actions, meta } = useImageEditorContext();

  return (
    <div
      data-slot="image-editor-undo-redo"
      className={cn('flex items-center gap-1', className)}
    >
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={actions.undo}
            disabled={!state.canUndo}
            aria-label="Undo"
          >
            <Undo2 />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          Undo
          <ShortcutHint shortcut={meta.shortcuts.undo} />
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={actions.redo}
            disabled={!state.canRedo}
            aria-label="Redo"
          >
            <Redo2 />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          Redo
          <ShortcutHint shortcut={meta.shortcuts.redo} />
        </TooltipContent>
      </Tooltip>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  ResetButton                                                       */
/* ------------------------------------------------------------------ */

function ImageEditorResetButton({ className }: { className?: string }) {
  const { state, actions, meta } = useImageEditorContext();

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={actions.reset}
          disabled={!state.isDirty}
          className={className}
          aria-label="Reset all edits"
        >
          <ResetIcon />
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        Reset
        <ShortcutHint shortcut={meta.shortcuts.reset} />
      </TooltipContent>
    </Tooltip>
  );
}

/* ------------------------------------------------------------------ */
/*  ZoomControls                                                      */
/* ------------------------------------------------------------------ */

function ImageEditorZoomControls({ className }: { className?: string }) {
  const { state, actions, meta } = useImageEditorContext();

  const displayZoom = state.zoomMode === 'fit' ? 'Fit' : `${state.zoom}%`;

  return (
    <div
      data-slot="image-editor-zoom-controls"
      className={cn('flex items-center gap-1', className)}
    >
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={actions.zoomOut}
            aria-label="Zoom out"
          >
            <Minus className="size-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          Zoom out
          <ShortcutHint shortcut={meta.shortcuts.zoomOut} />
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={actions.zoomToFit}
            className="min-w-[3.5rem] rounded px-1.5 py-0.5 text-center text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            {displayZoom}
          </button>
        </TooltipTrigger>
        <TooltipContent>
          Fit to screen
          <ShortcutHint shortcut={meta.shortcuts.zoomToFit} />
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={actions.zoomIn}
            aria-label="Zoom in"
          >
            <Plus className="size-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          Zoom in
          <ShortcutHint shortcut={meta.shortcuts.zoomIn} />
        </TooltipContent>
      </Tooltip>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  CropButton / CloseCropButton                                      */
/* ------------------------------------------------------------------ */

function ImageEditorCropButton({ className }: { className?: string }) {
  const { state, actions } = useImageEditorContext();

  if (state.mode === 'crop') return null;

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={actions.enterCropMode}
      className={className}
    >
      <Crop className="size-3.5" />
      Crop
    </Button>
  );
}

function ImageEditorCloseCropButton({ className }: { className?: string }) {
  const { state, actions } = useImageEditorContext();

  if (state.mode !== 'crop') return null;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={actions.exitCropMode}
          className={className}
          aria-label="Close crop mode"
        >
          <X />
        </Button>
      </TooltipTrigger>
      <TooltipContent>Close crop</TooltipContent>
    </Tooltip>
  );
}

/* ------------------------------------------------------------------ */
/*  DirtyIndicator                                                    */
/* ------------------------------------------------------------------ */

function ImageEditorDirtyIndicator({
  children,
}: {
  children: React.ReactNode;
}) {
  const { state } = useImageEditorContext();
  if (!state.isDirty) return null;
  return <>{children}</>;
}

/* ------------------------------------------------------------------ */
/*  ToolSlot / SidebarSlot (injection points)                         */
/* ------------------------------------------------------------------ */

function ImageEditorToolSlot({
  className,
  children,
}: {
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <div
      data-slot="image-editor-tool-slot"
      className={cn('flex items-center gap-1', className)}
    >
      {children}
    </div>
  );
}

function ImageEditorSidebarSlot({
  className,
  children,
}: {
  className?: string;
  children?: React.ReactNode;
}) {
  const { state } = useImageEditorContext();

  if (state.mode !== 'crop') return null;

  return (
    <div
      data-slot="image-editor-sidebar-slot"
      className={cn('flex flex-col gap-3 border-l bg-background p-3', className)}
    >
      {children}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Compound export                                                   */
/* ------------------------------------------------------------------ */

export const ImageEditor = {
  Provider: ImageEditorProvider,
  Root: ImageEditorRoot,
  Canvas: ImageEditorCanvas,
  Toolbar: ImageEditorToolbar,
  ResizeModePicker: ImageEditorResizeModePicker,
  TemplatePicker: ImageEditorTemplatePicker,
  RotateControls: ImageEditorRotateControls,
  FlipControls: ImageEditorFlipControls,
  UndoRedoControls: ImageEditorUndoRedoControls,
  ResetButton: ImageEditorResetButton,
  ZoomControls: ImageEditorZoomControls,
  CropButton: ImageEditorCropButton,
  CloseCropButton: ImageEditorCloseCropButton,
  DirtyIndicator: ImageEditorDirtyIndicator,
  ToolSlot: ImageEditorToolSlot,
  SidebarSlot: ImageEditorSidebarSlot,
  Context: ImageEditorContext,
};
