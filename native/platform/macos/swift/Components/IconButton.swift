import AppKit
import SwiftUI

/// Compact icon-only button used across the whole app — chat panel,
/// profile settings, toolbars. One visual + behavioral contract:
///
///  - Icon glyph centered in a square hit target
///  - Pointing-hand cursor on hover (matches hyperlink affordance)
///  - Optional hover background tint
///  - Optional "latched" state for feedback toggles (thumbs up/down)
///  - Native AppKit tooltip via `reliableHelp` (SwiftUI's `.help()`
///    silently drops on plain-style buttons with no fill — see
///    NativeTooltip)
///  - Accessibility label wired through
///
/// Sizing: 13pt glyph by default with ~28pt hit target; override via
/// the `size` param for places that need more presence.
public struct IconButton: View {
    let systemName: String
    let label: String
    let help: String?
    let role: ButtonRole?
    let size: CGFloat
    /// When true, icon is tinted with the accent color to indicate a
    /// latched / toggled-on state (e.g., thumbs-up after voting).
    let isOn: Bool
    /// Whether to show a subtle hover background. Off by default for
    /// actions that live inside tight rows (message action bars); on
    /// for standalone buttons (chat panel header, toolbars) where the
    /// affordance cue helps.
    let showsHoverBackground: Bool
    let action: () -> Void

    @State private var isHovered = false

    public init(
        systemName: String,
        label: String,
        help: String? = nil,
        role: ButtonRole? = nil,
        size: CGFloat = 13,
        isOn: Bool = false,
        showsHoverBackground: Bool = true,
        action: @escaping () -> Void
    ) {
        self.systemName = systemName
        self.label = label
        self.help = help
        self.role = role
        self.size = size
        self.isOn = isOn
        self.showsHoverBackground = showsHoverBackground
        self.action = action
    }

    public var body: some View {
        Button(role: role, action: action) {
            Image(systemName: systemName)
                .font(.system(size: size))
                .frame(width: size + 15, height: size + 15)
                .contentShape(Rectangle())
                .background(
                    RoundedRectangle(cornerRadius: 6)
                        .fill(hoverBackgroundColor)
                )
        }
        .buttonStyle(.plain)
        .foregroundStyle(isOn ? Color.accentColor : Color.secondary)
        // reliableHelp routes through an AppKit tooltip shim because
        // SwiftUI's native .help() fails to register on plain-style
        // buttons with no background fill.
        .reliableHelp(help ?? label)
        .accessibilityLabel(label)
        .pointingHandCursor()
        .onHover { isHovered = $0 }
    }

    private var hoverBackgroundColor: Color {
        guard showsHoverBackground, isHovered else { return .clear }
        return Color.secondary.opacity(0.12)
    }
}

// MARK: - Pointing-hand cursor modifier

/// Push / pop the pointing-hand `NSCursor` as the mouse enters / leaves
/// the view's hit area. SwiftUI on macOS lacks a first-class cursor
/// API, so we call AppKit directly. Push/pop semantics are safe for
/// nested hover regions.
private struct PointingHandCursorModifier: ViewModifier {
    func body(content: Content) -> some View {
        content.onHover { inside in
            if inside {
                NSCursor.pointingHand.push()
            } else {
                NSCursor.pop()
            }
        }
    }
}

extension View {
    /// Show the pointing-hand cursor while the mouse is inside this
    /// view. Used for any "this clicks" affordance that doesn't get
    /// it automatically from a button style.
    public func pointingHandCursor() -> some View {
        modifier(PointingHandCursorModifier())
    }
}
