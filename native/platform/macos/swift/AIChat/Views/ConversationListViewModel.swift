import Foundation
import SwiftUI

@MainActor
public final class ConversationListViewModel: ObservableObject {
    @Published public var conversations: [Pivox_Ai_V1_Conversation] = []
    @Published public var state: ListState = .idle

    private let client: ChatClient
    private let orgName: String

    public enum ListState: Equatable {
        case idle
        case loading
        case loaded
        case error(String)

        public static func == (lhs: ListState, rhs: ListState) -> Bool {
            switch (lhs, rhs) {
            case (.idle, .idle), (.loading, .loading), (.loaded, .loaded):
                return true
            case (.error(let a), .error(let b)):
                return a == b
            default:
                return false
            }
        }
    }

    public init(client: ChatClient, orgName: String) {
        self.client = client
        self.orgName = orgName
    }

    public func load() async {
        state = .loading
        do {
            let request = Pivox_Ai_V1_ListConversationsRequest.with {
                $0.parent = "organizations/\(orgName)"
                $0.pageSize = 50
            }
            let response = try await client.listConversations(request)
            conversations = response.conversations
            state = .loaded
        } catch {
            state = .error(error.localizedDescription)
        }
    }

    public func create(title: String = "") async throws -> Pivox_Ai_V1_Conversation {
        let request = Pivox_Ai_V1_CreateConversationRequest.with {
            $0.parent = "organizations/\(orgName)"
            $0.conversation = Pivox_Ai_V1_Conversation.with {
                $0.title = title
            }
        }
        let conv = try await client.createConversation(request)
        conversations.insert(conv, at: 0)
        return conv
    }

    public func delete(name: String) async throws {
        let request = Pivox_Ai_V1_DeleteConversationRequest.with {
            $0.name = name
        }
        try await client.deleteConversation(request)
        conversations.removeAll { $0.name == name }
    }

    public func archive(name: String) async throws {
        let request = Pivox_Ai_V1_UpdateConversationRequest.with {
            $0.conversation = Pivox_Ai_V1_Conversation.with {
                $0.name = name
                $0.archived = true
            }
            $0.updateMask = .with { $0.paths = ["archived"] }
        }
        let updated = try await client.updateConversation(request)
        if let idx = conversations.firstIndex(where: { $0.name == name }) {
            conversations[idx] = updated
        }
    }
}
