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

            // Straighten + Perspective — spacing matches Photos.app
            VStack(spacing: 6) {
                RulerSliderRow(label: "Straighten", icon: "circle.and.line.horizontal.fill",
                          value: Binding(
                            get: { model.state.straighten },
                            set: { model.bridge.setStraighten($0) }
                          ),
                          range: -45...45,
                          onCommit: { model.bridge.commitStraighten() },
                          id: "edit-straighten")

                RulerSliderRow(label: "Vertical", icon: "trapezoid.and.line.vertical.fill",
                          value: Binding(
                            get: { model.state.perspectiveV },
                            set: { model.bridge.setPerspectiveV($0) }
                          ),
                          range: -30...30,
                          onCommit: { model.bridge.commitPerspective() },
                          id: "edit-perspective-v")

                RulerSliderRow(label: "Horizontal", icon: "trapezoid.and.line.horizontal.fill",
                          value: Binding(
                            get: { model.state.perspectiveH },
                            set: { model.bridge.setPerspectiveH($0) }
                          ),
                          range: -30...30,
                          onCommit: { model.bridge.commitPerspective() },
                          id: "edit-perspective-h")
            }

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
/// Uses proper Button for accessibility (AXButton role, VoiceOver, keyboard).
struct CropToolButton: View {
    let icon: String
    let label: String
    var id: String = ""
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                Image(systemName: icon)
                    .font(.system(size: 13))
                    .foregroundStyle(.secondary)
                    .frame(width: 18)
                Text(label)
                    .font(.system(size: 13))
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(id)
    }
}

/// Photos-style straighten ruler control backed by a proper NSSlider.
/// Provides AXSlider accessibility, keyboard arrow keys, and VoiceOver for free.
///
/// Pixel-verified tick pattern (from Photos.app screenshot analysis):
///   - 5pt spacing between all ticks, symmetric from center
///   - Pattern from center outward: [4 short, 1 tall] repeating
///   - Every 10th tick from center: BRIGHT (rgb 191 = .primary.opacity(0.75))
///   - All other ticks: DIM (rgb 105 = .primary.opacity(0.4))
///   - Short ticks: 3pt, Tall ticks: 5pt
///   - Center bar: 2pt wide, muted blue-gray (NOT accent blue)
struct RulerSliderRow: View {
    let label: String
    let icon: String
    @Binding var value: Double
    let range: ClosedRange<Double>
    var onCommit: (() -> Void)?
    var id: String = ""

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: icon)
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
                .frame(width: 18)

            RulerSliderBridge(
                value: $value,
                range: range,
                label: label,
                onCommit: onCommit,
                accessibilityID: id
            )
            .frame(height: 24)
        }
        .padding(.horizontal, 20)
    }
}

// MARK: - Ruler Slider NSViewRepresentable

/// Bridges `RulerNSSlider` into SwiftUI via Coordinator pattern.
/// Accessibility identifier is set directly on the NSView (not via SwiftUI modifier)
/// because identifiers don't propagate through NSViewRepresentable.
private struct RulerSliderBridge: NSViewRepresentable {
    @Binding var value: Double
    let range: ClosedRange<Double>
    var label: String = ""
    var onCommit: (() -> Void)?
    var accessibilityID: String = ""

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> RulerNSSlider {
        let slider = RulerNSSlider(frame: .zero)
        slider.minValue = range.lowerBound
        slider.maxValue = range.upperBound
        slider.doubleValue = value
        slider.isContinuous = true
        (slider.cell as? RulerSliderNSCell)?.label = label
        slider.setAccessibilityIdentifier(accessibilityID)
        slider.setAccessibilityLabel(label)
        slider.target = context.coordinator
        slider.action = #selector(Coordinator.sliderChanged(_:))
        let coordinator = context.coordinator
        slider.commitAction = { coordinator.commit() }
        return slider
    }

    func updateNSView(_ nsView: RulerNSSlider, context: Context) {
        context.coordinator.parent = self
        // Don't fight NSSlider's own value during drag — only sync from model.
        if nsView.doubleValue != value {
            nsView.doubleValue = value
        }
        (nsView.cell as? RulerSliderNSCell)?.label = label
        let coordinator = context.coordinator
        nsView.commitAction = { coordinator.commit() }
        nsView.needsDisplay = true
    }

    final class Coordinator: NSObject {
        var parent: RulerSliderBridge

        init(_ parent: RulerSliderBridge) {
            self.parent = parent
        }

        @objc func sliderChanged(_ sender: NSSlider) {
            parent.value = sender.doubleValue
        }

        /// Called on mouseUp — snap near-zero and notify engine.
        func commit() {
            if abs(parent.value) < 1.0 { parent.value = 0 }
            parent.onCommit?()
        }
    }
}

// MARK: - Custom NSSlider (hover tracking + commit on mouseUp)

/// NSSlider subclass that tracks hover state and fires a commit callback on mouseUp.
/// Flipped coordinate system (origin top-left) matches the Photos tick pattern layout.
private final class RulerNSSlider: NSSlider {
    var commitAction: (() -> Void)?
    private var hoverArea: NSTrackingArea?

    override var isFlipped: Bool { true }

    // Match Photos: exactly 24pt tall, flexible width.
    override var intrinsicContentSize: NSSize {
        NSSize(width: NSView.noIntrinsicMetric, height: 24)
    }

    override init(frame: NSRect) {
        super.init(frame: frame)
        let c = RulerSliderNSCell()
        c.sliderType = .linear
        self.cell = c
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    override func mouseDown(with event: NSEvent) {
        super.mouseDown(with: event)  // blocks until mouseUp (AppKit tracking loop)
        commitAction?()
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let ta = hoverArea { removeTrackingArea(ta) }
        hoverArea = NSTrackingArea(
            rect: bounds,
            options: [.mouseEnteredAndExited, .activeInKeyWindow],
            owner: self, userInfo: nil)
        addTrackingArea(hoverArea!)
    }

    override func mouseEntered(with event: NSEvent) {
        (cell as? RulerSliderNSCell)?.isHovered = true
    }

    override func mouseExited(with event: NSEvent) {
        (cell as? RulerSliderNSCell)?.isHovered = false
    }
}

// MARK: - Custom NSSliderCell (Photos-style ruler ticks)

/// Draws the Photos-style ruler: rounded dark bar, tick marks on hover/drag,
/// muted blue-gray center bar, accent fill from center to value, label + degrees text.
/// No visible knob — the fill IS the indicator.
private final class RulerSliderNSCell: NSSliderCell {
    var label: String = "" { didSet { controlView?.needsDisplay = true } }
    var isHovered: Bool = false { didSet { controlView?.needsDisplay = true } }

    /// Active when hovered or during NSSliderCell's mouse tracking.
    private var isActive: Bool { isHovered || isHighlighted }

    // Tick layout — measured from Photos.app at 2x retina.
    private let ticksPerSide = 19
    private let dimOpacity: CGFloat = 0.4
    private let brightOpacity: CGFloat = 0.75
    private let shortHeight: CGFloat = 3.0
    private let tallHeight: CGFloat = 5.0
    private let centerBarWidth: CGFloat = 2.0

    /// Format degrees like Photos: shows "0°" at zero.
    private var degreeText: String {
        if abs(doubleValue) < 0.5 { return "0°" }
        return String(format: "%.0f°", doubleValue)
    }

    // The entire cell bounds IS the bar — no separate knob track.
    override func barRect(flipped: Bool) -> NSRect {
        controlView?.bounds ?? .zero
    }

    // Invisible knob positioned at the value for hit testing.
    override func knobRect(flipped: Bool) -> NSRect {
        let bar = barRect(flipped: flipped)
        guard bar.width > 0, maxValue > minValue else { return .zero }
        let fraction = CGFloat((doubleValue - minValue) / (maxValue - minValue))
        let x = bar.minX + fraction * bar.width
        return NSRect(x: x - 1, y: bar.minY, width: 2, height: bar.height)
    }

    override func drawBar(inside rect: NSRect, flipped: Bool) {
        guard let ctx = NSGraphicsContext.current?.cgContext else { return }
        let width = rect.width
        let height = rect.height
        let centerX = rect.midX
        guard width > 0, maxValue > minValue else { return }
        let fraction = CGFloat((doubleValue - minValue) / (maxValue - minValue))
        let indicatorX = rect.minX + fraction * width

        ctx.saveGState()

        // Clip to rounded rect
        let bgPath = CGPath(roundedRect: rect, cornerWidth: 4, cornerHeight: 4, transform: nil)
        ctx.addPath(bgPath)
        ctx.clip()

        // Background
        ctx.setFillColor(NSColor.labelColor.withAlphaComponent(0.08).cgColor)
        ctx.fill(rect)

        // Tick marks — only when active (hovered or tracking)
        if isActive {
            // 20 divisions per side — draw only the inner 19 so the last
            // tick doesn't land on the rounded-rect edge.
            let tickSpacing = width / 40.0
            for i in 1...ticksPerSide {
                let isBright = (i % 10 == 0)
                let isTall = (i % 5 == 0)
                let tickH = isTall ? tallHeight : shortHeight
                let opacity = isBright ? brightOpacity : dimOpacity
                let offset = CGFloat(i) * tickSpacing

                ctx.setStrokeColor(NSColor.labelColor.withAlphaComponent(opacity).cgColor)
                ctx.setLineWidth(0.5)

                // Right side of center
                ctx.beginPath()
                ctx.move(to: CGPoint(x: centerX + offset, y: rect.minY + 1))
                ctx.addLine(to: CGPoint(x: centerX + offset, y: rect.minY + 1 + tickH))
                ctx.strokePath()

                // Left side of center
                ctx.beginPath()
                ctx.move(to: CGPoint(x: centerX - offset, y: rect.minY + 1))
                ctx.addLine(to: CGPoint(x: centerX - offset, y: rect.minY + 1 + tickH))
                ctx.strokePath()
            }
        }

        // Center bar — muted blue-gray, NOT system accent
        ctx.setFillColor(NSColor.controlAccentColor.withAlphaComponent(isActive ? 0.5 : 0.25).cgColor)
        ctx.fill(CGRect(x: centerX - centerBarWidth / 2, y: rect.minY,
                        width: centerBarWidth, height: height))

        // Fill from center to current value
        let fillLeft = min(centerX, indicatorX)
        let fillW = abs(indicatorX - centerX)
        if fillW > 0.5 {
            let fillColor = isActive
                ? NSColor.controlAccentColor
                : NSColor.labelColor.withAlphaComponent(0.15)
            ctx.setFillColor(fillColor.cgColor)
            ctx.fill(CGRect(x: fillLeft, y: rect.minY, width: fillW, height: height))
        }

        // Label text (left) + degrees text (right)
        let labelAttrs: [NSAttributedString.Key: Any] = [
            .font: NSFont.systemFont(ofSize: 13),
            .foregroundColor: NSColor.secondaryLabelColor
        ]
        let degreeAttrs: [NSAttributedString.Key: Any] = [
            .font: NSFont.monospacedDigitSystemFont(ofSize: 13, weight: .regular),
            .foregroundColor: isActive ? NSColor.labelColor : NSColor.secondaryLabelColor
        ]
        let labelStr = NSAttributedString(string: label, attributes: labelAttrs)
        let degreeStr = NSAttributedString(string: degreeText, attributes: degreeAttrs)
        let labelSize = labelStr.size()
        let degreeSize = degreeStr.size()

        // Flipped view: origin at top-left, draw(at:) places text top-left corner
        let textY = rect.minY + (height - labelSize.height) / 2
        labelStr.draw(at: NSPoint(x: rect.minX + 10, y: textY))
        let degreeY = rect.minY + (height - degreeSize.height) / 2
        degreeStr.draw(at: NSPoint(x: rect.maxX - 10 - degreeSize.width, y: degreeY))

        ctx.restoreGState()
    }

    override func drawKnob(_ knobRect: NSRect) {
        // No visible knob — the fill IS the indicator.
    }

    override func drawKnob() {
        // No visible knob.
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
        let perspV = s.perspectiveV * .pi / 180
        let perspH = s.perspectiveH * .pi / 180

        // Apply perspective warp via Core Image when non-zero.
        let drawImage: CGImage
        let imgW: Double
        let imgH: Double
        if abs(perspV) > 0.01 || abs(perspH) > 0.01 {
            drawImage = perspectiveWarp(cgImage, vertRad: perspV, horizRad: perspH) ?? cgImage
            imgW = Double(drawImage.width)
            imgH = Double(drawImage.height)
        } else {
            drawImage = cgImage
            imgW = Double(cgImage.width)
            imgH = Double(cgImage.height)
        }

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
            ctx.draw(drawImage, in: CGRect(x: -imgW / 2, y: -imgH / 2, width: imgW, height: imgH))
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
            ctx.draw(drawImage, in: CGRect(x: -imgW / 2, y: -imgH / 2, width: imgW, height: imgH))
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
            || abs(model?.state.perspectiveV ?? 0) > 0.1
            || abs(model?.state.perspectiveH ?? 0) > 0.1
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

    // ── Perspective warp via Core Image ────────────────────────────

    private lazy var ciContext = CIContext()

    /// Applies a perspective correction warp using CIPerspectiveTransform.
    /// Projects image corners through a 3D rotation, then warps the image.
    func perspectiveWarp(_ image: CGImage, vertRad: Double, horizRad: Double) -> CGImage? {
        let w = Double(image.width)
        let h = Double(image.height)
        // Per-axis focal length — must match C++ perspective_constants.
        let base = max(w, h)
        let dV = base * 1.4   // kFocalLengthMultiplierV
        let dH = base * 0.8   // kFocalLengthMultiplierH

        let cosV = cos(vertRad)
        let sinV = sin(vertRad)
        let cosH = cos(horizRad)
        let sinH = sin(horizRad)

        // Compute where each corner lands after 3D perspective projection.
        // Image corners in centered coordinates: (±w/2, ±h/2)
        func project(_ x: Double, _ y: Double) -> CGPoint {
            let denom = 1.0 + x * sinH / dH + y * sinV / dV
            let px = (x * cosH) / denom
            let py = (y * cosV) / denom
            // Convert back to image coordinates (origin at bottom-left for CI)
            return CGPoint(x: px + w / 2, y: py + h / 2)
        }

        // In CI coordinates (y-up), -h/2 is bottom, +h/2 is top.
        let bl = project(-w / 2, -h / 2)  // bottom-left in CI
        let br = project( w / 2, -h / 2)  // bottom-right
        let tr = project( w / 2,  h / 2)  // top-right
        let tl = project(-w / 2,  h / 2)  // top-left

        let ciImage = CIImage(cgImage: image)
        guard let filter = CIFilter(name: "CIPerspectiveTransform") else { return nil }
        filter.setValue(ciImage, forKey: kCIInputImageKey)
        filter.setValue(CIVector(cgPoint: tl), forKey: "inputTopLeft")
        filter.setValue(CIVector(cgPoint: tr), forKey: "inputTopRight")
        filter.setValue(CIVector(cgPoint: br), forKey: "inputBottomRight")
        filter.setValue(CIVector(cgPoint: bl), forKey: "inputBottomLeft")

        guard let output = filter.outputImage else { return nil }
        let extent = output.extent
        guard extent.width > 0, extent.height > 0,
              !extent.isInfinite else { return nil }
        return ciContext.createCGImage(output, from: extent)
    }
}
