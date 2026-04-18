import Foundation
import PivoxModels

// Resource RPCs that the codegen'd facade can't emit today because their
// return type is `google.protobuf.Empty` (lives in the SwiftProtobuf
// module; we haven't generated `parse(fromBytes:)` for it). Keep these
// going through the bytes-based `unaryCall` compat shim until we either:
//   (a) add SwiftProtobuf-side bridge extensions, OR
//   (b) change the Delete RPC signatures to return something non-Empty.

extension ChatClient {
    private static let servicePath = "/pivox.ai.v1.AiChat"

    public func deleteConversation(
        _ request: Pivox_Ai_V1_DeleteConversationRequest
    ) async throws {
        _ = try await unaryCall(
            method: "\(Self.servicePath)/DeleteConversation", request: request)
    }

    public func deleteArtifact(
        _ request: Pivox_Ai_V1_DeleteArtifactRequest
    ) async throws {
        _ = try await unaryCall(
            method: "\(Self.servicePath)/DeleteArtifact", request: request)
    }

    public func deleteArtifactVersion(
        _ request: Pivox_Ai_V1_DeleteArtifactVersionRequest
    ) async throws {
        _ = try await unaryCall(
            method: "\(Self.servicePath)/DeleteArtifactVersion", request: request)
    }
}
