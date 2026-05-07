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
/// Sizing is driven by `PivoxTheme.iconToolbar` — the standard icon
/// font used across the app (17pt .medium by default, matching
/// Apple's toolbar-icon convention). `iconInline` is available in
/// the theme as a smaller variant if a specific spot needs it later;
/// for now every `IconButton` uses the toolbar size for consistency.
public struct IconButton: View {
    let systemName: String
    let label: String
    let help: String?
    let role: ButtonRole?
    /// When true, icon is tinted with the accent color to indicate a
    /// latched / toggled-on state (e.g., thumbs-up after voting).
    let isOn: Bool
    /// Whether to show a subtle hover background. Off for actions
    /// inside tight rows that already have their own background;
    /// on for standalone buttons where the affordance cue helps.
    let showsHoverBackground: Bool
    let action: () -> Void

    @Environment(\.pivoxTheme) private var theme
    @State private var isHovered = false

    public init(
        systemName: String,
        label: String,
        help: String? = nil,
        role: ButtonRole? = nil,
        isOn: Bool = false,
        showsHoverBackground: Bool = true,
        action: @escaping () -> Void
    ) {
        self.systemName = systemName
        self.label = label
        self.help = help
        self.role = role
        self.isOn = isOn
        self.showsHoverBackground = showsHoverBackground
        self.action = action
    }

    /// Hit-target metrics derived from `theme.iconSize`. 17pt glyph
    /// + 15pt padding = 32pt square hit target, matching Apple's
    /// default toolbar-button metrics.
    private static let hitTarget: CGFloat = 32

    public var body: some View {
        Button(role: role, action: action) {
            Image(systemName: systemName)
                .font(theme.iconToolbar)
                .frame(width: Self.hitTarget, height: Self.hitTarget)
                .contentShape(Rectangle())
                .background(
                    RoundedRectangle(cornerRadius: 6)
                        .fill(hoverBackgroundColor)
                )
        }
        .buttonStyle(.plain)
        .foregroundStyle(foregroundColor)
        // reliableHelp routes through an AppKit tooltip shim because
        // SwiftUI's native .help() fails to register on plain-style
        // buttons with no background fill.
        .reliableHelp(help ?? label)
        .accessibilityLabel(label)
        .pointingHandCursor()
        .onHover { isHovered = $0 }
    }

    /// Foreground color resolution, in priority order:
    ///   1. `role == .destructive` → `theme.destructive` so stop /
    ///      delete glyphs read as decisive.
    ///   2. `isOn` → `theme.accent` for latched / "primary action
    ///      enabled" states (e.g. the chat composer's send glyph
    ///      while text is non-empty).
    ///   3. Default → `theme.textSecondary` so dormant icons sit
    ///      quietly in the chrome.
    private var foregroundColor: Color {
        if role == .destructive { return theme.destructive }
        if isOn { return theme.accent }
        return theme.textSecondary
    }

    private var hoverBackgroundColor: Color {
        guard showsHoverBackground, isHovered else { return .clear }
        return theme.hoverFill
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

// MARK: - Previews

#if DEBUG

/// Visual canvas for `IconButton`. Each preview targets a state
/// transition or visual variant — hover and cursor affordances
/// don't surface in the canvas (Previews don't simulate hover),
/// so those are validated in the running app.

#Preview("Default") {
    IconButton(
        systemName: "paperplane.fill",
        label: "Send",
        action: {}
    )
    .padding()
}

#Preview("Latched (isOn) — feedback toggles") {
    HStack(spacing: 12) {
        IconButton(systemName: "hand.thumbsup",
                   label: "Helpful",
                   isOn: false,
                   action: {})
        IconButton(systemName: "hand.thumbsup.fill",
                   label: "Helpful",
                   isOn: true,
                   action: {})
        IconButton(systemName: "hand.thumbsdown",
                   label: "Not helpful",
                   isOn: false,
                   action: {})
    }
    .padding()
}

#Preview("No hover background — tight rows") {
    HStack(spacing: 4) {
        IconButton(systemName: "doc.on.doc",
                   label: "Copy",
                   showsHoverBackground: false,
                   action: {})
        IconButton(systemName: "arrow.up",
                   label: "Send",
                   showsHoverBackground: false,
                   action: {})
        IconButton(systemName: "ellipsis",
                   label: "More",
                   showsHoverBackground: false,
                   action: {})
    }
    .padding()
}

#Preview("Variants — common toolbar glyphs") {
    HStack(spacing: 12) {
        IconButton(systemName: "plus", label: "New", action: {})
        IconButton(systemName: "magnifyingglass",
                   label: "Search", action: {})
        IconButton(systemName: "gear",
                   label: "Settings", action: {})
        IconButton(systemName: "arrow.up.arrow.down",
                   label: "Sort", action: {})
        IconButton(systemName: "trash",
                   label: "Delete",
                   role: .destructive,
                   action: {})
    }
    .padding()
}

#endif
