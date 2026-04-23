import SwiftUI

/// Chat-style text input wearing the AI-Intelligence shimmer border.
/// Plain TextField inside a materialized capsule with an animated
/// gradient stroke overlay — ambient (low intensity) by default,
/// brighter when the field is focused.
///
/// Callers inject their own `@FocusState` via `FocusState.Binding`
/// so they can still drive focus programmatically (e.g. the
/// `⌘⇧A` hotkey focusing the chat input when it opens the panel).
struct ShimmerPromptField: View {
    @Binding var text: String
    let placeholder: String
    let isEnabled: Bool
    let onSubmit: () -> Void
    var focused: FocusState<Bool>.Binding

    /// Capsule corner radius — matches native macOS capsule inputs.
    private let cornerRadius: CGFloat = 18

    var body: some View {
        TextField(placeholder, text: $text)
            .textFieldStyle(.plain)
            .focused(focused)
            .disabled(!isEnabled)
            .onSubmit(onSubmit)
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .background(
                Capsule(style: .continuous)
                    .fill(.thinMaterial)
            )
            .aiShimmer(
                shape: Capsule(style: .continuous),
                // Ambient glow at rest (~35%), stronger when focused.
                // The shift happens via implicit animation on
                // intensity.
                intensity: focused.wrappedValue ? 0.95 : 0.35,
                lineWidth: 1.5
            )
            .animation(.easeInOut(duration: 0.25), value: focused.wrappedValue)
    }
}
