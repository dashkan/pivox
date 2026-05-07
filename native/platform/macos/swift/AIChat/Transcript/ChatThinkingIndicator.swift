import SwiftUI

/// "Working on it…" placeholder shown while the chat is
/// streaming but the assistant hasn't produced any text yet.
/// Cycles through reassurance copy on a slow timer so a long
/// silence doesn't read as a frozen UI.
///
/// The Claude / ChatGPT conventions — gradient shimmer + rotating
/// status text — are the visual reference. Copy escalates from
/// "Thinking" to "Still thinking" to "Almost there" to give a
/// rough sense of elapsed time without showing a precise clock
/// (which can read as a deadline).
struct ChatThinkingIndicator: View {
    @Environment(\.pivoxTheme) private var theme
    @State private var phaseIndex: Int = 0
    @State private var shimmerPhase: CGFloat = 0

    private let phases: [String] = [
        "Thinking…",
        "Still thinking…",
        "Almost there…",
        "Hang tight, this one's a tough one…",
    ]

    /// Seconds between each phrase escalation. After the last
    /// phrase we stop advancing — there's no informative phrase
    /// after "Hang tight".
    private static let phaseInterval: TimeInterval = 8

    var body: some View {
        HStack(spacing: theme.spacingSM) {
            // No explicit `.foregroundStyle` here — `aiShimmerSymbol`
            // owns the color, applying an animated AngularGradient.
            // Setting a concrete Color first would win the priority
            // contest in SwiftUI and mask the rainbow effect (the
            // earlier version used `.secondary` which is a
            // HierarchicalShapeStyle and could be overridden; a
            // concrete Color from the theme can't).
            Image(systemName: "sparkles")
                .font(theme.iconInline)
                .symbolEffect(.pulse, options: .repeating)
                .aiShimmerSymbol(isActive: true)

            Text(phases[min(phaseIndex, phases.count - 1)])
                .font(theme.bodySmallFont)
                .foregroundStyle(
                    LinearGradient(
                        stops: [
                            .init(color: theme.textSecondary, location: max(0, shimmerPhase - 0.2)),
                            .init(color: theme.textPrimary, location: shimmerPhase),
                            .init(color: theme.textSecondary, location: min(1, shimmerPhase + 0.2)),
                        ],
                        startPoint: .leading,
                        endPoint: .trailing))

            Spacer()
        }
        .padding(.horizontal, theme.spacingLG)
        .padding(.vertical, theme.spacingSM)
        .onAppear { startAnimating() }
    }

    private func startAnimating() {
        withAnimation(.linear(duration: 1.4).repeatForever(autoreverses: false)) {
            shimmerPhase = 1.0
        }
        Task {
            // Phase escalation. Each tick advances the phrase
            // until we hit the last one, then stops — no point in
            // looping back through "Thinking…" while the user is
            // already 30 seconds in.
            while phaseIndex < phases.count - 1 {
                try? await Task.sleep(for: .seconds(Self.phaseInterval))
                guard !Task.isCancelled else { return }
                phaseIndex += 1
            }
        }
    }
}
