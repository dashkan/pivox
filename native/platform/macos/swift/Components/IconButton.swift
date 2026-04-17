import SwiftUI

/// Compact icon-only button with adequate padding, a visible hover
/// background, and a keyboard focus ring that reads cleanly. Replaces the
/// ad-hoc `Button { ... } label: { Image(systemName: ...) }.buttonStyle(.plain)`
/// pattern scattered across the AI chat UI.
///
/// Accessibility:
///  * Requires a descriptive `label` — applied as `accessibilityLabel`.
///  * `help` is shown as a tooltip on hover for pointer users.
///
/// Sizing: default 13pt glyph with ~28pt square hit target. Pass a larger
/// `size` when the button needs more presence.
public struct IconButton: View {
    let systemName: String
    let label: String
    let help: String?
    let role: ButtonRole?
    let size: CGFloat
    let action: () -> Void

    @State private var isHovered = false

    public init(
        systemName: String,
        label: String,
        help: String? = nil,
        role: ButtonRole? = nil,
        size: CGFloat = 13,
        action: @escaping () -> Void
    ) {
        self.systemName = systemName
        self.label = label
        self.help = help
        self.role = role
        self.size = size
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
                        .fill(isHovered ? Color.secondary.opacity(0.12) : .clear)
                )
        }
        .buttonStyle(.plain)
        .help(help ?? label)
        .accessibilityLabel(label)
        .onHover { isHovered = $0 }
    }
}
