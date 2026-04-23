import Foundation
import PivoxModels
@testable import Pivox

/// Mock ChatClient for unit testing view models without a network connection.
final class MockChatClient: ChatClientProtocol, @unchecked Sendable {

    // MARK: - Stream

    /// Events to yield from stream(), consumed in order.
    var streamEvents: [Pivox_Ai_V1_ServerEvent] = []
    /// Error to throw from stream after yielding all events.
    var streamError: Error?
    /// Tracks whether stream() was called.
    var streamCallCount = 0

    var sentEvents: [Pivox_Ai_V1_ClientEvent] = []

    func stream(_ event: Pivox_Ai_V1_ClientEvent) -> AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error> {
        streamCallCount += 1
        sentEvents.append(event)
        let events = streamEvents
        let error = streamError
        return AsyncThrowingStream { continuation in
            for event in events {
                continuation.yield(event)
            }
            if let error {
                continuation.finish(throwing: error)
            } else {
                continuation.finish()
            }
        }
    }

    // MARK: - Conversations

    var conversations: [Pivox_Ai_V1_Conversation] = []
    var createdConversation: Pivox_Ai_V1_Conversation?
    var listConversationsError: Error?
    var createConversationError: Error?

    func listConversations(
        _ request: Pivox_Ai_V1_ListConversationsRequest
    ) async throws -> Pivox_Ai_V1_ListConversationsResponse {
        if let err = listConversationsError { throw err }
        return Pivox_Ai_V1_ListConversationsResponse.with {
            $0.conversations = conversations
        }
    }

    func createConversation(
        _ request: Pivox_Ai_V1_CreateConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        if let err = createConversationError { throw err }
        let conv = createdConversation ?? Pivox_Ai_V1_Conversation.with {
            $0.name = "organizations/test/conversations/new-conv"
            $0.title = request.conversation.title
        }
        conversations.append(conv)
        return conv
    }

    func updateConversation(
        _ request: Pivox_Ai_V1_UpdateConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        return request.conversation
    }

    func deleteConversation(
        _ request: Pivox_Ai_V1_DeleteConversationRequest
    ) async throws {
        conversations.removeAll { $0.name == request.name }
    }

    // MARK: - Messages

    var messages: [Pivox_Ai_V1_Message] = []
    var listMessagesError: Error?

    func listMessages(
        _ request: Pivox_Ai_V1_ListMessagesRequest
    ) async throws -> Pivox_Ai_V1_ListMessagesResponse {
        if let err = listMessagesError { throw err }
        return Pivox_Ai_V1_ListMessagesResponse.with {
            $0.messages = messages
        }
    }
}
