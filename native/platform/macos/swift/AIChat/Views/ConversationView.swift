import PivoxModels
import SwiftUI

public struct ConversationView: View {
    @ObservedObject var viewModel: ConversationViewModel
    @State private var inputText: String = ""
    @FocusState private var inputFocused: Bool

    public var body: some View {
        VStack(spacing: 0) {
            ZStack {
                messageList
                loadingOverlay
                errorOverlay
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

    private var messageList: some View {
        ScrollView {
            // Eager VStack — LazyVStack + .defaultScrollAnchor(.bottom) is
            // unreliable because the anchor resolves before lazy rows are
            // materialized and the scroll lands in a height-unknown gap.
            // Text bubbles are cheap; eager layout is fine up to a few
            // hundred messages.
            VStack(alignment: .leading, spacing: 12) {
                if viewModel.canLoadOlder {
                    loadOlderSentinel
                }

                ForEach(viewModel.messages, id: \.name) { msg in
                    MessageBubble(message: msg)
                        .id(msg.name)
                }

                // In-flight assistant text while streaming.
                if !viewModel.inFlightText.isEmpty {
                    inFlightBubble
                        .id("inflight")
                }
            }
            .padding()
        }
        // Chat semantics — content-size changes keep the bottom edge anchored
        // to the viewport bottom. Initial load lands at bottom; append follows
        // the new tail; prepend keeps the user reading the same message in the
        // same viewport position.
        .defaultScrollAnchor(.bottom)
    }

    /// Top-of-list sentinel that triggers older-page fetches via onAppear.
    /// No manual scroll preservation — .defaultScrollAnchor(.bottom) on the
    /// parent ScrollView keeps the user's current read position stable as
    /// older messages are prepended above.
    private var loadOlderSentinel: some View {
        HStack {
            Spacer()
            ProgressView()
                .controlSize(.small)
            Spacer()
        }
        .padding(.vertical, 6)
        .onAppear {
            Task { await viewModel.loadOlder() }
        }
    }

    private var inFlightBubble: some View {
        HStack {
            Text(viewModel.inFlightText)
                .textSelection(.enabled)
                .padding(10)
                .background(Color.secondary.opacity(0.1))
                .clipShape(RoundedRectangle(cornerRadius: 8))
            Spacer()
        }
    }

    private var promptInput: some View {
        HStack(spacing: 8) {
            TextField("Message...", text: $inputText)
                .textFieldStyle(.roundedBorder)
                .focused($inputFocused)
                .onSubmit { sendMessage() }

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
        .onAppear { inputFocused = true }
    }

    private func sendMessage() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        inputText = ""
        viewModel.send(text: text)
    }
}

struct MessageBubble: View {
    let message: Pivox_Ai_V1_Message

    var body: some View {
        let isUser = message.role == .user
        HStack {
            if isUser { Spacer() }
            VStack(alignment: isUser ? .trailing : .leading, spacing: 4) {
                Text(textContent)
                    .textSelection(.enabled)
                    .padding(10)
                    .background(isUser
                        ? Color.accentColor.opacity(0.15)
                        : Color.secondary.opacity(0.1))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
            }
            if !isUser { Spacer() }
        }
    }

    private var textContent: String {
        message.parts.compactMap { part in
            if case .text(let tp) = part.part {
                return tp.text
            }
            return nil
        }.joined()
    }
}
