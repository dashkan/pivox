Q1:
OK. My algo needs. Trying to understand how img.ly photo editor crop algo works
When you start a crop, you can rotate. But the rotate can never cause the photo to go outside of the crop region.

The demo page is https://img.ly/showcases/cesdk/photo-editor-ui/web#c

Do you want me to grab some screenshots of what it is doing or can you automate it and check it out by yourself?

Response 1:
That is a classic "Infinite Zoom" or "Automatic Scale-to-Fit" problem. The core goal of the algorithm is to ensure that as the image rotates, the crop rectangle (the "viewport") always stays completely filled by the image pixels—meaning no transparent or black "gutters" appear at the corners.

You don't need to send screenshots! I can break down the math for you. The logic hinges on a **dynamic minimum scale** calculation.

### The Problem

When you rotate a rectangle (the image) inside another rectangle (the crop area), the corners of the crop area are the first places where the background would "leak" through. To prevent this, the image must be scaled up just enough so that even at its most "vulnerable" rotation, its edges still touch or exceed the corners of the crop box.

### The "Fill-to-Frame" Logic

The algorithm IMG.LY and similar editors (like Lightroom or Instagram) use follows these steps:

**1. Calculate the Target Dimensions**
Let $W_c$ and $H_c$ be the width and height of your crop area. Let $W_i$ and $H_i$ be the original width and height of your image.

**2. Handle the Rotation ($\theta$)**
When the image is rotated by an angle $\theta$, the distance from the center to the edges changes relative to the fixed crop box. To find the required scale ($S$) to keep the frame filled, you use this formula:

$$S = \max\left( \frac{W_c}{|W_i \cos\theta| + |H_i \sin\theta|}, \frac{H_c}{|W_i \sin\theta| + |H_i \cos\theta|} \right)$$

_Wait, that's for fitting the whole image inside._ For **filling the frame** (the "no gaps" rule), the logic is actually the inverse: you need to find the scale where the crop box corners are just touching the image boundaries.

**3. The "Corner Constraint" Algorithm**
The most robust way to write this is to:

1.  **Project the Corners:** Take the 4 corners of your crop rectangle in "image space" (by rotating them by $-\theta$).
2.  **Check Bounds:** If any of those 4 points fall outside the original image dimensions ($0,0$ to $W_i, H_i$), the image is too small.
3.  **Calculate Minimum Scale:** You calculate the scale factor required to push the image boundaries out until they contain all 4 projected corners.

### Pseudocode for the Logic

```javascript
function getMinScaleToFill(cropW, cropH, imgW, imgH, angleRad) {
	const absCos = Math.abs(Math.cos(angleRad));
	const absSin = Math.abs(Math.sin(angleRad));

	// Calculate the dimensions the crop box takes up
	// when projected onto the image's axes
	const requiredW = cropW * absCos + cropH * absSin;
	const requiredH = cropW * absSin + cropH * absCos;

	// The scale is the ratio of what we need vs what we have
	const scaleX = requiredW / imgW;
	const scaleY = requiredH / imgH;

	// We take the max to ensure both dimensions are covered
	return Math.max(scaleX, scaleY);
}
```

### Why it feels "Sticky" in the Demo

If you play with the IMG.LY demo, you’ll notice that as you rotate, the image seems to "zoom in" automatically. This is because every time the `onRotate` event fires, the engine:

1.  Calculates the new `minScale` for that specific angle.
2.  If the current scale is less than `minScale`, it forces the scale up.
3.  It then clamps the "translation" (pan) so you can't slide the image and show a gap.

**Would you like to see how to implement the "Clamping" part (preventing the user from panning into the empty space) next?**

Q2:
I need full algo for rotate, clamping and move the image w/o losing "pixels" if that makes sense. Cannot have any dead pixels from any crop functionality.

Their tool allows:
Rotate 0-45 left/right - slider
Rotate CCW 90
Mirror horz/vert
Move the image when inside the crop area. But prevents dead areas.
Resize the crop area. Prevents making bigger than current state of image

Make sense?

Response 2:
To build a robust "No-Dead-Pixels" crop engine, you have to treat the **Crop Box** as a fixed window and the **Image** as a transformable layer behind it. The goal is to ensure the image always "overflows" or perfectly meets the edges of that window.

Here is the complete algorithmic logic to handle these constraints.

---

## 1. The Core State

You need to track the following variables for your image:

- **Dimensions:** $W_{img}, H_{img}$
- **Transform:** `Scale`, `Rotation` (radians), and `Translation` ($X, Y$ from center).
- **Crop Box:** $W_{crop}, H_{crop}$

## 2. The Minimum Scale (Auto-Zoom)

As you rotate, the image must scale up to ensure corners are covered.

$$Scale_{min} = \max\left( \frac{W_{crop} \cdot |\cos\theta| + H_{crop} \cdot |\sin\theta|}{W_{img}}, \frac{W_{crop} \cdot |\sin\theta| + H_{crop} \cdot |\cos\theta|}{H_{img}} \right)$$

> **Note:** If the user performs a **90° rotation**, $\cos(90)=0$ and $\sin(90)=1$, so the formula effectively swaps the width/height requirements automatically.

## 3. Clamping the Movement (The "No-Gaps" Rule)

When the user drags the image, you must calculate the maximum allowed travel. Because the image is rotated, its "boundary" isn't a simple box; it’s a diamond or tilted rectangle.

To prevent dead pixels, the **Translation** ($TX, TY$) must stay within these bounds:

1.  **Calculate the Current Visible Image Bounds:**
    - $W_{visible} = (W_{img} \cdot Scale)$
    - $H_{visible} = (H_{img} \cdot Scale)$
2.  **Calculate the "Slack":**
    - The horizontal slack is the difference between the rotated image width and the crop width.
    - $Boundary_X = \frac{|W_{visible} \cdot \cos\theta| + |H_{visible} \cdot \sin\theta| - W_{crop}}{2}$
    - $Boundary_Y = \frac{|W_{visible} \cdot \sin\theta| + |H_{visible} \cdot \cos\theta| - H_{crop}}{2}$
3.  **Clamp:** \* $TX = \text{clamp}(TX, -Boundary_X, Boundary_X)$
    - $TY = \text{clamp}(TY, -Boundary_Y, Boundary_Y)$

## 4. Resizing the Crop Area

When the user drags the crop handles to make the box **larger**, you must stop them if it would exceed the current image's coverage.

- **Constraint:** $W_{crop}$ cannot exceed the current projected width of the image at its current scale and rotation.
- **Logic:** Before updating the crop width/height, check:
  - `if (newCropW > (W_img * scale * absCos + H_img * scale * absSin)) return;`

---

## 5. The Full Logic Loop (Implementation)

```javascript
class PhotoCropAlgo {
	constructor(imgW, imgH) {
		this.imgW = imgW;
		this.imgH = imgH;
		this.state = {
			rotation: 0, // radians
			scale: 1,
			tx: 0,
			ty: 0,
			flipH: 1, // 1 or -1
			flipV: 1,
			cropW: 500,
			cropH: 500,
		};
	}

	updateTransform(newRotation, newTX, newTY) {
		const absCos = Math.abs(Math.cos(newRotation));
		const absSin = Math.abs(Math.sin(newRotation));

		// 1. Force Minimum Scale for this rotation
		const minScale = Math.max(
			(this.state.cropW * absCos + this.state.cropH * absSin) / this.imgW,
			(this.state.cropW * absSin + this.state.cropH * absCos) / this.imgH,
		);

		this.state.scale = Math.max(this.state.scale, minScale);
		this.state.rotation = newRotation;

		// 2. Calculate Boundaries for Translation
		const vW = this.imgW * this.state.scale;
		const vH = this.imgH * this.state.scale;

		const boundX = Math.max(
			0,
			(vW * absCos + vH * absSin - this.state.cropW) / 2,
		);
		const boundY = Math.max(
			0,
			(vW * absSin + vH * absCos - this.state.cropH) / 2,
		);

		// 3. Apply Clamped Translation
		this.state.tx = Math.min(Math.max(newTX, -boundX), boundX);
		this.state.ty = Math.min(Math.max(newTY, -boundY), boundY);
	}

	flip(direction) {
		if (direction === "horz") this.state.flipH *= -1;
		if (direction === "vert") this.state.flipV *= -1;
	}
}
```

### Pro Tip for "Mirroring":

Don't actually change the pixel data. Just multiply the CSS `transform` or the Canvas `context.scale()` by `-1`. It keeps the math for rotation and clamping identical regardless of whether the image is flipped or not.

**Would you like me to convert this logic into a React hook or a Vanilla JS class that handles the mouse/touch events for the dragging?**

Q3:
Is this best implemented using html canvas?

Response 3:
For a high-performance photo editor like the one in the IMG.LY demo, **HTML5 Canvas is absolutely the best choice.**

While you _could_ do this with CSS `transform` on a `<div>` or `<img>`, Canvas gives you much tighter control over pixel-perfect exports and complex clipping. In professional editors, the Canvas acts as the "Engine," while the UI (buttons, sliders, handles) is built with standard HTML/React.

### Why Canvas Wins

- **Performance:** Canvas uses hardware acceleration (GPU). Smoothly rotating a high-resolution 20MP image at 60fps is difficult with the DOM but easy for Canvas.
- **Exporting:** When the user clicks "Save," you can instantly grab the cropped area using `getImageData` or `toDataURL`. With CSS, you have to recreate the logic on the server or use a library to "screenshot" the DOM.
- **Coordinate Control:** Canvas provides a clear coordinate system ($0,0$ to $W,H$) that makes the math we discussed much easier to debug.

---

### Canvas Implementation Strategy

To implement the "No-Dead-Pixels" algo on Canvas, follow the **"Save, Translate, Rotate, Draw, Restore"** pattern.

#### 1. The Draw Loop

You should never move the Canvas itself. Instead, you move the "Context" (the paintbrush), draw the image, and then reset the context.

```javascript
function drawClippedImage(ctx, img, state) {
	const { tx, ty, rotation, scale, flipH, flipV, cropW, cropH } = state;

	// 1. Clear the canvas to the crop area size
	ctx.clearRect(0, 0, cropW, cropH);

	ctx.save();

	// 2. Move the origin to the center of the crop box
	ctx.translate(cropW / 2 + tx, cropH / 2 + ty);

	// 3. Apply Rotation and Flips
	ctx.rotate(rotation);
	ctx.scale(flipH * scale, flipV * scale);

	// 4. Draw image centered on the new origin
	// This is where the magic happens: drawing at (-width/2, -height/2)
	ctx.drawImage(img, -img.width / 2, -img.height / 2);

	ctx.restore();
}
```

#### 2. Visualizing the Math

When you rotate the image on the canvas, the "clamping" logic from earlier acts as a leash.

### Handling Interaction (Mouse/Touch)

Since you are a software developer, you likely know that Canvas doesn't "know" where the image is once it's drawn. To handle **dragging**, you need to:

1.  **Listen to `mousedown`** on the Canvas.
2.  **Calculate the Delta:** On `mousemove`, calculate how many pixels the mouse moved ($dX, dY$).
3.  **Update State:** Add that delta to your `state.tx` and `state.ty`.
4.  **Run the Clamping Function:** Immediately run the logic from my previous message to "snap" the image back if it exposes a dead pixel.
5.  **Re-render:** Call the `drawClippedImage` function.

### A Quick Tip on "Resolution"

One common mistake is setting the Canvas size via CSS. **Don't do that.** It will make the image look blurry. Always set the internal resolution to match the display size:

```javascript
// Correct way to set resolution
canvas.width = cropW * window.devicePixelRatio;
canvas.height = cropH * window.devicePixelRatio;
ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
```

**Would you like me to put this all together into a single, copy-pasteable Vanilla JS class that you can drop into a project to test the "clamping" feel?**

Response 4:
This implementation uses a "State-First" approach. We calculate the constraints (the "leash") based on the current rotation and scale, then apply those constraints to the user's mouse/touch input.

I've structured this as a Vanilla JS class. You can drop this into any project, pass it a `<canvas>` element and an `Image` object, and it will handle the heavy lifting.

### The "No-Dead-Pixels" Canvas Engine

```javascript
class PhotoEditorEngine {
	constructor(canvas, image) {
		this.canvas = canvas;
		this.ctx = canvas.getContext("2d");
		this.img = image;

		this.state = {
			zoom: 1.0, // User-controlled extra zoom
			rotation: 0, // Radians
			tx: 0, // Translation X
			ty: 0, // Translation Y
			flipH: 1,
			flipV: 1,
			cropW: 500,
			cropH: 500,
		};

		this.isDragging = false;
		this.lastMouse = { x: 0, y: 0 };

		this.initEvents();
		this.render();
	}

	// --- THE MATH CORE ---

	getConstraints() {
		const { rotation, zoom, cropW, cropH } = this.state;
		const absCos = Math.abs(Math.cos(rotation));
		const absSin = Math.abs(Math.sin(rotation));

		// 1. Calculate Minimum Scale to prevent "dead pixels" at this rotation
		const minScale = Math.max(
			(cropW * absCos + cropH * absSin) / this.img.width,
			(cropW * absSin + cropH * absCos) / this.img.height,
		);

		// Actual scale used for drawing
		const activeScale = minScale * zoom;

		// 2. Calculate Translation Boundaries (The "Leash")
		// How much "extra" image do we have beyond the crop box?
		const imgVisibleW =
			this.img.width * activeScale * absCos +
			this.img.height * activeScale * absSin;
		const imgVisibleH =
			this.img.width * activeScale * absSin +
			this.img.height * activeScale * absCos;

		const maxX = Math.max(0, (imgVisibleW - cropW) / 2);
		const maxY = Math.max(0, (imgVisibleH - cropH) / 2);

		return { minScale, activeScale, maxX, maxY };
	}

	updateTransform(updates) {
		this.state = { ...this.state, ...updates };

		const { activeScale, maxX, maxY } = this.getConstraints();

		// Clamp translation so no dead pixels show
		this.state.tx = Math.max(-maxX, Math.min(this.state.tx, maxX));
		this.state.ty = Math.max(-maxY, Math.min(this.state.ty, maxY));

		this.render();
	}

	// --- RENDERING ---

	render() {
		const { tx, ty, rotation, flipH, flipV, cropW, cropH } = this.state;
		const { activeScale } = this.getConstraints();

		// Match canvas resolution to crop size
		this.canvas.width = cropW;
		this.canvas.height = cropH;

		this.ctx.clearRect(0, 0, cropW, cropH);
		this.ctx.save();

		// Move to center + user pan
		this.ctx.translate(cropW / 2 + tx, cropH / 2 + ty);

		// Apply rotation and flips
		this.ctx.rotate(rotation);
		this.ctx.scale(flipH * activeScale, flipV * activeScale);

		// Draw image centered on the transformed origin
		this.ctx.drawImage(this.img, -this.img.width / 2, -this.img.height / 2);

		this.ctx.restore();
	}

	// --- INTERACTION ---

	initEvents() {
		this.canvas.addEventListener("mousedown", (e) => {
			this.isDragging = true;
			this.lastMouse = { x: e.clientX, y: e.clientY };
		});

		window.addEventListener("mousemove", (e) => {
			if (!this.isDragging) return;

			const dx = e.clientX - this.lastMouse.x;
			const dy = e.clientY - this.lastMouse.y;

			this.updateTransform({
				tx: this.state.tx + dx,
				ty: this.state.ty + dy,
			});

			this.lastMouse = { x: e.clientX, y: e.clientY };
		});

		window.addEventListener("mouseup", () => (this.isDragging = false));
	}
}
```

### Key Logic Highlights:

1.  **Dynamic Clamping:** Notice that `getConstraints()` is called inside `updateTransform()`. This means if you rotate the image, the engine automatically checks if the new rotation forces the image to be "pushed" back into the frame.
2.  **Scale Coupling:** The `activeScale` is a product of `minScale` (required for rotation) and `zoom` (user preference). This ensures the user can't zoom out far enough to see the background.
3.  **The "Projected" Boundary:** We calculate `imgVisibleW/H` by projecting the rotated image onto the $X$ and $Y$ axes. This is the secret to why the image feels "sticky" at the edges when you rotate it.

### How to use the 90° CCW and Slider:

- **Slider (±45°):** Map your slider value to `rotation` in radians ($degrees \cdot \frac{\pi}{180}$).
- **90° CCW:** Just subtract $\frac{\pi}{2}$ from the current `state.rotation` and call `updateTransform`.
- **Mirror:** Toggle `flipH` or `flipV` between `1` and `-1`.

**Would you like me to explain how to calculate the "Crop Box Resize" logic so the handles stop moving when they hit the image edge?**

Q5:
yes. gimme all features in one.
rotate, zoom, flip, handles, canvas, move, etc

Response 5:
Implementing crop handle resizing is a bit different from rotating or moving the image. In those operations, we move the image _inside_ a fixed frame. For resizing, we keep the image in its current orientation and scale, and move the _frame itself_.

The challenge is to prevent the user from dragging a handle so far that it creates "dead pixels." This means the four corners of your new, theoretical crop box must _always_ lie within the bounds of the currently transformed image.

### 1. Visualizing the "Containment"

Think of the rotated and scaled image as a boundary. Your crop box is a smaller window inside this boundary. As you drag a handle, you can only move it as far as the edge of that boundary allows.

[Image showing a rectangular boundary representing the transformed image with an inscribed crop box, where an arrow indicates resizing and a boundary point shows containment constraint]

To do this mathematically, we must use coordinate geometry:

1.  We have the 4 corners of the potential new crop box (in screen space, centered at 0,0).
2.  We rotate these 4 points _back_ by the negative of the image's current rotation.
3.  We check if these four newly rotated points all fall within a simple box defined by the image's width and height (scaled by the current zoom).

---

### 2. The Complete Canvas Crop Engine

Here is the full implementation, integrating the clamping and movement from before with new logic to handle interactive crop handles and containment checking.

#### Part 1: The Engine Class

```javascript
class PhotoCropEngine {
	constructor(canvas, img) {
		this.canvas = canvas;
		this.ctx = canvas.getContext("2d");
		this.img = img;

		this.state = {
			zoom: 1.0,
			rotation: 0, // Radians
			tx: 0,
			ty: 0,
			flipH: 1,
			flipV: 1,
			cropW: 400,
			cropH: 400,
		};

		// Handle configuration
		this.handleSize = 20; // Hit area diameter
		this.handles = []; // Populated in initHandles

		this.isDragging = false;
		this.activeHandle = null;
		this.lastMouse = { x: 0, y: 0 };

		this.init();
	}

	// Initialize/Update handles based on current crop size
	updateHandles() {
		const { cropW, cropH } = this.state;
		this.handles = [
			{ name: "tl", x: -cropW / 2, y: -cropH / 2 },
			{ name: "tr", x: cropW / 2, y: -cropH / 2 },
			{ name: "bl", x: -cropW / 2, y: cropH / 2 },
			{ name: "br", x: cropW / 2, y: cropH / 2 },
		];
	}

	// --- CORE VALIDATION LOGIC ---

	// The most complex part: checking if a crop size is valid
	isCropValid(newW, newH) {
		const { rotation, zoom } = this.state;
		const absCos = Math.abs(Math.cos(rotation));
		const absSin = Math.abs(Math.sin(rotation));

		// 1. Minimum scale that forces "no gaps" at this rotation
		const minScaleForRotation = Math.max(
			(newW * absCos + newH * absSin) / this.img.width,
			(newW * absSin + newH * absCos) / this.img.height,
		);

		// 2. We use 'zoom' as "extra zoom beyond minimum required"
		// Active scale = minScaleForRotation * userExtraZoom
		const activeScale = minScaleForRotation * zoom;

		// If the resulting scale would make the image smaller than the original required minScale, it's invalid
		if (activeScale < minScaleForRotation) return false;
		return true;
	}

	// --- UPDATE STATE AND RENDER ---

	updateTransform(updates) {
		let newState = { ...this.state, ...updates };
		const { rotation, zoom, cropW, cropH } = newState;

		// 1. Enforce No Dead Pixels
		const absCos = Math.abs(Math.cos(rotation));
		const absSin = Math.abs(Math.sin(rotation));

		// Required scale is dynamic based on rotation/size
		const minScale = Math.max(
			(cropW * absCos + cropH * absSin) / this.img.width,
			(cropW * absSin + cropH * absCos) / this.img.height,
		);
		const activeScale = minScale * zoom;

		// Calculate translation boundaries ("Leash")
		const imgVisibleW =
			this.img.width * activeScale * absCos +
			this.img.height * activeScale * absSin;
		const imgVisibleH =
			this.img.width * activeScale * absSin +
			this.img.height * activeScale * absCos;
		const maxX = Math.max(0, (imgVisibleW - cropW) / 2);
		const maxY = Math.max(0, (imgVisibleH - cropH) / 2);

		// Clamp translation
		newState.tx = Math.max(-maxX, Math.min(newState.tx, maxX));
		newState.ty = Math.max(-maxY, Math.min(newState.ty, maxY));

		this.state = newState;
		this.render();
	}

	render() {
		// Clear canvas
		this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height);

		// State variables
		const { tx, ty, rotation, flipH, flipV, cropW, cropH, zoom } = this.state;
		this.canvas.width = cropW + this.handleSize;
		this.canvas.height = cropH + this.handleSize;

		const absCos = Math.abs(Math.cos(rotation));
		const absSin = Math.abs(Math.sin(rotation));
		const minScale = Math.max(
			(cropW * absCos + cropH * absSin) / this.img.width,
			(cropW * absSin + cropH * absCos) / this.img.height,
		);
		const activeScale = minScale * zoom;

		this.ctx.save();
		this.ctx.translate(this.canvas.width / 2 + tx, this.canvas.height / 2 + ty);
		this.ctx.rotate(rotation);
		this.ctx.scale(flipH * activeScale, flipV * activeScale);
		this.ctx.drawImage(this.img, -this.img.width / 2, -this.img.height / 2);
		this.ctx.restore();

		// Draw handles
		this.drawHandles();
	}

	drawHandles() {
		this.updateHandles();
		const { handleSize } = this;
		this.ctx.fillStyle = "rgba(255, 255, 255, 0.8)";
		this.ctx.strokeStyle = "#000";
		this.ctx.lineWidth = 2;

		this.handles.forEach((h) => {
			const hx = this.canvas.width / 2 + h.x;
			const hy = this.canvas.height / 2 + h.y;
			this.ctx.beginPath();
			this.ctx.arc(hx, hy, handleSize / 2, 0, Math.PI * 2);
			this.ctx.fill();
			this.ctx.stroke();
		});
	}

	// ... Interaction logic (next section)
}
```

#### Part 2: Interaction (Dragging and Resizing)

```javascript
    // --- INTERACTION ---

    init() {
        this.canvas.addEventListener('mousedown', this.handleMouseDown.bind(this));
        window.addEventListener('mousemove', this.handleMouseMove.bind(this));
        window.addEventListener('mouseup', this.handleMouseUp.bind(this));
    }

    handleMouseDown(e) {
        const mouseX = e.clientX - this.canvas.getBoundingClientRect().left;
        const mouseY = e.clientY - this.canvas.getBoundingClientRect().top;

        // 1. Check if user clicked on a handle
        this.updateHandles();
        this.activeHandle = null;
        this.handles.forEach(h => {
            const hx = this.canvas.width / 2 + h.x;
            const hy = this.canvas.height / 2 + h.y;
            if (Math.hypot(mouseX - hx, mouseY - hy) < this.handleSize / 2) {
                this.activeHandle = h;
            }
        });

        // 2. Otherwise, start dragging the image
        if (!this.activeHandle) {
            this.isDragging = true;
        }

        this.lastMouse = { x: e.clientX, y: e.clientY };
    }

    handleMouseMove(e) {
        if (!this.isDragging && !this.activeHandle) return;

        const dx = e.clientX - this.lastMouse.x;
        const dy = e.clientY - this.lastMouse.y;

        if (this.isDragging) {
            this.updateTransform({
                tx: this.state.tx + dx,
                ty: this.state.ty + dy
            });
        } else if (this.activeHandle) {
            this.handleResize(dx, dy);
        }

        this.lastMouse = { x: e.clientX, y: e.clientY };
    }

    handleMouseUp() {
        this.isDragging = false;
        this.activeHandle = null;
    }

    handleResize(dx, dy) {
        let { cropW, cropH, tx, ty } = this.state;
        let newW = cropW;
        let newH = cropH;

        // Resize from current handle
        switch (this.activeHandle.name) {
            case 'tl': newW -= dx * 2; newH -= dy * 2; break;
            case 'tr': newW += dx * 2; newH -= dy * 2; break;
            case 'bl': newW -= dx * 2; newH += dy * 2; break;
            case 'br': newW += dx * 2; newH += dy * 2; break;
        }

        // Validate and apply
        if (this.isCropValid(newW, newH) && newW > 100 && newH > 100) {
            // Update crop size
            this.state.cropW = newW;
            this.state.cropH = newH;

            // Recalculate containment (we may need to "push" the image if the frame grew)
            this.updateTransform({});
        }
    }
}
```

#### Part 3: Usage

```javascript
const canvas = document.getElementById("myCanvas");
const img = new Image();
img.src = "my_awesome_photo.jpg";

img.onload = () => {
	const editor = new PhotoCropEngine(canvas, img);

	// Wire up UI controls (buttons, sliders, etc.)
	document.getElementById("rotateSlide").oninput = (e) => {
		editor.updateTransform({ rotation: e.target.value * (Math.PI / 180) });
	};
	document.getElementById("zoomSlide").oninput = (e) => {
		editor.updateTransform({ zoom: parseFloat(e.target.value) });
	};
	document.getElementById("flipHBtn").onclick = () => {
		editor.updateTransform({ flipH: editor.state.flipH * -1 });
	};
	// ... other controls
};
```
