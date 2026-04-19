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
        ScrollViewReader { proxy in
            scrollViewBody(proxy: proxy)
        }
    }

    @ViewBuilder
    private func scrollViewBody(proxy: ScrollViewProxy) -> some View {
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
                    Message(
                        message: msg,
                        pinActions: isLastAssistant(msg),
                        onEditSubmit: { editedText in
                            // Edit-and-resubmit: send as a new turn.
                            // Old user message stays; new exchange
                            // appends below. Proper history rewrite /
                            // branch semantics requires BE support —
                            // not in v1.
                            viewModel.send(text: editedText)
                        },
                        onRegenerate: regenerateHandler(for: msg)
                    )
                    .id(msg.name)
                }

                // In-flight assistant text while streaming. Use the
                // streaming variant so partial markdown (unclosed fences,
                // lists, emphasis) renders cleanly mid-generation.
                if !viewModel.inFlightText.isEmpty {
                    InFlightAssistantMessage(text: viewModel.inFlightText)
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
        // ⌘↓ / ⌘↑ jump to the latest / oldest message. ⌘↑ lands on
        // the oldest-loaded message, which keeps the load-older
        // sentinel on-screen and pulls in earlier pages as they
        // arrive — effectively progressive auto-load up to the start
        // of the conversation. Matches macOS's native "end/start of
        // document" mnemonics.
        .background(
            Group {
                Button { scrollToBottom(proxy) } label: { EmptyView() }
                    .keyboardShortcut(.downArrow, modifiers: .command)
                Button { scrollToTop(proxy) } label: { EmptyView() }
                    .keyboardShortcut(.upArrow, modifiers: .command)
            }
            .opacity(0)
            .accessibilityHidden(true)
        )
    }

    /// Scroll to the last message, or the in-flight bubble if present.
    private func scrollToBottom(_ proxy: ScrollViewProxy) {
        let targetID: String
        if !viewModel.inFlightText.isEmpty {
            targetID = "inflight"
        } else if let last = viewModel.messages.last?.name {
            targetID = last
        } else {
            return
        }
        withAnimation(.easeOut(duration: 0.18)) {
            proxy.scrollTo(targetID, anchor: .bottom)
        }
    }

    /// Scroll to the oldest loaded message. The load-older sentinel
    /// sits above it; when the sentinel appears it fires loadOlder(),
    /// which prepends a page and keeps the sentinel in view — so
    /// repeated scroll-to-top calls naturally walk back through the
    /// conversation without needing a bespoke pagination loop.
    private func scrollToTop(_ proxy: ScrollViewProxy) {
        guard let first = viewModel.messages.first?.name else { return }
        withAnimation(.easeOut(duration: 0.18)) {
            proxy.scrollTo(first, anchor: .top)
        }
    }

    /// Top-of-list sentinel that triggers older-page fetches via onAppear.
    /// No manual scroll preservation — .defaultScrollAnchor(.bottom) on
    /// the parent ScrollView keeps the user's current read position
    /// stable as older messages are prepended above.
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
        .onAppear {
            // Defer by a tick so the NavigationSplitView's default-focus
            // heuristics (which otherwise land on the panel's first
            // focusable control — the history button) have already run
            // by the time we claim focus for the input.
            DispatchQueue.main.async { inputFocused = true }
        }
    }

    private func sendMessage() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        inputText = ""
        viewModel.send(text: text)
    }

    /// True if this assistant message is the most recent assistant turn
    /// in the conversation. Used to pin its action row visible while
    /// earlier assistants fall back to hover-reveal, keeping scrollback
    /// visually quiet.
    private func isLastAssistant(_ msg: Pivox_Ai_V1_Message) -> Bool {
        guard msg.role == .assistant else { return false }
        return viewModel.messages.last(where: { $0.role == .assistant })?.name == msg.name
    }

    /// Returns a redo closure for an assistant message, or nil if no
    /// preceding user turn is available. Walks backward from the
    /// assistant message to find the nearest user message, then kicks
    /// off a new send with its text. Old history stays put — this is a
    /// new turn, not a history rewrite.
    private func regenerateHandler(for msg: Pivox_Ai_V1_Message) -> (() -> Void)? {
        guard msg.role == .assistant else { return nil }
        guard let idx = viewModel.messages.firstIndex(where: { $0.name == msg.name }),
              let priorUser = viewModel.messages[..<idx].reversed().first(where: { $0.role == .user })
        else { return nil }
        let text = priorUser.parts.compactMap { part -> String? in
            if case .text(let tp) = part.part { return tp.text }
            return nil
        }.joined()
        guard !text.isEmpty else { return nil }
        return { viewModel.send(text: text) }
    }
}

