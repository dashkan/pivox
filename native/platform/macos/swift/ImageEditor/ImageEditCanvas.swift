import AppKit
import CoreImage
import SwiftUI

// MARK: - Canvas Container (handles flip animation)

struct ImageEditCanvasContainer: View {
  let model: ImageEditModel
  let image: NSImage

  @Environment(\.pivoxTheme) private var theme
  @State private var flipHAngle: Double = 0
  @State private var flipVAngle: Double = 0

  var body: some View {
    ZStack {
      theme.background.ignoresSafeArea()
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

// MARK: - Canvas NSViewRepresentable

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

// MARK: - Canvas Renderer

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

  /// Computed values passed from draw() to the mode-specific rendering methods.
  private struct DrawContext {
    let image: CGImage
    let imgW: Double
    let imgH: Double
    let cropScreenX: Double
    let cropScreenY: Double
    let cropScreenW: Double
    let cropScreenH: Double
    let containerW: Double
    let containerH: Double
    let vpScale: Double
    let angleRad: Double
    let flipX: Double
    let flipY: Double
    let state: IEBState
    let cw: Double
    let ch: Double
  }

  // swiftlint:disable:next function_body_length
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

    let perspV = s.perspectiveV * .pi / 180
    let perspH = s.perspectiveH * .pi / 180

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

    let dc = DrawContext(
      image: drawImage, imgW: imgW, imgH: imgH,
      cropScreenX: cropScreenX, cropScreenY: cropScreenY,
      cropScreenW: cropScreenW, cropScreenH: cropScreenH,
      containerW: containerW, containerH: containerH,
      vpScale: vpScale,
      angleRad: (Double(s.rotation) + s.straighten) * .pi / 180,
      flipX: s.flipHorizontal ? -1 : 1,
      flipY: s.flipVertical ? -1 : 1,
      state: s, cw: cw, ch: ch)

    if s.isCropMode {
      drawCropMode(ctx: ctx, dc: dc)
    } else {
      drawViewMode(ctx: ctx, dc: dc)
    }
  }

  // MARK: - Draw Modes

  private func drawCropMode(ctx: CGContext, dc: DrawContext) {
    let cgFlipY: Double = -1

    // 1. Image
    ctx.saveGState()
    ctx.translateBy(
      x: dc.cropScreenX + dc.cropScreenW / 2 + dc.state.tx * dc.vpScale,
      y: dc.cropScreenY + dc.cropScreenH / 2 + dc.state.ty * dc.vpScale)
    ctx.rotate(by: dc.angleRad)
    ctx.scaleBy(
      x: dc.flipX * dc.state.scale * dc.vpScale,
      y: dc.flipY * dc.state.scale * dc.vpScale * cgFlipY)
    ctx.draw(
      dc.image, in: CGRect(x: -dc.imgW / 2, y: -dc.imgH / 2, width: dc.imgW, height: dc.imgH))
    ctx.restoreGState()

    // 2. Dimmed overlay
    ctx.setFillColor(NSColor.windowBackgroundColor.withAlphaComponent(0.5).cgColor)
    ctx.fill(CGRect(x: 0, y: 0, width: dc.containerW, height: dc.cropScreenY))
    ctx.fill(
      CGRect(
        x: 0, y: dc.cropScreenY + dc.cropScreenH, width: dc.containerW,
        height: dc.containerH - dc.cropScreenY - dc.cropScreenH))
    ctx.fill(CGRect(x: 0, y: dc.cropScreenY, width: dc.cropScreenX, height: dc.cropScreenH))
    ctx.fill(
      CGRect(
        x: dc.cropScreenX + dc.cropScreenW, y: dc.cropScreenY,
        width: dc.containerW - dc.cropScreenX - dc.cropScreenW, height: dc.cropScreenH))

    // 3. Controls
    ctx.saveGState()
    ctx.translateBy(x: dc.cropScreenX, y: dc.cropScreenY)
    ctx.scaleBy(x: dc.vpScale, y: dc.vpScale)
    drawControls(ctx: ctx, dc: dc)
    ctx.restoreGState()
  }

  private func drawViewMode(ctx: CGContext, dc: DrawContext) {
    let cgFlipY: Double = -1

    ctx.saveGState()
    ctx.clip(
      to: CGRect(
        x: dc.cropScreenX, y: dc.cropScreenY,
        width: dc.cropScreenW, height: dc.cropScreenH))
    ctx.translateBy(
      x: dc.cropScreenX + dc.cropScreenW / 2 + dc.state.tx * dc.vpScale,
      y: dc.cropScreenY + dc.cropScreenH / 2 + dc.state.ty * dc.vpScale)
    ctx.rotate(by: dc.angleRad)
    ctx.scaleBy(
      x: dc.flipX * dc.state.scale * dc.vpScale,
      y: dc.flipY * dc.state.scale * dc.vpScale * cgFlipY)
    ctx.draw(
      dc.image, in: CGRect(x: -dc.imgW / 2, y: -dc.imgH / 2, width: dc.imgW, height: dc.imgH))
    ctx.restoreGState()
  }

  // MARK: - Controls (crop-space coordinates)

  private func drawControls(ctx: CGContext, dc: DrawContext) {
    let cw = dc.cw
    let ch = dc.ch
    let vpScale = dc.vpScale
    // Literal white for crop UI is intentional — these draw on top
    // of the user's image, not the app background. Using semantic
    // `labelColor` would render black-on-bright-regions in light
    // mode (invisible). Every image editor (Photos, Lightroom,
    // Pixelmator) uses white crop handles regardless of appearance.
    let white = NSColor.white.cgColor
    let gridColor = NSColor.white.withAlphaComponent(0.25).cgColor

    // Thin white crop border
    ctx.setStrokeColor(white)
    ctx.setLineWidth(1.0 / vpScale)
    ctx.stroke(CGRect(x: 0, y: 0, width: cw, height: ch))

    // Grid
    let s = dc.state
    let showGrid =
      s.isDragging || abs(s.straighten) > 0.1
      || abs(s.perspectiveV) > 0.1
      || abs(s.perspectiveH) > 0.1
    if showGrid {
      ctx.setStrokeColor(gridColor)
      ctx.setLineWidth(0.5 / vpScale)
      let divisions = 8
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

    drawCornerHandles(ctx: ctx, cw: cw, ch: ch, vpScale: vpScale)
    drawEdgeHandles(ctx: ctx, cw: cw, ch: ch, vpScale: vpScale)
  }

  private func drawCornerHandles(ctx: CGContext, cw: Double, ch: Double, vpScale: Double) {
    let len = min(20 / vpScale, cw / 4, ch / 4)
    let lineW = 3.0 / vpScale
    let outset = lineW / 2

    ctx.setStrokeColor(NSColor.white.cgColor)
    ctx.setLineWidth(lineW)
    ctx.setLineCap(.square)

    // NW, NE, SW, SE
    let brackets = [
      LBracket(vx: -outset, vy: -outset, hx: len, hy: 0, vax: 0, vay: len),
      LBracket(vx: cw + outset, vy: -outset, hx: -len, hy: 0, vax: 0, vay: len),
      LBracket(vx: -outset, vy: ch + outset, hx: len, hy: 0, vax: 0, vay: -len),
      LBracket(vx: cw + outset, vy: ch + outset, hx: -len, hy: 0, vax: 0, vay: -len),
    ]
    for b in brackets {
      ctx.beginPath()
      ctx.move(to: CGPoint(x: b.vx, y: b.vy))
      ctx.addLine(to: CGPoint(x: b.vx + b.hx, y: b.vy + b.hy))
      ctx.strokePath()
      ctx.beginPath()
      ctx.move(to: CGPoint(x: b.vx, y: b.vy))
      ctx.addLine(to: CGPoint(x: b.vx + b.vax, y: b.vy + b.vay))
      ctx.strokePath()
    }
  }

  /// L-bracket corner handle: vertex position + two arm directions.
  private struct LBracket {
    let vx, vy: Double      // vertex
    let hx, hy: Double      // horizontal arm delta
    let vax, vay: Double     // vertical arm delta
  }

  private func drawEdgeHandles(ctx: CGContext, cw: Double, ch: Double, vpScale: Double) {
    let barLen = min(20 / vpScale, cw / 5, ch / 5)
    let lineW = 3.0 / vpScale

    ctx.setStrokeColor(NSColor.white.cgColor)
    ctx.setLineWidth(lineW)
    ctx.setLineCap(.round)

    for y in [0.0, ch] {
      ctx.beginPath()
      ctx.move(to: CGPoint(x: cw / 2 - barLen / 2, y: y))
      ctx.addLine(to: CGPoint(x: cw / 2 + barLen / 2, y: y))
      ctx.strokePath()
    }

    for x in [0.0, cw] {
      ctx.beginPath()
      ctx.move(to: CGPoint(x: x, y: ch / 2 - barLen / 2))
      ctx.addLine(to: CGPoint(x: x, y: ch / 2 + barLen / 2))
      ctx.strokePath()
    }
  }

  // MARK: - Pointer Events

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
    model?.bridge.onPointerMoveX(
      cx, y: cy, screenDeltaX: Double(event.deltaX),
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
    trackingArea = NSTrackingArea(
      rect: bounds,
      options: [.mouseMoved, .activeInKeyWindow],
      owner: self, userInfo: nil)
    addTrackingArea(trackingArea!)
  }

  override func mouseMoved(with event: NSEvent) {
    // Bail out unless the pointer is actually over us. Tracking
    // areas fire mouseMoved across our full bounds even when an
    // overlay covers part of them (e.g. the floating AI chat panel
    // overlaid in a ZStack), and calling `NSCursor.set` here would
    // stomp the overlay's own cursor. Treat both "hit-tested to a
    // sibling on top" and "hit-test returned nil" (cursor briefly
    // outside the window content area while a tracking event was
    // already in flight) as bail-outs — the only case we want to
    // proceed is when the canvas itself owns the cursor position.
    if let window {
      guard let hit = window.contentView?.hitTest(event.locationInWindow),
            hit === self || hit.isDescendant(of: self) else { return }
    }
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
    case "n", "s": NSCursor.resizeUpDown.set()
    case "e", "w": NSCursor.resizeLeftRight.set()
    case "move": NSCursor.openHand.set()
    default: NSCursor.arrow.set()
    }
  }

  // MARK: - Perspective Warp (Core Image)

  private lazy var ciContext = CIContext()

  /// Applies a perspective correction warp using CIPerspectiveTransform.
  func perspectiveWarp(_ image: CGImage, vertRad: Double, horizRad: Double) -> CGImage? {
    let w = Double(image.width)
    let h = Double(image.height)
    // Per-axis focal length — must match C++ perspective_constants.
    let base = max(w, h)
    let dV = base * 1.4  // kFocalLengthMultiplierV
    let dH = base * 0.8  // kFocalLengthMultiplierH

    let cosV = cos(vertRad)
    let sinV = sin(vertRad)
    let cosH = cos(horizRad)
    let sinH = sin(horizRad)

    func project(_ x: Double, _ y: Double) -> CGPoint {
      let denom = 1.0 + x * sinH / dH + y * sinV / dV
      let px = (x * cosH) / denom
      let py = (y * cosV) / denom
      return CGPoint(x: px + w / 2, y: py + h / 2)
    }

    // In CI coordinates (y-up), -h/2 is bottom, +h/2 is top.
    let bl = project(-w / 2, -h / 2)
    let br = project(w / 2, -h / 2)
    let tr = project(w / 2, h / 2)
    let tl = project(-w / 2, h / 2)

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
      !extent.isInfinite
    else { return nil }
    return ciContext.createCGImage(output, from: extent)
  }
}
