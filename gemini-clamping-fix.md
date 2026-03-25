# Gemini: Fix Translation Clamping Formula

## Context

You previously gave me the "No Dead Pixels" crop algorithm with this translation clamping formula:

```javascript
// Bounding box projection
const imgVisibleW = imgW * scale * absCos + imgH * scale * absSin;
const imgVisibleH = imgW * scale * absSin + imgH * scale * absCos;

const maxTx = Math.max(0, (imgVisibleW - cropW) / 2);
const maxTy = Math.max(0, (imgVisibleH - cropH) / 2);
```

This works well for small angles but **breaks at the corners** for larger angles combined with translation.

## The Problem

The bounding-box projection overestimates the available translation space because it uses the **outer bounding box** of the rotated image, not the **actual rotated rectangle edges**.

After rotating by θ and moving the image to the boundary allowed by maxTx/maxTy, the **CORNERS of the crop rect can poke through** the actual rotated image edge — creating dead pixels at the corners.

### Visual Example

```
        ┌──────────────────────┐  ← Bounding box (what formula uses)
       /                      /
      /   ┌─────────────┐   /     ← Crop rect at max tx
     /    │  DEAD PIXEL →│● /      ← Corner exits rotated image
    /     │              │ /
   /      └─────────────┘/
  /______________________/         ← Actual rotated image edge
```

The formula says maxTx allows this position, but the crop rect corner is OUTSIDE the rotated image (between the bounding box edge and the actual rotated edge).

### Concrete Example

- Image: 1920×2658
- Rotation: 15°
- Crop rect: 1920×2658 (full image)
- minScale correctly computed ≈ 1.08
- maxTx from bounding box ≈ 50px
- At tx=50, the top-right corner of the crop rect exits the rotated image diamond → dead pixels

## What I Need

Update the `computeTranslationBounds` function so that at **any** `(tx, ty)` within `[-maxTx, maxTx] × [-maxTy, maxTy]`, **ALL 4 corners** of the crop rect remain inside the **actual rotated+scaled image rectangle** (not just the bounding box).

### The Constraint

Each crop rect corner at `(tx ± cropW/2, ty ± cropH/2)`, when inverse-rotated by `-θ`, must fall within the unrotated image bounds `[-imgW*scale/2, imgW*scale/2] × [-imgH*scale/2, imgH*scale/2]`.

Mathematically, for each sign combination `(sx, sy)` ∈ {±1, ±1}:

```
|cos(θ) × (tx + sx × cropW/2) + sin(θ) × (ty + sy × cropH/2)| ≤ imgW × scale / 2
|−sin(θ) × (tx + sx × cropW/2) + cos(θ) × (ty + sy × cropH/2)| ≤ imgH × scale / 2
```

### What I want

Give me the corrected closed-form formula for `maxTx` and `maxTy` that satisfies the above constraint for all 4 corners simultaneously.

The formula should:
1. Work for all angles 0° to 360° (including 90° multiples)
2. Be computable without iteration (closed-form, no binary search)
3. Assume `tx` and `ty` are independent (rectangular bounds, not coupled)
4. Be conservative — if the exact solution requires coupling tx and ty, give me a safe rectangular approximation

## Current Implementation

```javascript
function computeTranslationBounds(cropW, cropH, imgW, imgH, scale, angleRad) {
    const absCos = Math.abs(Math.cos(angleRad));
    const absSin = Math.abs(Math.sin(angleRad));

    const imgVisibleW = imgW * scale * absCos + imgH * scale * absSin;
    const imgVisibleH = imgW * scale * absSin + imgH * scale * absCos;

    const maxTx = Math.max(0, (imgVisibleW - cropW) / 2);
    const maxTy = Math.max(0, (imgVisibleH - cropH) / 2);

    return { maxTx, maxTy };
}
```

Give me the corrected version of this function.
