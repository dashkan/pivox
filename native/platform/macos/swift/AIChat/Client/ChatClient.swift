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

    public init() throws {
        let (host, port) = try CloudConfig.parsedEndpoint()
        let transport = try HTTP2ClientTransport.Posix(
            target: .dns(host: host, port: port),
            // Endpoint + TLS choice resolved through `CloudConfig`
            // so chat + orgs + any future gRPC client stay in sync.
            transportSecurity: CloudConfig.transportSecurity
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

}

/// Errors surfaced by `ChatClient` to UI code.
///
/// User-visible `description` strings are deliberately generic — they
/// never echo Firebase SDK error details (e.g. "email not found",
/// "invalid credential") because that would let an attacker probing
/// from a compromised client distinguish "user exists but wrong
/// password" from "no such user". Detailed error info is logged
/// internally (see `signpostAuthFailure` below) for diagnostics.
public enum ChatClientError: Error, CustomStringConvertible {
    /// No Firebase user is signed in. Caller (UI) should route to the
    /// sign-in screen.
    case notSignedIn
    /// The Firebase ID-token fetch failed (network outage, expired
    /// session, revoked refresh token, clock skew, etc.). Caller should
    /// prompt for re-authentication.
    case authenticationRequired

    public var description: String {
        switch self {
        case .notSignedIn: return "Sign in to continue."
        case .authenticationRequired: return "Authentication failed. Please sign in again."
        }
    }
}

// MARK: - Auth interceptor

/// Attaches the current Firebase user's ID token as an `authorization:
/// Bearer <token>` metadata header on every outbound RPC.
///
/// Two failure modes are kept distinct so the UI can route correctly:
///   - no signed-in user → throw `notSignedIn` before the wire call
///   - Firebase token-fetch threw (network, expired session, revoked
///     refresh, etc.) → log internally, throw `authenticationRequired`
///
/// In both cases the user sees a generic message; the underlying
/// Firebase SDK error is not echoed to the UI (and not visible from
/// any RPC reply path) because that would leak account-existence
/// signal to anyone running a compromised client.
///
/// Firebase's `getIDToken(forcingRefresh: false)` returns a cached
/// token if it has >~5 min validity left, otherwise refreshes
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
        let token = try await Self.fetchToken()
        var req = request
        req.metadata.addString("Bearer \(token)", forKey: "authorization")
        return try await next(req, context)
    }

    private static func fetchToken() async throws -> String {
        guard let user = Auth.auth().currentUser else {
            throw ChatClientError.notSignedIn
        }
        do {
            return try await user.getIDToken()
        } catch {
            // Log the underlying error for diagnostics; surface only the
            // generic ChatClientError to the caller.
            NSLog("[ChatClient] Firebase getIDToken failed: %@", String(describing: error))
            throw ChatClientError.authenticationRequired
        }
    }
}
