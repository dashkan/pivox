import Foundation
import PivoxModels
@testable import Pivox

/// Mock ChatClient for unit testing view models without a network connection.
final class MockChatClient: ChatClientProtocol, @unchecked Sendable {

    // MARK: - Stream

    /// Events to yield from streamGenerateContent(), consumed in order.
    var streamEvents: [Pivox_Ai_V1_ServerEvent] = []
    /// Error to throw from stream after yielding all events.
    var streamError: Error?
    /// Tracks how many stream calls have been initiated.
    var streamCallCount = 0
    /// Captured `GenerateContentRequest`s sent to streamGenerateContent.
    var sentRequests: [Pivox_Ai_V1_GenerateContentRequest] = []

    func streamGenerateContent(
        _ request: Pivox_Ai_V1_GenerateContentRequest
    ) -> AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error> {
        streamCallCount += 1
        sentRequests.append(request)
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

    // MARK: - Generate (unary)

    var generateResponse: Pivox_Ai_V1_GenerateContentResponse?
    var generateError: Error?

    func generateContent(
        _ request: Pivox_Ai_V1_GenerateContentRequest
    ) async throws -> Pivox_Ai_V1_GenerateContentResponse {
        if let err = generateError { throw err }
        return generateResponse ?? Pivox_Ai_V1_GenerateContentResponse()
    }

    // MARK: - Summarize

    var summarizeResponse: Pivox_Ai_V1_Conversation?
    var summarizeError: Error?

    func summarizeConversation(
        _ request: Pivox_Ai_V1_SummarizeConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        if let err = summarizeError { throw err }
        return summarizeResponse ?? Pivox_Ai_V1_Conversation()
    }

    // MARK: - Get conversation

    var getConversationResponse: Pivox_Ai_V1_Conversation?
    var getConversationError: Error?

    func getConversation(
        _ request: Pivox_Ai_V1_GetConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        if let err = getConversationError { throw err }
        return getConversationResponse ?? Pivox_Ai_V1_Conversation.with {
            $0.name = request.name
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
