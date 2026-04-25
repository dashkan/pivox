import SwiftUI

/// Inline (docked) variant of the AI chat surface. Wraps
/// `AIChatContainerView` with an opaque content background — the
/// main window itself is `isOpaque = false` so the sidebar can
/// bleed desktop wallpaper through, and the chat panel needs to
/// opt out of that or the wallpaper shows through it too.
///
/// The detach button lives inside the panel's existing header
/// (see `AIChatPanel.body`), conditional on
/// `AIChatState.shared.mode == .docked`. There's no separate
/// header strip here.
struct InlineAIChatPanel: View {
    var body: some View {
        AIChatContainerView()
            .background(Color(nsColor: .windowBackgroundColor))
    }
}
