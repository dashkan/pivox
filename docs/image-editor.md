# Image Editor

A canvas-based image editor for broadcast production asset management. Supports crop, rotate, straighten, flip, pan, and zoom with a zero-dead-pixels guarantee.

## Architecture

```
@pivox/image-editor     — Vanilla JS engine (no framework deps)
@pivox/ui               — React compound components (thin wrapper)
@pivox/features          — Platform-aware wiring (keyboard shortcuts, OS detection)
```

### Package Responsibilities

**`@pivox/image-editor`** (the engine)
- `ImageEditorEngine` class — state, undo/redo, pointer events, canvas rendering
- `CropOverlayRenderer` — handles, grid, overlay drawing
- `crop-math.ts` — minScale, translation bounds, clamping, aspect ratio math
- Zero framework dependencies. Mounts on any `HTMLElement`.

**`@pivox/ui`** (React wrapper)
- `useImageEditorState` hook — creates engine, syncs state to React via version counter
- `ImageEditor.*` compound components — Provider, Canvas, Toolbar, TemplatePicker, etc.
- ~100 lines. One ref (engine instance). No stale closures.

**`@pivox/features`** (behavior wiring)
- `useImageEditorFeature` — platform detection, keyboard shortcut defaults, keybinding handler
- `ImageEditorFeature` — Provider component for consuming apps

---

## State Model: Crop-as-Viewport

The editor uses a **crop-as-viewport** model. The crop rect is a fixed window. The image transforms (scale, rotate, translate) behind it.

### Why Not Crop-as-Rectangle?

The first implementation stored the crop as `{ x, y, width, height }` in image-pixel space. The image was fixed, the crop rect moved. This caused:

1. **Circular dependency with rotation zoom.** The zoom needed to fill the crop rect depended on the crop rect position. Moving the crop changed the required zoom, which changed the allowed positions. Every operation fought with every other operation.

2. **Movement clamping was wrong at every angle.** `clampCropRect` used the original image bounds, but after rotation the effective bounds are different. For 90° rotation of a portrait image, the crop rect couldn't move at all because the original bounds were too small.

3. **Nine React refs.** Scale, offset, drag origin, pan origin, image element, canvas, render frame, hover handle, previous edit state — all refs to work around React's render cycle. Every callback had potential stale closure bugs.

### The Correct Model

Inspired by [Gemini's analysis of img.ly](discussions/gemini-crop-algo.md):

| Field | Type | Description |
|-------|------|-------------|
| `cropWidth` | number | Crop viewport width in image pixels |
| `cropHeight` | number | Crop viewport height in image pixels |
| `rotation` | 0/90/180/270 | Quarter-turn rotation |
| `straighten` | number | Fine rotation (-45° to 45°) |
| `scale` | number | Image scale (≥ minScale) |
| `tx` | number | Image X translation (relative to crop center) |
| `ty` | number | Image Y translation (relative to crop center) |
| `flipHorizontal` | boolean | Horizontal mirror |
| `flipVertical` | boolean | Vertical mirror |

The crop rect has no position — it's always centered. The image moves behind it.

---

## Core Algorithm: No Dead Pixels

Three formulas guarantee zero dead pixels at any rotation angle and translation:

### 1. Minimum Scale

The minimum scale ensures the rotated image completely covers the crop rect:

```
θ = rotation + straighten (in radians)

minScale = max(
  (cropW × |cos θ| + cropH × |sin θ|) / imgW,
  (cropW × |sin θ| + cropH × |cos θ|) / imgH
)
```

This projects the crop rect's dimensions onto the rotated image's axes. The image must be large enough on both axes to cover the crop.

### 2. Translation Bounds (Inverse Projection)

The maximum pan distance prevents crop corners from exiting the rotated image:

```
hIW = imgW × scale / 2    (half scaled image width)
hIH = imgH × scale / 2    (half scaled image height)
hCW = cropW / 2            (half crop width)
hCH = cropH / 2            (half crop height)

txLimit1 = (hIW - (hCW × |cos θ| + hCH × |sin θ|)) / |cos θ|
txLimit2 = (hIH - (hCW × |sin θ| + hCH × |cos θ|)) / |sin θ|
maxTx = max(0, min(txLimit1, txLimit2))

tyLimit1 = (hIW - (hCW × |cos θ| + hCH × |sin θ|)) / |sin θ|
tyLimit2 = (hIH - (hCW × |sin θ| + hCH × |cos θ|)) / |cos θ|
maxTy = max(0, min(tyLimit1, tyLimit2))

tx = clamp(tx, -maxTx, maxTx)
ty = clamp(ty, -maxTy, maxTy)
```

This is the [Gemini inverse-projection formula](discussions/gemini-clamping-fix.md). It projects crop rect corners into the image's unrotated coordinate space and ensures they stay within bounds.

### 3. Resize with Auto-Zoom

When the user resizes the crop rect (drags a handle):

```
newMinScale = computeMinScale(newCropW, newCropH, imgW, imgH, angle)
scale = max(currentScale, newMinScale)
// Re-clamp translation for new crop size
{ maxTx, maxTy } = computeTranslationBounds(newCropW, newCropH, ...)
tx = clamp(tx, -maxTx, maxTx)
ty = clamp(ty, -maxTy, maxTy)
```

If the crop grows, scale increases to accommodate (zoom out). If the crop shrinks, scale stays (more room to pan). Translation always re-clamped. See [Gemini resize discussion](discussions/gemini-resize-logic.md).

### Why the First Algorithm Failed

The first implementation used `computeRotationZoom` which inverse-rotated all 4 corners of the crop rect and checked if they fell within scaled image bounds. This was **mathematically correct for rendering** but **wrong for movement clamping** because:

- The zoom depended on the crop rect position (circular dependency)
- Moving the crop changed the required zoom, which changed the bounds
- For 90° rotation, `clampCropRect` used original image dimensions (1920×2658) but the effective dimensions were swapped (2658×1920) — leaving zero movement freedom

The Gemini formula decouples everything: minScale depends only on crop SIZE and angle (not position), translation bounds depend on scale and angle (not crop position). No circular dependencies.

---

## Rendering

### Canvas Pipeline

```
1. Fill background (from CSS variable --image-editor-canvas)
2. Compute viewport scale (crop rect → screen pixels)
3. CROP MODE:
   a. Draw image: translate to crop center + tx/ty, rotate, scale, flip, drawImage centered
   b. Draw overlay: screen-space dim outside crop rect (NOT rotated)
   c. Draw controls: handles, grid, border in crop viewport space
4. VIEW MODE:
   a. Clip to crop rect screen area
   b. Draw image: same transform as crop mode
```

The overlay is drawn in screen space (not rotated with the image). This ensures the dim area always aligns with the crop rect border regardless of rotation angle.

### CSS Rendering (No Canvas)

The state maps directly to CSS transforms for displaying the crop result via an `<img>` tag:

```html
<div style="width: {cropWidth}px; height: {cropHeight}px; overflow: hidden;">
  <img
    src="..."
    style="transform-origin: center;
           transform: translate({tx}px, {ty}px)
                      rotate({rotation + straighten}deg)
                      scale({flipH * scale}, {flipV * scale});"
  />
</div>
```

### Proto/API Output

The server receives `CropArea { x, y, width, height }` in original image pixel coordinates:

```
offsetX = (-tx / scale) × cos(-θ) - (-ty / scale) × sin(-θ)
offsetY = (-tx / scale) × sin(-θ) + (-ty / scale) × cos(-θ)

centerX = imgW / 2 + offsetX
centerY = imgH / 2 + offsetY

w = cropW / scale
h = cropH / scale

CropArea { x: centerX - w/2, y: centerY - h/2, width: w, height: h }
```

Use `stateToImageCropRect()` from `@pivox/image-editor` for this conversion.

---

## Usage

### Basic (Vanilla JS)

```typescript
import { ImageEditorEngine } from '@pivox/image-editor';

const engine = new ImageEditorEngine({
  src: 'photo.jpg',
  templates: [
    { label: '16:9', ratio: 16/9 },
    { label: '1:1', ratio: 1 },
  ],
});

engine.mount(document.getElementById('editor-container'));
engine.onChange = (state) => updateUI(state);

// Actions
engine.enterCropMode();
engine.rotateClockwise();
engine.setStraighten(15);
engine.commitStraighten();
engine.applyTemplate({ label: '16:9', ratio: 16/9 });
engine.exitCropMode();

// Get output
const cropRect = engine.getCropRect();
// { x, y, width, height } in original image pixels

engine.destroy();
```

### React (via @pivox/ui)

```tsx
import { ImageEditorFeature } from '@pivox/features/image-editor';
import { ImageEditor, DEFAULT_CROP_TEMPLATES } from '@pivox/ui/image-editor';
import { TooltipProvider } from '@pivox/primitives/tooltip';

function Editor() {
  return (
    <TooltipProvider>
      <ImageEditorFeature
        src="photo.jpg"
        templates={DEFAULT_CROP_TEMPLATES}
        onChange={(editState) => console.log(editState)}
      >
        <ImageEditor.Toolbar>
          <ImageEditor.CropButton />
          <ImageEditor.CloseCropButton />
          <ImageEditor.UndoRedoControls />
          <ImageEditor.ResetButton />
          <ImageEditor.RotateControls />
          <ImageEditor.FlipControls />
          <ImageEditor.ZoomControls />
        </ImageEditor.Toolbar>

        <ImageEditor.Root>
          <ImageEditor.Canvas />
        </ImageEditor.Root>

        <ImageEditor.SidebarSlot>
          <ImageEditor.ResizeModePicker />
          <ImageEditor.TemplatePicker />
        </ImageEditor.SidebarSlot>
      </ImageEditorFeature>
    </TooltipProvider>
  );
}
```

### Engine Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `src` | string | — | Image URL or base64 data URI |
| `templates` | CropTemplate[] | [] | Aspect ratio presets (Free is always built-in) |
| `defaultTemplate` | CropTemplate | Free | Auto-selected template on load |
| `maxHistory` | number | 50 | Undo history depth |
| `onChange` | (state) => void | — | Called on every state change |
| `onEditChange` | (editState) => void | — | Called when edit fields change |
| `colors` | CropColors | CSS vars | Override crop overlay colors |

### CropColors (Themed via CSS)

Defined in `@pivox/ui/colors.css`:

```css
:root {
  --image-editor-canvas: oklch(0.95 0.002 197.1);
  --image-editor-crop-border: oklch(0.62 0.19 255);
  --image-editor-crop-handle: oklch(0.62 0.19 255);
  --image-editor-crop-grid: oklch(0.55 0 0 / 40%);
  --image-editor-crop-overlay: oklch(0 0 0 / 50%);
}

.dark {
  --image-editor-canvas: oklch(0.2 0.005 220);
  --image-editor-crop-border: oklch(0.7 0.17 255);
  --image-editor-crop-handle: oklch(0.7 0.17 255);
  --image-editor-crop-grid: oklch(0.7 0 0 / 35%);
  --image-editor-crop-overlay: oklch(0 0 0 / 60%);
}
```

### Keyboard Shortcuts (via @pivox/features)

Shortcuts are provided by the features layer with platform-aware defaults:

| Action | Mac | Windows |
|--------|-----|---------|
| Undo | ⌘Z | Ctrl+Z |
| Redo | ⌘⇧Z | Ctrl+Y |
| Rotate CW | ] | ] |
| Rotate CCW | [ | [ |
| Flip H | H | H |
| Flip V | V | V |
| Zoom In | ⌘+ | Ctrl++ |
| Zoom Out | ⌘- | Ctrl+- |
| Fit | ⌘0 | Ctrl+0 |
| Reset | ⌘⇧R | Ctrl+Shift+R |

Shortcuts are display-only in the UI package (shown in tooltips via `Kbd` primitive). The features package handles the actual `keydown` listener. Consumers can override or disable entirely.

---

## Lessons Learned

### 1. Vanilla JS > React for Canvas

The first implementation used 9 React refs, 4 `useCallback` hooks with stale closure bugs, and `useEffect` chains to drive `requestAnimationFrame`. Refactoring to a vanilla `ImageEditorEngine` class eliminated all of it — state is just class properties, the render loop is a simple rAF dirty flag, pointer events are native DOM.

The React wrapper is ~100 lines: one ref (engine instance), one version counter for re-renders, one callback ref for mount/unmount. Zero stale closures because actions delegate directly to engine methods.

### 2. The Coordinate Model Matters More Than the Code

We spent days patching the crop-as-rectangle model. Each fix introduced new edge cases. The Gemini crop-as-viewport model fixed everything in one pass because the math has no circular dependencies.

### 3. Use the Right Tool for the Right Job

Gemini wrote the rotation zoom and translation clamping algorithms from a pure math description — better than what we produced with iterative coding. Claude handled the full system wiring: React integration, compound components, state management, build tooling, testing, and architecture decisions. Different strengths, both needed.

### 4. Playwright for Visual Verification

Using Playwright MCP to take screenshots after every change caught issues immediately — handle seams, dead pixels, theme mismatches, stale state. Manual testing alone would have missed many of these.

### 5. CSS Variables for Canvas Colors

The canvas reads colors from CSS custom properties (`:root` variables), not from Tailwind theme tokens (which are compiled away at build time). A `MutationObserver` on `<html>` class changes triggers re-render on theme switch.
