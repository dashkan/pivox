import AppKit
import SwiftUI

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
        .font(.callout)
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

// MARK: - NSViewRepresentable Bridge

/// Bridges `RulerNSSlider` into SwiftUI via Coordinator pattern.
/// Accessibility identifier is set directly on the NSView (not via SwiftUI modifier)
/// because identifiers don't propagate through NSViewRepresentable.
struct RulerSliderBridge: NSViewRepresentable {
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

// MARK: - Custom NSSlider

/// NSSlider subclass that tracks hover state and fires a commit callback on mouseUp.
/// Flipped coordinate system (origin top-left) matches the Photos tick pattern layout.
final class RulerNSSlider: NSSlider {
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
final class RulerSliderNSCell: NSSliderCell {
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

  // swiftlint:disable:next function_body_length
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
    ctx.fill(
      CGRect(
        x: centerX - centerBarWidth / 2, y: rect.minY,
        width: centerBarWidth, height: height))

    // Fill from center to current value
    let fillLeft = min(centerX, indicatorX)
    let fillW = abs(indicatorX - centerX)
    if fillW > 0.5 {
      let fillColor =
        isActive
        ? NSColor.controlAccentColor
        : NSColor.labelColor.withAlphaComponent(0.15)
      ctx.setFillColor(fillColor.cgColor)
      ctx.fill(CGRect(x: fillLeft, y: rect.minY, width: fillW, height: height))
    }

    // Label text (left) + degrees text (right)
    // Match the SwiftUI `.callout` size used elsewhere in this view
    // so the ruler labels line up with other UI metrics.
    let labelFontSize = NSFont.preferredFont(forTextStyle: .callout).pointSize
    let labelAttrs: [NSAttributedString.Key: Any] = [
      .font: NSFont.systemFont(ofSize: labelFontSize),
      .foregroundColor: NSColor.secondaryLabelColor,
    ]
    let degreeAttrs: [NSAttributedString.Key: Any] = [
      .font: NSFont.monospacedDigitSystemFont(ofSize: labelFontSize, weight: .regular),
      .foregroundColor: isActive ? NSColor.labelColor : NSColor.secondaryLabelColor,
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
