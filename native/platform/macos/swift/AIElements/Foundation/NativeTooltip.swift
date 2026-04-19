import AppKit
import SwiftUI

/// SwiftUI's `.help()` modifier silently drops tooltips on plain-style
/// buttons with no background fill. AppKit's tooltip manager relies on
/// hit-testing against non-transparent pixels, and `.buttonStyle(.plain)`
/// strips the NSButton wrapper that would otherwise host a tracking
/// area. The workaround is an NSView overlay that registers a native
/// tooltip and passes mouse events through so the underlying SwiftUI
/// button still receives the click.
///
/// Use `reliableHelp` instead of `.help()` on plain icon buttons. The
/// cost is one extra NSView per button, which is negligible at chat-row
/// scale.
struct NativeTooltip: NSViewRepresentable {
    let tooltip: String

    func makeNSView(context: Context) -> PassThroughTooltipView {
        let view = PassThroughTooltipView()
        view.toolTip = tooltip
        return view
    }

    func updateNSView(_ nsView: PassThroughTooltipView, context: Context) {
        nsView.toolTip = tooltip
    }
}

/// NSView that registers a tooltip via AppKit's NSToolTip manager while
/// forwarding all mouse events to the SwiftUI content layered behind it.
/// Returning nil from `hitTest` is what makes clicks pass through.
final class PassThroughTooltipView: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
}

extension View {
    /// Native AppKit tooltip for views that `.help()` fails on (plain-
    /// style buttons, transparent hit areas). Mirrors `.help()`'s API.
    func reliableHelp(_ text: String) -> some View {
        overlay(NativeTooltip(tooltip: text))
    }
}
