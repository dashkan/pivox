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
        HStack(spacing: 8) {
            Image(systemName: "sparkles")
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(.secondary)
                .symbolEffect(.pulse, options: .repeating)
                // Always-on AI shimmer — same effect the toolbar
                // sparkles uses on hover, but here it runs the
                // whole time the thinking indicator is visible to
                // signal "AI is doing work right now."
                .aiShimmerSymbol(isActive: true)

            Text(phases[min(phaseIndex, phases.count - 1)])
                .font(.system(size: 13))
                .foregroundStyle(
                    LinearGradient(
                        stops: [
                            .init(color: .secondary, location: max(0, shimmerPhase - 0.2)),
                            .init(color: .primary, location: shimmerPhase),
                            .init(color: .secondary, location: min(1, shimmerPhase + 0.2)),
                        ],
                        startPoint: .leading,
                        endPoint: .trailing))

            Spacer()
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
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
