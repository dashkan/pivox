import Foundation
import PivoxModels
import SwiftUI

@MainActor
public final class ConversationListViewModel: ObservableObject {
    @Published public var conversations: [Pivox_Ai_V1_Conversation] = []
    @Published public var state: ListState = .idle
    @Published public var isLoadingMore: Bool = false

    private let client: ChatClient
    private let orgName: String
    private var nextPageToken: String = ""
    private var hasMore: Bool = true

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

    /// Loads the first page, resetting any pagination state.
    public func load() async {
        state = .loading
        nextPageToken = ""
        hasMore = true
        conversations = []
        await fetchNextPage(replace: true)
    }

    /// Fetches the next page if one is available. Safe to call repeatedly —
    /// concurrent calls are coalesced by the `isLoadingMore` guard.
    public func loadMore() async {
        guard hasMore, !isLoadingMore, state != .loading else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        await fetchNextPage(replace: false)
    }

    private func fetchNextPage(replace: Bool) async {
        do {
            let request = Pivox_Ai_V1_ListConversationsRequest.with {
                $0.parent = "organizations/\(orgName)"
                $0.pageSize = 50
                if !nextPageToken.isEmpty { $0.pageToken = nextPageToken }
            }
            let response = try await client.listConversations(request)
            if replace {
                conversations = response.conversations
            } else {
                conversations.append(contentsOf: response.conversations)
            }
            nextPageToken = response.nextPageToken
            hasMore = !response.nextPageToken.isEmpty
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

    /// Renames a conversation (title only). Server sets update_time and etag.
    public func rename(name: String, newTitle: String) async throws {
        let request = Pivox_Ai_V1_UpdateConversationRequest.with {
            $0.conversation = Pivox_Ai_V1_Conversation.with {
                $0.name = name
                $0.title = newTitle
            }
            $0.updateMask = .with { $0.paths = ["title"] }
        }
        let updated = try await client.updateConversation(request)
        if let idx = conversations.firstIndex(where: { $0.name == name }) {
            conversations[idx] = updated
        }
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
