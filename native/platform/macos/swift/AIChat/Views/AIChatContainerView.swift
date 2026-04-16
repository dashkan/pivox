import SwiftUI

/// Top-level container for the AI Chat feature. Shows a conversation list
/// on the left and the active conversation on the right.
struct AIChatContainerView: View {
    @State private var selectedConversation: Pivox_Ai_V1_Conversation?
    @State private var client: ChatClient?
    @State private var initError: String?

    // TODO: Resolve from authenticated user's org membership.
    private let orgName = "local-corp"
    private let endpoint = "localhost:50051"

    var body: some View {
        Group {
            if let client {
                chatNavigation(client: client)
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

    private func chatNavigation(client: ChatClient) -> some View {
        NavigationSplitView {
            let vm = ConversationListViewModel(client: client, orgName: orgName)
            ConversationListView(viewModel: vm) { conv in
                selectedConversation = conv
            }
            .navigationTitle("Conversations")
        } detail: {
            if let conv = selectedConversation {
                let vm = ConversationViewModel(
                    client: client,
                    conversationName: conv.name
                )
                ConversationView(viewModel: vm)
                    .navigationTitle(conv.title.isEmpty ? "Chat" : conv.title)
                    .id(conv.name) // Reset view when switching conversations.
            } else {
                Text("Select a conversation")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }
}
