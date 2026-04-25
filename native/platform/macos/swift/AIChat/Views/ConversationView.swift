import PivoxModels
import SwiftUI

public struct ConversationView: View {
    @ObservedObject var viewModel: ConversationViewModel
    @State private var inputText: String = ""
    @FocusState private var inputFocused: Bool

    public var body: some View {
        VStack(spacing: 0) {
            ZStack {
                ConversationTranscriptView(viewModel: viewModel)
                loadingOverlay
                errorOverlay
                // Jump-to-latest pill, anchored to the bottom of
                // the transcript region. Visible whenever the user
                // has scrolled away from the bottom — same pattern
                // Slack / Discord / Claude use.
                if !viewModel.stickToBottom {
                    VStack {
                        Spacer()
                        JumpToLatestPill()
                            .padding(.bottom, 12)
                            .transition(.opacity.combined(with: .move(edge: .bottom)))
                    }
                }

                // Hidden keyboard shortcuts. Buttons in a 0-opacity,
                // accessibility-hidden background so they're in the
                // responder chain only when this view is — i.e.
                // the chat is focused, not globally fighting the
                // main window's shortcuts. Mirrors the pattern the
                // pre-AppKit transcript used.
                shortcuts
            }
            // Shimmer placeholder shown only during the gap between
            // "user sent" and "first streaming delta arrives". Once
            // tokens start, the streaming response is rendered as
            // a normal message row in the transcript above (the
            // view model appends a placeholder on the first delta
            // and updates its text in place), so no separate
            // streaming container is needed here.
            if viewModel.state == .streaming && viewModel.inFlightText.isEmpty {
                ChatThinkingIndicator()
                    .transition(.opacity)
            }
            Divider()
            promptInput
        }
        .task {
            if viewModel.messages.isEmpty && viewModel.state == .idle {
                await viewModel.loadHistory()
            }
        }
    }

    /// Invisible buttons that exist solely to host keyboard
    /// shortcuts. SwiftUI's `.keyboardShortcut` only fires when the
    /// hosting view is in the responder chain, which means these
    /// shortcuts are scoped to "AI chat is focused" — they don't
    /// shadow shortcuts the main window also defines.
    ///
    /// Both shortcuts are `.disabled` while the prompt input is
    /// focused. macOS's standard ⌘↑ / ⌘↓ in a text field move the
    /// caret to document start / end; we don't want to shadow
    /// those when the user is actively editing a multi-line draft.
    /// Once focus leaves the input (clicking the transcript, ESC,
    /// etc.), the shortcuts re-enable for transcript navigation.
    private var shortcuts: some View {
        ZStack {
            // ⌘↓: jump to latest. Reuses the pill's notification so
            // there's one path for "snap to bottom and re-engage
            // stick-to-bottom."
            Button {
                NotificationCenter.default.post(
                    name: .aiChatJumpToLatest, object: nil)
            } label: { EmptyView() }
                .buttonStyle(.plain)
                .keyboardShortcut(.downArrow, modifiers: .command)
                .disabled(inputFocused)

            // ⌘↑: scroll to top of currently-loaded content. The
            // existing `contentViewBoundsDidChange` observer fires
            // one `loadOlder()` (latched) when origin reaches the
            // top, so repeated ⌘↑ traverses one page per press
            // until the conversation runs out of older history.
            Button {
                NotificationCenter.default.post(
                    name: .aiChatScrollUp, object: nil)
            } label: { EmptyView() }
                .buttonStyle(.plain)
                .keyboardShortcut(.upArrow, modifiers: .command)
                .disabled(inputFocused)

            // Esc cancels the in-flight stream. Handled inside
            // `ShimmerPromptField`'s `.onKeyPress(.escape)` so the
            // composer wins the responder race when the input has
            // focus (a sibling Button-level shortcut would lose
            // to NSTextView's `cancelOperation:`). When the input
            // doesn't have focus, ⌘. is the documented alternative
            // — but for now Esc-cancel scoped to the input is the
            // primary path, since the user is almost always
            // focused on the composer when they want to interrupt.
        }
        .opacity(0)
        .accessibilityHidden(true)
    }

    @ViewBuilder
    private var loadingOverlay: some View {
        if viewModel.state == .loading && viewModel.messages.isEmpty {
            ProgressView()
                .controlSize(.regular)
        }
    }

    @ViewBuilder
    private var errorOverlay: some View {
        if case .error(let msg) = viewModel.state, viewModel.messages.isEmpty {
            VStack(spacing: 6) {
                Image(systemName: "exclamationmark.triangle")
                    .font(.title2)
                    .foregroundStyle(.orange)
                Text("Couldn't load messages")
                    .font(.subheadline)
                Text(msg)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                Button("Retry") {
                    Task { await viewModel.loadHistory() }
                }
                .buttonStyle(.borderless)
            }
            .padding()
        }
    }

    private var promptInput: some View {
        ShimmerPromptField(
            text: $inputText,
            placeholder: "Message...",
            isEnabled: true,
            isStreaming: viewModel.state == .streaming,
            onSubmit: sendMessage,
            onCancel: { viewModel.cancel() },
            focused: $inputFocused,
            toolItems: { ChatAttachmentMenuButton() })
            .padding(12)
            .onReceive(NotificationCenter.default.publisher(for: .aiChatFocusRequested)) { _ in
                DispatchQueue.main.async { inputFocused = true }
            }
    }

    private func sendMessage() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        inputText = ""
        viewModel.send(text: text)
    }
}
