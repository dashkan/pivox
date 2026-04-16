import Foundation

// MARK: - Conversation RPCs

extension ChatClient {
    private static let servicePath = "/pivox.ai.v1.AiChat"

    public func listConversations(
        _ request: Pivox_Ai_V1_ListConversationsRequest
    ) async throws -> Pivox_Ai_V1_ListConversationsResponse {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/ListConversations", request: request)
        return try Pivox_Ai_V1_ListConversationsResponse(serializedBytes: data)
    }

    public func getConversation(
        _ request: Pivox_Ai_V1_GetConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/GetConversation", request: request)
        return try Pivox_Ai_V1_Conversation(serializedBytes: data)
    }

    public func createConversation(
        _ request: Pivox_Ai_V1_CreateConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/CreateConversation", request: request)
        return try Pivox_Ai_V1_Conversation(serializedBytes: data)
    }

    public func updateConversation(
        _ request: Pivox_Ai_V1_UpdateConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/UpdateConversation", request: request)
        return try Pivox_Ai_V1_Conversation(serializedBytes: data)
    }

    public func deleteConversation(
        _ request: Pivox_Ai_V1_DeleteConversationRequest
    ) async throws {
        _ = try await unaryCall(
            method: "\(Self.servicePath)/DeleteConversation", request: request)
    }
}

// MARK: - Message RPCs

extension ChatClient {
    public func listMessages(
        _ request: Pivox_Ai_V1_ListMessagesRequest
    ) async throws -> Pivox_Ai_V1_ListMessagesResponse {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/ListMessages", request: request)
        return try Pivox_Ai_V1_ListMessagesResponse(serializedBytes: data)
    }

    public func getMessage(
        _ request: Pivox_Ai_V1_GetMessageRequest
    ) async throws -> Pivox_Ai_V1_Message {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/GetMessage", request: request)
        return try Pivox_Ai_V1_Message(serializedBytes: data)
    }
}

// MARK: - Artifact RPCs

extension ChatClient {
    public func listArtifacts(
        _ request: Pivox_Ai_V1_ListArtifactsRequest
    ) async throws -> Pivox_Ai_V1_ListArtifactsResponse {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/ListArtifacts", request: request)
        return try Pivox_Ai_V1_ListArtifactsResponse(serializedBytes: data)
    }

    public func getArtifact(
        _ request: Pivox_Ai_V1_GetArtifactRequest
    ) async throws -> Pivox_Ai_V1_Artifact {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/GetArtifact", request: request)
        return try Pivox_Ai_V1_Artifact(serializedBytes: data)
    }

    public func deleteArtifact(
        _ request: Pivox_Ai_V1_DeleteArtifactRequest
    ) async throws {
        _ = try await unaryCall(
            method: "\(Self.servicePath)/DeleteArtifact", request: request)
    }
}

// MARK: - Artifact Version RPCs

extension ChatClient {
    public func listArtifactVersions(
        _ request: Pivox_Ai_V1_ListArtifactVersionsRequest
    ) async throws -> Pivox_Ai_V1_ListArtifactVersionsResponse {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/ListArtifactVersions", request: request)
        return try Pivox_Ai_V1_ListArtifactVersionsResponse(serializedBytes: data)
    }

    public func getArtifactVersion(
        _ request: Pivox_Ai_V1_GetArtifactVersionRequest
    ) async throws -> Pivox_Ai_V1_ArtifactVersion {
        let data = try await unaryCall(
            method: "\(Self.servicePath)/GetArtifactVersion", request: request)
        return try Pivox_Ai_V1_ArtifactVersion(serializedBytes: data)
    }

    public func deleteArtifactVersion(
        _ request: Pivox_Ai_V1_DeleteArtifactVersionRequest
    ) async throws {
        _ = try await unaryCall(
            method: "\(Self.servicePath)/DeleteArtifactVersion", request: request)
    }
}
