import AppKit
import SwiftUI

/// Reusable icon-only action button used across AIElements surfaces
/// (message actions, code block header, future toolbars). Keeps a
/// single visual + behavioral contract so every action button in the
/// app looks and feels the same.
///
/// Behavior:
///  - Icon-only, 26×26 hit area, no chrome
///  - Pointing-hand cursor on hover (matches hyperlink affordance)
///  - Optional "latched" state (e.g., feedback toggles) shown by
///    swapping to the accent color
///  - `help` text wired for tooltips and accessibility
struct ActionIcon: View {
    let systemName: String
    let label: String
    let isOn: Bool
    let action: () -> Void

    init(systemName: String, label: String,
         isOn: Bool = false, action: @escaping () -> Void) {
        self.systemName = systemName
        self.label = label
        self.isOn = isOn
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            Image(systemName: systemName)
                .font(.system(size: 13))
                .frame(width: 26, height: 26)
        }
        .buttonStyle(.plain)
        .contentShape(Rectangle())
        .foregroundStyle(isOn ? Color.accentColor : .secondary)
        // .help() is unreliable on plain-style buttons — see
        // NativeTooltip for the AppKit shim that actually works.
        .reliableHelp(label)
        .accessibilityLabel(label)
        .pointingHandCursor()
    }
}

/// SwiftUI view modifier — push / pop the pointing-hand NSCursor as the
/// mouse enters / leaves the view's hit area. SwiftUI on macOS has no
/// first-class cursor API yet, so we call into AppKit directly. Push/
/// pop semantics are safe for nested hover regions.
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
    /// view. Matches the hyperlink affordance — signals "this clicks."
    func pointingHandCursor() -> some View {
        modifier(PointingHandCursorModifier())
    }
}
