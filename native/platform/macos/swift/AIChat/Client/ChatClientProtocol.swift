import Foundation
import PivoxModels

/// Protocol abstracting ChatClient for testability.
/// View models depend on this protocol, not the concrete ChatClient.
public protocol ChatClientProtocol: Sendable {
    func streamGenerateContent(
        _ request: Pivox_Ai_V1_GenerateContentRequest
    ) -> AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error>

    func generateContent(
        _ request: Pivox_Ai_V1_GenerateContentRequest
    ) async throws -> Pivox_Ai_V1_GenerateContentResponse

    func summarizeConversation(
        _ request: Pivox_Ai_V1_SummarizeConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation

    func getConversation(
        _ request: Pivox_Ai_V1_GetConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation

    func listConversations(
        _ request: Pivox_Ai_V1_ListConversationsRequest
    ) async throws -> Pivox_Ai_V1_ListConversationsResponse

    func createConversation(
        _ request: Pivox_Ai_V1_CreateConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation

    func updateConversation(
        _ request: Pivox_Ai_V1_UpdateConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation

    func deleteConversation(
        _ request: Pivox_Ai_V1_DeleteConversationRequest
    ) async throws

    func listMessages(
        _ request: Pivox_Ai_V1_ListMessagesRequest
    ) async throws -> Pivox_Ai_V1_ListMessagesResponse
}

// ChatClient already conforms — all methods match.
extension ChatClient: ChatClientProtocol {}
