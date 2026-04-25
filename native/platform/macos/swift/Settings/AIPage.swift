import SwiftUI

/// Settings page for AI behaviors. Currently scoped to chat-panel
/// layout; intended to grow to host other AI-related preferences
/// (model defaults, system instruction, telemetry, etc.) without
/// each one needing its own top-level Settings tab.
struct AIPage: View {
    @Environment(\.pivoxTheme) private var theme
    /// Direct binding to the shared `@Observable` state. Picker
    /// mutations write through immediately and external changes
    /// (e.g., a future imported preferences flow) flow back into
    /// the picker without a manual mirror.
    @Bindable private var aiChatState = AIChatState.shared

    var body: some View {
        Form {
            Section {
                Picker("Layout", selection: $aiChatState.layoutMode) {
                    Text("Floating").tag(AIChatState.LayoutMode.float)
                    Text("Side panel").tag(AIChatState.LayoutMode.push)
                }
                .pickerStyle(.segmented)

                Text(layoutDescription)
                    .font(theme.captionFont)
                    .foregroundStyle(theme.textSecondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.top, 2)
            } header: {
                Text("Chat panel")
            } footer: {
                Text("Floating panels feel modern and don't crowd the canvas. " +
                     "Side panels keep the chat and your work fully visible side-by-side.")
                    .font(theme.captionFont)
                    .foregroundStyle(theme.textTertiary)
            }
        }
        .formStyle(.grouped)
        .frame(width: 640)
    }

    private var layoutDescription: String {
        switch aiChatState.layoutMode {
        case .float:
            return "The chat panel overlays the right side of the window over " +
                   "translucent material. Your canvas keeps its full width."
        case .push:
            return "The chat panel sits in a fixed column on the right. Your " +
                   "canvas resizes to make room when the chat is open."
        }
    }
}
