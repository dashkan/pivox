import SwiftUI

/// Inline (docked) variant of the AI chat surface. Just wraps
/// `AIChatContainerView` — backdrop styling is the *parent's*
/// responsibility, not this view's:
///
///   - Push layout (`ContentView.pushLayout`) wraps us with an
///     opaque `Color(nsColor: .windowBackgroundColor)` so the
///     wallpaper bleed (which the main window enables for the
///     sidebar) stops at the panel edge.
///   - Float layout (`ContentView.floatLayout`) wraps us with
///     `.thinMaterial` so canvas content bleeds through.
///
/// Setting an opaque background here would paint on top of the
/// float layout's material and defeat the Liquid Glass effect.
///
/// The detach button lives inside the panel's existing header
/// (see `AIChatPanel.body`), conditional on
/// `AIChatState.shared.mode == .docked`. There's no separate
/// header strip here.
struct InlineAIChatPanel: View {
    var body: some View {
        AIChatContainerView()
    }
}
