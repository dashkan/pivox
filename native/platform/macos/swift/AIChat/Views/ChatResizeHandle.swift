import AppKit
import SwiftUI

/// AppKit-native resize handle for the floating AI chat panel.
///
/// # Why this isn't pure SwiftUI
///
/// Float-mode resize was originally a SwiftUI `.overlay { Rectangle }`
/// with a `DragGesture` and `pointerStyle(.frameResize)`. It worked,
/// but flickered visibly during drag — both the cursor and the panel
/// edge. We tried three escalating SwiftUI-only fixes (snapshotting
/// the start width, swapping `NSCursor.push/pop` for `pointerStyle`,
/// reordering overlay relative to `clipShape`); each reduced the
/// flicker but none eliminated it.
///
/// The reason is structural, not a bug we can paper over:
///
///   1. `.frame(width: chatPanelWidth)` triggers a full SwiftUI
///      layout pass every drag tick. The overlay strip — and the
///      cursor rect SwiftUI publishes for it via `pointerStyle` —
///      moves with the panel edge frame-by-frame. AppKit chases the
///      rect, occasionally drops a frame, and reverts to `.arrow`.
///   2. `.thinMaterial` recomposites every tick because the blur
///      sample under it is changing as the panel resizes. That's
///      another expensive operation in the hot drag loop.
///   3. SwiftUI's `DragGesture` is reactive — it produces a stream
///      of `value.translation` snapshots that we read inside `body`,
///      which *is* the layout pass. There's no "drag handle owns
///      the drag" model: every tick is a re-render of the whole
///      reactive subgraph.
///
/// Push mode (HSplitView) doesn't have any of this. NSSplitView's
/// divider is a real NSView. Its cursor rect is registered once via
/// `resetCursorRects` and stays in AppKit's cursor system until the
/// view's bounds change. The drag is captured natively by the
/// divider — mouse events route to it directly without a SwiftUI
/// rebuild. There's no translucent material between the cursor and
/// the divider for AppKit to recomposite.
///
/// # The pattern
///
/// When SwiftUI's reactive layout becomes the bottleneck — drag
/// handles, large scroll views, custom hit-testing, anything that
/// needs frame-rate-stable input across a translucent surface — drop
/// to `NSViewRepresentable`. Own the input on a real NSView (cursor
/// rects, mouse events, drag capture) and write through to SwiftUI
/// via a binding. SwiftUI re-renders only on `width` changes, not
/// on raw mouse-move events.
///
/// This handle ports the NSSplitView-divider pattern: a real NSView,
/// a registered cursor rect (added via `resetCursorRects`, not
/// republished per layout), and native `mouseDown`/`mouseDragged`/
/// `mouseUp` that capture the drag and write back through a binding.
/// The owning SwiftUI view sees a single `width` change per tick
/// and lays out once — no overlay-frame chase, no cursor-rect
/// republishing.
///
/// Translation is subtracted because the panel sits on the trailing
/// edge of the window — dragging LEFT widens it.
struct ChatResizeHandle: NSViewRepresentable {

  @Binding var width: CGFloat
  let range: ClosedRange<CGFloat>
  /// Called once when the drag ends, so the caller can persist the
  /// final width. Not called per tick — persistence on every frame
  /// would thrash UserDefaults.
  let onEnded: (CGFloat) -> Void

  func makeNSView(context: Context) -> ResizeHandleView {
    let view = ResizeHandleView()
    view.coordinator = context.coordinator
    return view
  }

  func updateNSView(_ nsView: ResizeHandleView, context: Context) {
    // True no-op. `context.coordinator` is the same object for the
    // representable's lifetime, and the handle has no SwiftUI-driven
    // visual state — drag input flows handle → binding only.
  }

  func makeCoordinator() -> Coordinator {
    Coordinator(parent: self)
  }

  final class Coordinator {
    var parent: ChatResizeHandle
    var dragStartWidth: CGFloat?

    init(parent: ChatResizeHandle) {
      self.parent = parent
    }

    func begin() {
      dragStartWidth = parent.width
    }

    func update(translationX: CGFloat) {
      guard let start = dragStartWidth else { return }
      let candidate = start - translationX
      let clamped = min(max(candidate, parent.range.lowerBound),
                        parent.range.upperBound)
      parent.width = clamped
    }

    func end() {
      dragStartWidth = nil
      parent.onEnded(parent.width)
    }
  }

  /// Custom NSView that owns its cursor rect and drag tracking.
  /// Cursor and hit-testing are AppKit-native — same model as
  /// NSSplitView's divider — so neither the SwiftUI layout pass nor
  /// the panel's `.thinMaterial` recomposite can disturb them.
  final class ResizeHandleView: NSView {
    weak var coordinator: Coordinator?
    private var dragStartLocationInWindow: NSPoint?

    override var isFlipped: Bool { true }

    override func resetCursorRects() {
      // Registered once per layout pass via AppKit's cursor system.
      // Unlike SwiftUI `pointerStyle`, this doesn't get republished
      // every drag tick, so AppKit doesn't lose track of it under
      // load.
      addCursorRect(bounds, cursor: .resizeLeftRight)
    }

    override func mouseDown(with event: NSEvent) {
      dragStartLocationInWindow = event.locationInWindow
      coordinator?.begin()
      // Pin the cursor for the duration of the drag. `addCursorRect`
      // handles the hover case; this guarantees the resize cursor
      // stays put even if the cursor briefly leaves the strip's
      // bounds during a fast drag (the strip moves with the panel
      // edge, so the cursor's relative position can drift).
      NSCursor.resizeLeftRight.push()
    }

    override func mouseDragged(with event: NSEvent) {
      guard let start = dragStartLocationInWindow else { return }
      let dx = event.locationInWindow.x - start.x
      coordinator?.update(translationX: dx)
    }

    override func mouseUp(with event: NSEvent) {
      NSCursor.pop()
      dragStartLocationInWindow = nil
      coordinator?.end()
    }
  }
}
