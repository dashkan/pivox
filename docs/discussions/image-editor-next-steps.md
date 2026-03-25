# Image Editor — Next Steps Discussion

## 1. Rename: Crop → ImageTransform

The engine is no longer just a crop tool. It handles scale, rotate, straighten, translate, flip, and crop. The crop rect is a derived output, not the primary concept.

**Proposal:**
- Proto stays `Crop` (server operation — it receives x/y/w/h + rotation + flip)
- Engine/UI renames to `ImageTransform` / `ImageTransformEngine`
- `ImageEditorEditState` → `ImageTransformState`
- Package `@pivox/image-editor` → `@pivox/image-transform` (or keep name, change internals)

**When:** Dedicated rename pass once feature set is stable. No rush.

---

## 2. Alpha Channel Detection + Checkerboard Background

**Requirement:** Detect if source image has transparency, show checkerboard behind transparent pixels, provide a toggle.

**Approach:** Full pixel scan on load (not sampling — too unreliable for small transparent corners). Pre-upload use case means no server metadata available.

```typescript
function detectAlpha(img: HTMLImageElement): boolean {
  const canvas = new OffscreenCanvas(img.width, img.height);
  const ctx = canvas.getContext('2d')!;
  ctx.drawImage(img, 0, 0);
  const data = ctx.getImageData(0, 0, img.width, img.height).data;
  for (let i = 3; i < data.length; i += 4) {
    if (data[i] < 255) return true;
  }
  return false;
}
```

- **Performance:** Early exit on first transparent pixel. Worst case (opaque JPEG) ~15-20ms for 16MB image.
- **Scan once** on image load, cache result in engine.
- **Checkerboard:** Drawn within image bounds only, transforms WITH the image (rotate/scale/translate aligned).
- **Toggle:** Visible in both view and crop mode toolbar.

**Engine option:**
```typescript
interface ImageEditorEngineOptions {
  showCheckerboard?: boolean;  // user toggle
}
// Engine exposes:
engine.hasAlpha: boolean;  // detected on load
```

---

## 3. Allow Dead Pixels + Background Color

**Requirement:** Make the "no dead pixels" behavior configurable. When dead pixels are allowed, let user pick a background color (including transparent).

**Engine option:**
```typescript
interface ImageEditorEngineOptions {
  allowDeadPixels?: boolean;  // default: false
  backgroundColor?: string | 'transparent';  // for dead pixel areas
}
```

**When `allowDeadPixels: true`:**
- `computeMinScale` returns base fit scale (no auto-zoom on rotation)
- `computeTranslationBounds` returns Infinity (no pan clamping)
- Crop rect can exceed image bounds
- Dead areas filled with `backgroundColor`
- If transparent, show checkerboard in dead areas too

**Proto extension needed:**
```protobuf
message ImageTransform {
  // ...existing fields
  BackgroundFill background = 6;
}

message BackgroundFill {
  oneof fill {
    string color = 1;       // hex e.g. "#FF0000"
    bool transparent = 2;    // requires alpha-capable output
  }
}
```

**Format conversion:** If source is JPEG (no alpha) but user selects transparent background with dead pixels, the output must be PNG/WebP. The asset version resource proto needs an `output_content_type` field for format conversion.

---

## 4. Adjustments (Future Feature)

The engine architecture supports this — adjustments are just more transform state applied during the render pipeline.

**Planned adjustments:**
- Brightness, Contrast, Saturation
- Exposure, Temperature
- Highlights, Shadows
- Sharpness
- Filters (presets)
- Vignette, Grain

**Render pipeline order:**
1. Draw image (with crop/rotate/scale/translate) ← **done**
2. Apply adjustments (via `ctx.filter` CSS filter syntax — GPU accelerated)
3. Draw overlay/handles ← **done**

**The rename to `ImageTransform` makes even more sense with adjustments** — the crop is one tool, adjustments is another, filters is another. All operating on the same engine, same canvas, same undo history.

---

## 5. Outstanding Bugs / Polish

- [ ] Grid line width should be configurable
- [ ] Resize still slightly janky with aspect ratio lock at certain angles
- [ ] View mode should show the final composited result (with adjustments when added)
- [ ] Straighten slider undo — verify `commitStraighten` works correctly with new state model
- [ ] Test all features after 90° + 180° + 270° rotations
- [ ] Verify `stateToImageCropRect` conversion accuracy for proto output

---

## 6. Terminal Setup

- Run `/terminal-setup` outside tmux on the Mac to get Shift+Enter working for multi-line input in Claude Code
- iTerm2 natively supports Shift+Enter — just needs the terminal setup command run once
