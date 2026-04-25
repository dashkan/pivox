import AppKit
import SwiftUI

/// Floating "Jump to latest" capsule shown when the user has
/// scrolled away from the bottom of the transcript. Click → posts
/// `.aiChatJumpToLatest`, which the transcript coordinator
/// handles by re-engaging stick-to-bottom and snapping to the
/// latest content.
///
/// Faded by default, full opacity + slight scale on hover, with
/// the pointing-hand cursor so it reads as a clickable affordance
/// at a glance. Same Slack / Discord pattern.
struct JumpToLatestPill: View {
    @Environment(\.pivoxTheme) private var theme
    @State private var hovered = false

    var body: some View {
        Button {
            NotificationCenter.default.post(
                name: .aiChatJumpToLatest, object: nil)
        } label: {
            HStack(spacing: 6) {
                Image(systemName: "arrow.down")
                Text("Jump to latest")
            }
            .font(.system(size: 12, weight: .semibold))
            .foregroundStyle(.white)
            .padding(.horizontal, 14)
            .padding(.vertical, 7)
            .background(theme.accent, in: Capsule())
            .overlay(
                Capsule()
                    .strokeBorder(Color.white.opacity(0.15), lineWidth: 0.5))
            .shadow(color: .black.opacity(hovered ? 0.30 : 0.18),
                    radius: hovered ? 6 : 4, y: 2)
            .scaleEffect(hovered ? 1.04 : 1.0)
        }
        .buttonStyle(.plain)
        .opacity(hovered ? 1.0 : 0.55)
        .animation(.easeOut(duration: 0.12), value: hovered)
        // `onContinuousHover` over `onHover` here because the pill
        // floats above selectable transcript text. The text view
        // sets the I-beam on every mouse move within its tracking
        // area, which would overwrite a one-shot cursor change.
        // Re-asserting on each phase update keeps the right cursor
        // stable while hovered.
        //
        // Arrow cursor — not pointing-hand. macOS HIG reserves the
        // pointing hand for link-like elements inside text (the
        // web-style "cursor: pointer" pattern is a web convention,
        // not a Mac one). Standard buttons stay on the arrow
        // cursor; we just need to override the I-beam from the
        // text behind the pill.
        .onContinuousHover { phase in
            switch phase {
            case .active:
                hovered = true
                NSCursor.arrow.set()
            case .ended:
                hovered = false
            }
        }
    }
}
