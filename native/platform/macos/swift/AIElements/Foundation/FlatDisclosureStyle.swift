import SwiftUI

/// A flat disclosure group style matching the ai-elements pattern:
/// row with chevron rotation, no indentation, subtle separator.
/// Used by Reasoning, Tool, Task, Sandbox, Agent, Sources, StackTrace, FileTree.
public struct FlatDisclosureStyle: DisclosureGroupStyle {
    @Environment(\.pivoxTheme) private var theme

    public func makeBody(configuration: Configuration) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    configuration.isExpanded.toggle()
                }
            } label: {
                HStack(spacing: theme.spacingSM) {
                    Image(systemName: "chevron.right")
                        .font(.caption)
                        .foregroundStyle(theme.textTertiary)
                        .rotationEffect(
                            .degrees(configuration.isExpanded ? 90 : 0))

                    configuration.label
                        .font(theme.bodyFont)
                        .foregroundStyle(theme.textPrimary)

                    Spacer()
                }
                .padding(.vertical, theme.spacingSM)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if configuration.isExpanded {
                configuration.content
                    .padding(.leading, theme.spacingLG)
                    .padding(.bottom, theme.spacingSM)
            }
        }
    }
}

extension DisclosureGroupStyle where Self == FlatDisclosureStyle {
    /// The AIElements flat disclosure style with chevron rotation.
    public static var flat: FlatDisclosureStyle { FlatDisclosureStyle() }
}
