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
        HStack(spacing: 8) {
            ShimmerPromptField(
                text: $inputText,
                placeholder: "Message...",
                isEnabled: true,
                onSubmit: sendMessage,
                focused: $inputFocused)

            if viewModel.state == .streaming {
                IconButton(systemName: "stop.circle.fill", label: "Stop", help: "Stop") {
                    viewModel.cancel()
                }
            } else {
                IconButton(systemName: "paperplane.fill", label: "Send", help: "Send") {
                    sendMessage()
                }
                .disabled(inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
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
