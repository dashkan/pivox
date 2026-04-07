import SwiftUI
import AppKit

// MARK: - Observable wrapper around ImageEditorBridge

@Observable
class ImageEditModel {
    let bridge: ImageEditorBridge
    private(set) var state: IEBState
    var isEditing = false
    var flipHCount = 0  // increments on each horizontal flip (for animation trigger)
    var flipVCount = 0  // increments on each vertical flip

    init() {
        bridge = ImageEditorBridge()
        state = bridge.currentState()!
        bridge.onStateChanged = { [weak self] in
            guard let self, let s = self.bridge.currentState() else { return }
            self.state = s
        }
    }

    func loadImage(_ image: NSImage) {
        guard let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else { return }
        bridge.setImageLoadedWidth(Int32(cgImage.width), height: Int32(cgImage.height))
    }

    func enterEditMode() {
        isEditing = true
        bridge.enterCropMode()
    }

    func exitEditMode() {
        isEditing = false
        bridge.exitCropMode()
    }
}

// MARK: - Main ImageEditView (Photos-style)

struct ImageEditView: View {
    let image: NSImage
    @Binding var isEditing: Bool
    @Binding var sidebarVisibility: NavigationSplitViewVisibility
    var onDone: ((IEBCropRect) -> Void)?
    var onCancel: (() -> Void)?

    @State private var model = ImageEditModel()

    var body: some View {
        ImageEditCanvasContainer(model: model, image: image)
            .overlay(alignment: .trailing) {
                // Right tool panel — overlays canvas, no extra width
                if model.isEditing {
                    CropToolPanel(model: model)
                        .frame(width: 260)
                        .background(.background)
                        .transition(.move(edge: .trailing))
                }
            }
            .animation(.easeInOut(duration: 0.25), value: model.isEditing)
        .toolbar {
            if model.isEditing {
                editingToolbar
            } else {
                viewingToolbar
            }
        }
        .onAppear {
            model.loadImage(image)
        }
        .accessibilityIdentifier("image-edit-view")
    }

    // ── View Mode Toolbar ────────────────────────────────────────────
    // [← Back]   [- ●─── +]                              [Edit]

    @ToolbarContentBuilder
    private var viewingToolbar: some ToolbarContent {
        ToolbarItem(placement: .navigation) {
            Button(action: { onCancel?() }) {
                Image(systemName: "chevron.left")
            }
            .help("Back")
            .accessibilityIdentifier("edit-back")
        }

        ToolbarItem(placement: .automatic) {
            zoomControls
        }

        ToolbarItem(placement: .primaryAction) {
            Button("Edit") {
                withAnimation {
                    model.enterEditMode()
                    isEditing = true
                    sidebarVisibility = .detailOnly
                }
            }
            .accessibilityIdentifier("edit-enter")
        }
    }

    // ── Edit Mode Toolbar ────────────────────────────────────────────
    // [Revert]   [- ●─── +]     [Crop]                   [Done]

    @ToolbarContentBuilder
    private var editingToolbar: some ToolbarContent {
        ToolbarItem(placement: .navigation) {
            Button("Revert to Original") {
                model.bridge.reset()
            }
            .disabled(!model.state.isDirty)
            .accessibilityIdentifier("edit-revert")
        }

        ToolbarItem(placement: .automatic) {
            zoomControls
        }

        ToolbarItem(placement: .principal) {
            // Tool selector — only Crop for now, expandable later
            HStack(spacing: 2) {
                toolTabButton(label: "Crop", icon: "crop", isSelected: true)
            }
        }

        ToolbarItem(placement: .primaryAction) {
            Button("Done") {
                let cropRect = model.bridge.getCropRect()!
                // Exit edit mode first (reverts dark theme + hides panel)
                // before onDone destroys this view.
                withAnimation {
                    model.exitEditMode()
                    isEditing = false
                    sidebarVisibility = .automatic
                }
                onDone?(cropRect)
            }
            .buttonStyle(.borderedProminent)
            .tint(.yellow)
            .foregroundStyle(.black)
            .accessibilityIdentifier("edit-done")
        }
    }

    // ── Zoom controls (Photos-style: slider inside rounded track) ───

    private var zoomControls: some View {
        HStack(spacing: 6) {
            Image(systemName: "minus")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .onTapGesture { model.bridge.zoomOut() }

            Slider(value: Binding(
                get: { model.state.zoom },
                set: { model.bridge.setZoom($0) }
            ), in: 100...800)
            .frame(width: 100)
            .controlSize(.small)
            .tint(.secondary)

            Image(systemName: "plus")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .onTapGesture { model.bridge.zoomIn() }
        }
        .accessibilityIdentifier("edit-zoom")
    }

    private func toolTabButton(label: String, icon: String, isSelected: Bool) -> some View {
        Button(action: {}) {
            Text(label)
                .font(.subheadline)
                .fontWeight(isSelected ? .semibold : .regular)
        }
        .buttonStyle(.borderless)
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(isSelected ? AnyShapeStyle(.primary.opacity(0.15)) : AnyShapeStyle(.clear), in: Capsule())
    }
}

// MARK: - Crop Tool Panel (right side, Photos-style)

struct CropToolPanel: View {
    let model: ImageEditModel

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            Text("CROP")
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 20)
                .padding(.top, 16)
                .padding(.bottom, 12)

            // Straighten
            RulerSliderRow(label: "Straighten", icon: "circle.and.line.horizontal.fill",
                      value: Binding(
                        get: { model.state.straighten },
                        set: { model.bridge.setStraighten($0) }
                      ),
                      range: -45...45,
                      onCommit: { model.bridge.commitStraighten() },
                      id: "edit-straighten")

            Divider().padding(.horizontal, 16).padding(.vertical, 8)

            // Flip
            VStack(alignment: .leading, spacing: 2) {
                CropToolButton(icon: "arrow.left.and.right.righttriangle.left.righttriangle.right",
                               label: "Flip Horizontally", id: "edit-flip-h") {
                    model.bridge.toggleFlipHorizontal()
                    model.flipHCount += 1
                }
                CropToolButton(icon: "arrow.up.and.down.righttriangle.up.righttriangle.down",
                               label: "Flip Vertically", id: "edit-flip-v") {
                    model.bridge.toggleFlipVertical()
                    model.flipVCount += 1
                }
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 4)

            Divider().padding(.horizontal, 16).padding(.vertical, 8)

            // Aspect ratio header
            CropToolButton(icon: "aspectratio", label: "Aspect", id: "edit-aspect") { }
                .padding(.horizontal, 20)
                .padding(.bottom, 4)

            if let templates = model.state.templates {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(templates.enumerated()), id: \.offset) { _, tmpl in
                        let isActive = model.state.activeTemplate?.label == tmpl.label
                        Button(action: {
                            if tmpl.isFreeform {
                                model.bridge.applyFreeformTemplate()
                            } else {
                                model.bridge.applyTemplate(withLabel: tmpl.label ?? "", ratio: tmpl.ratio)
                            }
                        }) {
                            HStack {
                                if isActive {
                                    Image(systemName: "checkmark")
                                        .font(.caption2)
                                        .frame(width: 16)
                                } else {
                                    Spacer().frame(width: 16)
                                }
                                Text(tmpl.label ?? "Free")
                                    .font(.subheadline)
                                Spacer()
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .padding(.vertical, 3)
                        .padding(.horizontal, 20)
                    }
                }
            }

            Spacer()

            // Bottom: Undo/Redo + Reset
            Divider().padding(.horizontal, 16)

            HStack {
                Button(action: { model.bridge.undo() }) {
                    Image(systemName: "arrow.uturn.backward")
                }
                .disabled(!model.state.canUndo)
                .help("Undo")
                .accessibilityIdentifier("edit-undo")

                Button(action: { model.bridge.redo() }) {
                    Image(systemName: "arrow.uturn.forward")
                }
                .disabled(!model.state.canRedo)
                .help("Redo")
                .accessibilityIdentifier("edit-redo")

                Spacer()

                Button("Reset") {
                    model.bridge.reset()
                }
                .disabled(!model.state.isDirty)
                .accessibilityIdentifier("edit-reset")
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 12)
        }
    }
}

/// Crop tool panel button — matches Photos icon style.
/// Icon turns white on mouse-down, returns to gray on release. Text color stays the same.
struct CropToolButton: View {
    let icon: String
    let label: String
    var id: String = ""
    let action: () -> Void

    @State private var isPressed = false

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: icon)
                .font(.system(size: 13))
                .foregroundStyle(isPressed ? .primary : .secondary)
                .frame(width: 18)
                .animation(.easeInOut(duration: 0.08), value: isPressed)
            Text(label)
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
        .onTapGesture {
            action()
        }
        .simultaneousGesture(
            DragGesture(minimumDistance: 0)
                .onChanged { _ in isPressed = true }
                .onEnded { _ in isPressed = false }
        )
        .accessibilityIdentifier(id)
    }
}

/// Photos-style straighten ruler control.
/// Unified dark bar with label inside. The fill IS the indicator (no separate handle).
/// Default: gray fill from center, center mark visible. Hover: ticks + blue fill, degrees turn white.
struct RulerSliderRow: View {
    let label: String
    let icon: String
    @Binding var value: Double
    let range: ClosedRange<Double>
    var onCommit: (() -> Void)?
    var id: String = ""

    @State private var isHovered = false
    @State private var isDragging = false

    private var isActive: Bool { isHovered || isDragging }

    /// Format degrees like Photos: shows "-0°" at zero.
    private var degreeText: String {
        if abs(value) < 0.5 { return "-0°" }
        return String(format: "%.0f°", value)
    }

    var body: some View {
        HStack(spacing: 8) {
            // Icon outside the bar
            Image(systemName: icon)
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
                .frame(width: 18)

            // Unified bar — label + ruler + degrees all inside
            GeometryReader { geo in
                let width = geo.size.width
                let height = geo.size.height
                let rangeSpan = range.upperBound - range.lowerBound
                let fraction = (value - range.lowerBound) / rangeSpan
                let centerFraction = (0 - range.lowerBound) / rangeSpan
                let indicatorX = fraction * width
                let centerX = centerFraction * width

                ZStack {
                    // Tick marks along top — only when active
                    if isActive {
                        Canvas { context, size in
                            let tickCount = 90
                            let spacing = size.width / Double(tickCount)
                            for i in 0...tickCount {
                                let x = Double(i) * spacing
                                let isMajor = i % 10 == 0
                                let tickH = isMajor ? 5.0 : 3.0
                                var path = Path()
                                path.move(to: CGPoint(x: x, y: 1))
                                path.addLine(to: CGPoint(x: x, y: 1 + tickH))
                                context.stroke(path, with: .color(.primary.opacity(isMajor ? 0.3 : 0.12)),
                                              lineWidth: 0.5)
                            }
                        }
                        .transition(.opacity)
                    }

                    // Center mark — always visible, subtle
                    Rectangle()
                        .fill(Color.primary.opacity(0.15))
                        .frame(width: 0.5, height: height * 0.45)
                        .position(x: centerX, y: height / 2)

                    // Fill from center to value
                    let fillLeft = min(centerX, indicatorX)
                    let fillW = abs(indicatorX - centerX)
                    if fillW > 0.5 {
                        Rectangle()
                            .fill(isActive ? Color.accentColor : Color.primary.opacity(0.15))
                            .frame(width: fillW, height: height)
                            .position(x: fillLeft + fillW / 2, y: height / 2)
                    }

                    // Label + degrees inside the bar
                    HStack {
                        Text(label)
                            .font(.system(size: 13))
                            .foregroundStyle(.secondary)
                        Spacer()
                        Text(degreeText)
                            .font(.system(size: 13))
                            .monospacedDigit()
                            .foregroundStyle(isActive ? .primary : .secondary)
                    }
                    .padding(.horizontal, 10)
                    .allowsHitTesting(false)
                }
                .contentShape(Rectangle())
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { drag in
                            isDragging = true
                            let frac = max(0, min(1, drag.location.x / width))
                            value = range.lowerBound + frac * rangeSpan
                            if abs(value) < 1.0 { value = 0 }
                        }
                        .onEnded { _ in
                            isDragging = false
                            onCommit?()
                        }
                )
                .onHover { hovering in
                    withAnimation(.easeInOut(duration: 0.12)) {
                        isHovered = hovering
                    }
                }
            }
            .frame(height: 22)
            .background(Color.primary.opacity(0.08), in: RoundedRectangle(cornerRadius: 4))
            .accessibilityIdentifier(id)
        }
        .padding(.horizontal, 20)
    }
}

// MARK: - Canvas Container (handles flip animation)

struct ImageEditCanvasContainer: View {
    let model: ImageEditModel
    let image: NSImage

    @State private var flipHAngle: Double = 0
    @State private var flipVAngle: Double = 0

    var body: some View {
        ZStack {
            Color(nsColor: .windowBackgroundColor).ignoresSafeArea()
            ImageEditCanvasView(model: model, image: image)
                .accessibilityIdentifier("image-edit-view")
                .rotation3DEffect(.degrees(flipHAngle), axis: (x: 0, y: 1, z: 0), perspective: 0)
                .rotation3DEffect(.degrees(flipVAngle), axis: (x: 1, y: 0, z: 0), perspective: 0)
        }
        .onChange(of: model.flipHCount) { _, _ in
            // Animate 0 → 90 (halfway), then snap to 0 (engine already flipped the image)
            withAnimation(.easeIn(duration: 0.15)) {
                flipHAngle = 90
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) {
                withAnimation(.easeOut(duration: 0.15)) {
                    flipHAngle = 0
                }
            }
        }
        .onChange(of: model.flipVCount) { _, _ in
            withAnimation(.easeIn(duration: 0.15)) {
                flipVAngle = 90
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) {
                withAnimation(.easeOut(duration: 0.15)) {
                    flipVAngle = 0
                }
            }
        }
    }
}

// MARK: - Canvas Renderer

struct ImageEditCanvasView: NSViewRepresentable {
    let model: ImageEditModel
    let image: NSImage

    func makeNSView(context: Context) -> ImageEditCanvasNSView {
        let view = ImageEditCanvasNSView()
        view.setAccessibilityElement(true)
        view.setAccessibilityRole(.image)
        view.setAccessibilityLabel("Image Editor")
        view.setAccessibilityIdentifier("image-edit-view")
        view.model = model
        view.sourceImage = image
        return view
    }

    func updateNSView(_ nsView: ImageEditCanvasNSView, context: Context) {
        nsView.model = model
        nsView.needsDisplay = true
    }
}

class ImageEditCanvasNSView: NSView {
    var model: ImageEditModel?
    var sourceImage: NSImage?

    private var trackingArea: NSTrackingArea?
    private var viewportScale: Double = 1
    private var viewportOffsetX: Double = 0
    private var viewportOffsetY: Double = 0

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        model?.bridge.setContainerWidth(bounds.width, height: bounds.height)
    }

    override func draw(_ dirtyRect: NSRect) {
        guard let ctx = NSGraphicsContext.current?.cgContext,
              let model = model,
              let sourceImage = sourceImage,
              let cgImage = sourceImage.cgImage(forProposedRect: nil, context: nil, hints: nil)
        else { return }

        let s = model.state
        let containerW = bounds.width
        let containerH = bounds.height

        let cw = s.cropWidth
        let ch = s.cropHeight
        guard cw > 0, ch > 0 else { return }

        // Canvas background — adapts to appearance
        ctx.setFillColor(NSColor.windowBackgroundColor.cgColor)
        ctx.fill(bounds)

        let pad = 24.0
        let drawW = containerW - pad * 2
        let drawH = containerH - pad * 2
        let effectiveZoom = s.isZoomFit ? 100.0 : s.zoom
        let vpScale = min(drawW / cw, drawH / ch) * (effectiveZoom / 100)
        let cropScreenW = cw * vpScale
        let cropScreenH = ch * vpScale
        let cropScreenX = (containerW - cropScreenW) / 2 + s.panOffsetX
        let cropScreenY = (containerH - cropScreenH) / 2 + s.panOffsetY

        viewportScale = vpScale
        viewportOffsetX = cropScreenX
        viewportOffsetY = cropScreenY
        model.bridge.setViewportScale(vpScale)

        let angle = Double(s.rotation) + s.straighten
        let angleRad = angle * .pi / 180

        let imgW = Double(cgImage.width)
        let imgH = Double(cgImage.height)
        let flipX: Double = s.flipHorizontal ? -1 : 1
        let flipY: Double = s.flipVertical ? -1 : 1
        let cgFlipY: Double = -1  // CGContext draws bottom-up in flipped view

        if s.isCropMode {
            // ── CROP MODE ──
            // 1. Draw image
            ctx.saveGState()
            ctx.translateBy(x: cropScreenX + cropScreenW / 2 + s.tx * vpScale,
                           y: cropScreenY + cropScreenH / 2 + s.ty * vpScale)
            ctx.rotate(by: angleRad)
            ctx.scaleBy(x: flipX * s.scale * vpScale,
                       y: flipY * s.scale * vpScale * cgFlipY)
            ctx.draw(cgImage, in: CGRect(x: -imgW / 2, y: -imgH / 2, width: imgW, height: imgH))
            ctx.restoreGState()

            // 2. Subtle dimmed overlay
            ctx.setFillColor(NSColor.windowBackgroundColor.withAlphaComponent(0.5).cgColor)
            ctx.fill(CGRect(x: 0, y: 0, width: containerW, height: cropScreenY))
            ctx.fill(CGRect(x: 0, y: cropScreenY + cropScreenH, width: containerW,
                           height: containerH - cropScreenY - cropScreenH))
            ctx.fill(CGRect(x: 0, y: cropScreenY, width: cropScreenX, height: cropScreenH))
            ctx.fill(CGRect(x: cropScreenX + cropScreenW, y: cropScreenY,
                           width: containerW - cropScreenX - cropScreenW, height: cropScreenH))

            // 3. Controls in crop viewport space
            ctx.saveGState()
            ctx.translateBy(x: cropScreenX, y: cropScreenY)
            ctx.scaleBy(x: vpScale, y: vpScale)
            drawControls(ctx: ctx, cw: cw, ch: ch, vpScale: vpScale,
                         isDragging: s.isDragging, straighten: s.straighten)
            ctx.restoreGState()

        } else {
            // ── VIEW MODE ──
            ctx.saveGState()
            ctx.clip(to: CGRect(x: cropScreenX, y: cropScreenY,
                               width: cropScreenW, height: cropScreenH))
            ctx.translateBy(x: cropScreenX + cropScreenW / 2 + s.tx * vpScale,
                           y: cropScreenY + cropScreenH / 2 + s.ty * vpScale)
            ctx.rotate(by: angleRad)
            ctx.scaleBy(x: flipX * s.scale * vpScale,
                       y: flipY * s.scale * vpScale * cgFlipY)
            ctx.draw(cgImage, in: CGRect(x: -imgW / 2, y: -imgH / 2, width: imgW, height: imgH))
            ctx.restoreGState()
        }
    }

    // ── Controls (crop-space coordinates, white like Photos) ─────────

    private func drawControls(ctx: CGContext, cw: Double, ch: Double,
                               vpScale: Double, isDragging: Bool, straighten: Double) {
        let white = NSColor.white.cgColor
        let gridColor = NSColor.white.withAlphaComponent(0.25).cgColor

        // Thin white crop border
        ctx.setStrokeColor(white)
        ctx.setLineWidth(1.0 / vpScale)
        ctx.stroke(CGRect(x: 0, y: 0, width: cw, height: ch))

        // Grid — show during drag or when straighten is applied (Photos pattern)
        let showGrid = isDragging || abs(straighten) > 0.1
        if showGrid {
            ctx.setStrokeColor(gridColor)
            ctx.setLineWidth(0.5 / vpScale)
            let divisions = 8  // Photos uses a denser grid
            for i in 1..<divisions {
                let gx = cw * Double(i) / Double(divisions)
                ctx.move(to: CGPoint(x: gx, y: 0))
                ctx.addLine(to: CGPoint(x: gx, y: ch))
                ctx.strokePath()

                let gy = ch * Double(i) / Double(divisions)
                ctx.move(to: CGPoint(x: 0, y: gy))
                ctx.addLine(to: CGPoint(x: cw, y: gy))
                ctx.strokePath()
            }
        }

        // Corner handles — white L-brackets (Photos-style)
        drawCornerHandles(ctx: ctx, cw: cw, ch: ch, vpScale: vpScale)

        // Edge handles — white pills
        drawEdgeHandles(ctx: ctx, cw: cw, ch: ch, vpScale: vpScale)
    }

    /// Corner handles — L-brackets like Photos: vertex outside the crop border,
    /// arms run inward along the border edges. Like a picture frame corner.
    private func drawCornerHandles(ctx: CGContext, cw: Double, ch: Double, vpScale: Double) {
        let len = min(20 / vpScale, cw / 4, ch / 4)
        let lineW = 3.0 / vpScale
        let outset = lineW / 2  // vertex sits flush with outer edge of border

        ctx.setStrokeColor(NSColor.white.cgColor)
        ctx.setLineWidth(lineW)
        ctx.setLineCap(.square)

        // NW: vertex outside top-left, arms go right and down along border
        drawL(ctx: ctx, vx: -outset, vy: -outset, hx: len, hy: 0, vax: 0, vay: len)
        // NE: vertex outside top-right, arms go left and down
        drawL(ctx: ctx, vx: cw + outset, vy: -outset, hx: -len, hy: 0, vax: 0, vay: len)
        // SW: vertex outside bottom-left, arms go right and up
        drawL(ctx: ctx, vx: -outset, vy: ch + outset, hx: len, hy: 0, vax: 0, vay: -len)
        // SE: vertex outside bottom-right, arms go left and up
        drawL(ctx: ctx, vx: cw + outset, vy: ch + outset, hx: -len, hy: 0, vax: 0, vay: -len)
    }

    private func drawL(ctx: CGContext, vx: Double, vy: Double,
                        hx: Double, hy: Double, vax: Double, vay: Double) {
        // Horizontal arm from vertex
        ctx.beginPath()
        ctx.move(to: CGPoint(x: vx, y: vy))
        ctx.addLine(to: CGPoint(x: vx + hx, y: vy + hy))
        ctx.strokePath()
        // Vertical arm from vertex
        ctx.beginPath()
        ctx.move(to: CGPoint(x: vx, y: vy))
        ctx.addLine(to: CGPoint(x: vx + vax, y: vy + vay))
        ctx.strokePath()
    }

    /// Edge handles — short white bars at midpoints, ON the crop border.
    private func drawEdgeHandles(ctx: CGContext, cw: Double, ch: Double, vpScale: Double) {
        let barLen = min(20 / vpScale, cw / 5, ch / 5)
        let lineW = 3.0 / vpScale

        ctx.setStrokeColor(NSColor.white.cgColor)
        ctx.setLineWidth(lineW)
        ctx.setLineCap(.round)

        // Top, bottom (horizontal)
        for y in [0.0, ch] {
            ctx.beginPath()
            ctx.move(to: CGPoint(x: cw / 2 - barLen / 2, y: y))
            ctx.addLine(to: CGPoint(x: cw / 2 + barLen / 2, y: y))
            ctx.strokePath()
        }

        // Left, right (vertical)
        for x in [0.0, cw] {
            ctx.beginPath()
            ctx.move(to: CGPoint(x: x, y: ch / 2 - barLen / 2))
            ctx.addLine(to: CGPoint(x: x, y: ch / 2 + barLen / 2))
            ctx.strokePath()
        }
    }

    // ── Pointer events ──────────────────────────────────────────────

    private func screenToCropSpace(_ point: NSPoint) -> (x: Double, y: Double) {
        let cropX = (point.x - viewportOffsetX) / viewportScale - (model?.state.cropWidth ?? 0) / 2
        let cropY = (point.y - viewportOffsetY) / viewportScale - (model?.state.cropHeight ?? 0) / 2
        return (cropX, cropY)
    }

    override func mouseDown(with event: NSEvent) {
        let point = convert(event.locationInWindow, from: nil)
        let (cx, cy) = screenToCropSpace(point)
        let isAlt = event.modifierFlags.contains(.option)
        model?.bridge.onPointerDownX(cx, y: cy, altOrMiddle: isAlt)
        needsDisplay = true
    }

    override func mouseDragged(with event: NSEvent) {
        let point = convert(event.locationInWindow, from: nil)
        let (cx, cy) = screenToCropSpace(point)
        model?.bridge.onPointerMoveX(cx, y: cy, screenDeltaX: Double(event.deltaX),
                                      screenDeltaY: Double(event.deltaY))
        needsDisplay = true
    }

    override func mouseUp(with event: NSEvent) {
        model?.bridge.onPointerUp()
        needsDisplay = true
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let ta = trackingArea { removeTrackingArea(ta) }
        trackingArea = NSTrackingArea(rect: bounds,
                                      options: [.mouseMoved, .activeInKeyWindow],
                                      owner: self, userInfo: nil)
        addTrackingArea(trackingArea!)
    }

    override func mouseMoved(with event: NSEvent) {
        guard model?.state.isCropMode == true else {
            NSCursor.arrow.set()
            return
        }
        let point = convert(event.locationInWindow, from: nil)
        let (cx, cy) = screenToCropSpace(point)
        guard let handle = model?.bridge.hitTestX(cx, y: cy) else {
            NSCursor.arrow.set()
            return
        }
        switch handle {
        case "n", "s":   NSCursor.resizeUpDown.set()
        case "e", "w":   NSCursor.resizeLeftRight.set()
        case "move":     NSCursor.openHand.set()
        default:         NSCursor.arrow.set()
        }
    }
}
