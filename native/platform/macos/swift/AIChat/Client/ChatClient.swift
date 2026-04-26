import FirebaseAuth
import Foundation
import GRPCCore
import GRPCNIOTransportHTTP2
import GRPCProtobuf
import PivoxModels
import SwiftProtobuf

/// Pivox cloud gRPC chat client. Pure Swift (grpc-swift-2). Wraps the
/// generated `Pivox_Ai_V1_AiChat.Client` and manages the long-lived
/// HTTP/2 connection that runs in the background while the chat surface
/// is mounted.
///
/// Auth: every outbound RPC carries the current Firebase user's ID
/// token via `FirebaseAuthInterceptor`. Token freshness is Firebase's
/// problem — its SDK refreshes the cached token before each
/// `getIDToken()` if the existing one is within the refresh window.
@MainActor
public final class ChatClient {
    private let grpc: GRPCClient<HTTP2ClientTransport.Posix>
    private let aiChat: Pivox_Ai_V1_AiChat.Client<HTTP2ClientTransport.Posix>
    private var runTask: Task<Void, Error>?

    public init(endpoint: String) throws {
        let (host, port) = try Self.parseEndpoint(endpoint)
        let transport = try HTTP2ClientTransport.Posix(
            target: .dns(host: host, port: port),
            // Local dev cluster runs plaintext on :50051. Prod terminates
            // TLS at nginx; the Pivox cloud server itself stays plaintext.
            // Add a TLS config option here when we wire prod direct-dial.
            transportSecurity: .plaintext
        )
        self.grpc = GRPCClient(
            transport: transport,
            interceptors: [FirebaseAuthInterceptor()]
        )
        self.aiChat = Pivox_Ai_V1_AiChat.Client(wrapping: grpc)
        self.runTask = Task { [grpc] in
            try await grpc.runConnections()
        }
    }

    /// Cancel the background connection task and tear down the channel.
    /// Safe to call from anywhere; idempotent.
    public func cancel() {
        runTask?.cancel()
        runTask = nil
        grpc.beginGracefulShutdown()
    }

    // MARK: - RPCs

    func listConversations(
        _ request: Pivox_Ai_V1_ListConversationsRequest
    ) async throws -> Pivox_Ai_V1_ListConversationsResponse {
        try await aiChat.listConversations(request)
    }

    func getConversation(
        _ request: Pivox_Ai_V1_GetConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        try await aiChat.getConversation(request)
    }

    func createConversation(
        _ request: Pivox_Ai_V1_CreateConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        try await aiChat.createConversation(request)
    }

    func updateConversation(
        _ request: Pivox_Ai_V1_UpdateConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        try await aiChat.updateConversation(request)
    }

    func deleteConversation(
        _ request: Pivox_Ai_V1_DeleteConversationRequest
    ) async throws {
        _ = try await aiChat.deleteConversation(request)
    }

    func summarizeConversation(
        _ request: Pivox_Ai_V1_SummarizeConversationRequest
    ) async throws -> Pivox_Ai_V1_Conversation {
        try await aiChat.summarizeConversation(request)
    }

    func listMessages(
        _ request: Pivox_Ai_V1_ListMessagesRequest
    ) async throws -> Pivox_Ai_V1_ListMessagesResponse {
        try await aiChat.listMessages(request)
    }

    /// Server-streaming RPC. Caller iterates the returned sequence.
    /// On cancellation or task abort the stream finishes; surfaces
    /// errors as thrown values from `next()`.
    func streamGenerateContent(
        _ request: Pivox_Ai_V1_GenerateContentRequest
    ) -> AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    try await aiChat.streamGenerateContent(request) { stream in
                        for try await response in stream.messages {
                            continuation.yield(response)
                        }
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    // MARK: -

    private static func parseEndpoint(_ s: String) throws -> (String, Int) {
        let parts = s.split(separator: ":", maxSplits: 1).map(String.init)
        guard parts.count == 2, let port = Int(parts[1]) else {
            throw ChatClientError.invalidEndpoint(s)
        }
        return (parts[0], port)
    }
}

enum ChatClientError: Error, CustomStringConvertible {
    case invalidEndpoint(String)

    var description: String {
        switch self {
        case .invalidEndpoint(let s): return "invalid endpoint: \(s)"
        }
    }
}

// MARK: - Auth interceptor

/// Attaches the current Firebase user's ID token as an `authorization:
/// Bearer <token>` metadata header on every outbound RPC. If no user is
/// signed in, no header is attached — the server-side AuthInterceptor
/// rejects it (as it should).
///
/// Firebase's `getIDToken(forcingRefresh: false)` returns a cached token
/// if it has more than ~5 minutes of validity left, otherwise refreshes
/// transparently. We don't need to manage expiry ourselves.
struct FirebaseAuthInterceptor: ClientInterceptor {
    func intercept<Input: Sendable, Output: Sendable>(
        request: StreamingClientRequest<Input>,
        context: ClientContext,
        next: (
            StreamingClientRequest<Input>,
            ClientContext
        ) async throws -> StreamingClientResponse<Output>
    ) async throws -> StreamingClientResponse<Output> {
        var req = request
        if let token = await Self.fetchToken() {
            req.metadata.addString("Bearer \(token)", forKey: "authorization")
        }
        return try await next(req, context)
    }

    private static func fetchToken() async -> String? {
        guard let user = Auth.auth().currentUser else { return nil }
        return try? await user.getIDToken()
    }
}
