import SwiftUI

/// Top-level container for the AI Chat feature. Shows a conversation list
/// on the left and the active conversation on the right.
struct AIChatContainerView: View {
    @State private var client: ChatClient?
    @State private var initError: String?

    // TODO: Resolve from authenticated user's org membership.
    private let orgName = "local-corp"
    private let endpoint = "localhost:50051"

    var body: some View {
        Group {
            if let client {
                AIChatNavigationView(client: client, orgName: orgName)
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

/// Inner view that owns the view models via @StateObject.
struct AIChatNavigationView: View {
    let client: ChatClient
    let orgName: String
    @StateObject private var listViewModel: ConversationListViewModel
    @State private var selectedConversation: Pivox_Ai_V1_Conversation?

    init(client: ChatClient, orgName: String) {
        self.client = client
        self.orgName = orgName
        _listViewModel = StateObject(wrappedValue: ConversationListViewModel(client: client, orgName: orgName))
    }

    var body: some View {
        NavigationSplitView {
            ConversationListView(viewModel: listViewModel) { conv in
                selectedConversation = conv
            }
            .navigationTitle("Conversations")
        } detail: {
            if let conv = selectedConversation {
                ConversationView(
                    viewModel: ConversationViewModel(
                        client: client,
                        conversationName: conv.name
                    )
                )
                .navigationTitle(conv.title.isEmpty ? "Chat" : conv.title)
                .id(conv.name)
            } else {
                Text("Select a conversation")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }
}
