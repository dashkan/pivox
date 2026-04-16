import SwiftUI

/// Right-side chat panel. Opens straight to a new conversation.
/// Toggle via the toolbar button or ⌘⇧A.
struct AIChatContainerView: View {
    @State private var client: ChatClient?
    @State private var initError: String?

    // TODO: Resolve from authenticated user's org membership.
    private let orgName = "local-corp"
    private let endpoint = "localhost:50051"

    var body: some View {
        Group {
            if let client {
                AIChatPanel(client: client, orgName: orgName)
            } else if let initError {
                Text("Chat unavailable: \(initError)")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ProgressView("Connecting...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .task {
            do {
                let token = AppStateBridge.shared().loadSecure(forKey: "firebase_id_token") ?? ""
                client = try ChatClient(endpoint: endpoint, authToken: token)
            } catch {
                initError = error.localizedDescription
            }
        }
    }
}

/// The chat panel: starts with a new conversation, can switch to history.
struct AIChatPanel: View {
    let client: ChatClient
    let orgName: String

    @State private var conversationName: String?
    @State private var showHistory = false

    var body: some View {
        VStack(spacing: 0) {
            // Header bar
            HStack {
                Button {
                    showHistory.toggle()
                } label: {
                    Image(systemName: "clock.arrow.circlepath")
                }
                .buttonStyle(.plain)
                .help("Conversation history")

                Spacer()

                Text(conversationName != nil ? "Chat" : "New Chat")
                    .font(.headline)

                Spacer()

                Button {
                    conversationName = nil
                } label: {
                    Image(systemName: "plus")
                }
                .buttonStyle(.plain)
                .help("New conversation")
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)

            Divider()

            if showHistory {
                let listVM = ConversationListViewModel(client: client, orgName: orgName)
                ConversationListView(viewModel: listVM) { conv in
                    conversationName = conv.name
                    showHistory = false
                }
            } else if let name = conversationName {
                // Wrap in a keyed container so @StateObject resets on name change.
                ActiveConversationView(client: client, conversationName: name)
                    .id(name)
            } else {
                NewChatView(client: client, orgName: orgName) { name in
                    conversationName = name
                }
            }
        }
        .background(.background)
    }
}

/// Owns the ConversationViewModel via @StateObject so SwiftUI observes @Published changes.
/// Keyed by conversation name via .id() in the parent — new name = new view + new VM.
struct ActiveConversationView: View {
    @StateObject private var viewModel: ConversationViewModel

    init(client: ChatClient, conversationName: String) {
        _viewModel = StateObject(wrappedValue: ConversationViewModel(
            client: client, conversationName: conversationName))
    }

    var body: some View {
        ConversationView(viewModel: viewModel)
    }
}

/// The initial view when no conversation exists — just a prompt input.
struct NewChatView: View {
    let client: ChatClient
    let orgName: String
    let onCreated: (String) -> Void

    @State private var inputText = ""
    @State private var isCreating = false

    var body: some View {
        VStack {
            Spacer()

            Image(systemName: "bubble.left.and.text.bubble.right")
                .font(.system(size: 36))
                .foregroundStyle(.tertiary)
                .padding(.bottom, 8)

            Text("Start a conversation")
                .font(.title3)
                .foregroundStyle(.secondary)

            Spacer()

            // Prompt input at the bottom
            HStack(spacing: 8) {
                TextField("Message...", text: $inputText, axis: .vertical)
                    .textFieldStyle(.plain)
                    .lineLimit(1...5)
                    .onSubmit { sendFirst() }

                Button {
                    sendFirst()
                } label: {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.title2)
                }
                .buttonStyle(.plain)
                .disabled(inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isCreating)
            }
            .padding(12)
        }
    }

    private func sendFirst() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !isCreating else { return }
        isCreating = true

        Task {
            do {
                // Create the conversation on the server.
                let request = Pivox_Ai_V1_CreateConversationRequest.with {
                    $0.parent = "organizations/\(orgName)"
                    $0.conversation = Pivox_Ai_V1_Conversation.with {
                        $0.title = String(text.prefix(50))
                    }
                }
                let conv = try await client.createConversation(request)
                inputText = ""
                onCreated(conv.name)

                // TODO: Send the first message to the conversation via the bidi stream.
                // For now, the conversation is created and the user can type in the
                // ConversationView which handles sending.
            } catch {
                isCreating = false
            }
        }
    }
}
