import SwiftUI

public struct ConversationView: View {
    @ObservedObject var viewModel: ConversationViewModel
    @State private var inputText: String = ""
    @FocusState private var inputFocused: Bool

    public var body: some View {
        VStack(spacing: 0) {
            messageList
            Divider()
            promptInput
        }
        .task {
            if viewModel.messages.isEmpty && viewModel.state == .idle {
                await viewModel.loadHistory()
            }
        }
    }

    private var messageList: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    ForEach(Array(viewModel.messages.enumerated()), id: \.offset) { index, msg in
                        MessageBubble(message: msg)
                            .id(index)
                    }

                    // In-flight assistant text while streaming.
                    if !viewModel.inFlightText.isEmpty {
                        inFlightBubble
                            .id("inflight")
                    }
                }
                .padding()
            }
            .onChange(of: viewModel.messages.count) {
                withAnimation {
                    proxy.scrollTo(viewModel.messages.count - 1, anchor: .bottom)
                }
            }
            .onChange(of: viewModel.inFlightText) {
                withAnimation {
                    proxy.scrollTo("inflight", anchor: .bottom)
                }
            }
        }
    }

    private var inFlightBubble: some View {
        HStack {
            Text(viewModel.inFlightText)
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
                Button("Stop") { viewModel.cancel() }
            } else {
                Button("Send") { sendMessage() }
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
