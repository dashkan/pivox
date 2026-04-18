import Foundation
import PivoxModels
import SwiftProtobuf

/// Swift error surfaced for any non-OK gRPC result.
public struct GRPCError: Error, CustomStringConvertible {
    public let code: Int32   // grpc::StatusCode raw value
    public let message: String

    public var description: String { "GRPCError(\(code)): \(message)" }
}

/// Swift facade over the shared C++ `pivox::ai_chat::ChatClient`.
///
/// `ChatClient` is a Swift-C++ interop shared-reference type, so this
/// facade holds it directly as a typed C++ value. All calls into C++
/// use typed Swift proto pointers (Swift passes them as `OpaquePointer`;
/// C++ sees the real typed pointer). Response values flow back as
/// typed Swift proto values in Result structs or via callbacks.
public final class ChatClient: @unchecked Sendable {
    /// The underlying C++ shared-reference instance. `SWIFT_SHARED_REFERENCE`
    /// makes Swift manage retain/release automatically via the global
    /// `ChatClientRetain`/`ChatClientRelease` hooks.
    ///
    /// Visibility is `internal` (module-scoped) so the generated facade
    /// extensions in the same module can reach the C++ methods.
    internal let cpp: pivox.ai_chat.ChatClient

    public init(endpoint: String, authToken: String) throws {
        guard let instance = pivox.ai_chat.ChatClient.Create(endpoint, authToken) else {
            throw ChatError.initFailed
        }
        self.cpp = instance
    }

    public func setAuthToken(_ token: String) {
        cpp.SetAuthToken(token)
    }

    // MARK: - Generic unary (compat shim for non-migrated resource RPCs).
    // TODO: replace all call sites in ChatClient+Resources.swift with
    // typed C++ methods (like listMessages) once codegen lands.

    func unaryCall(
        method: String,
        request: any SwiftProtobuf.Message
    ) async throws -> Data {
        let requestData = try request.serializedData()
        return try await withCheckedThrowingContinuation { cont in
            let box = Unmanaged.passRetained(ContinuationBox(continuation: cont))

            requestData.withUnsafeBytes { buf in
                cpp.UnaryCallBytes(
                    method,
                    buf.bindMemory(to: UInt8.self).baseAddress,
                    buf.count,
                    box.toOpaque(),
                    { rawCtx, bytes, size in
                        guard let rawCtx else { return }
                        let b = Unmanaged<ContinuationBox>
                            .fromOpaque(rawCtx).takeRetainedValue()
                        if let bytes, size > 0 {
                            b.continuation.resume(
                                returning: Data(bytes: bytes, count: size))
                        } else {
                            b.continuation.resume(returning: Data())
                        }
                    },
                    { rawCtx, code, msg in
                        guard let rawCtx else { return }
                        let b = Unmanaged<ContinuationBox>
                            .fromOpaque(rawCtx).takeRetainedValue()
                        let text = msg.map { String(cString: $0) } ?? "RPC failed"
                        b.continuation.resume(
                            throwing: GRPCError(code: code, message: text))
                    }
                )
            }
        }
    }

    // Typed per-RPC methods (stream, listMessages, etc.) are generated
    // in AiChat+Facade.swift by protoc-gen-pivox-swift-facade.
}

private final class ContinuationBox {
    let continuation: CheckedContinuation<Data, Error>
    init(continuation: CheckedContinuation<Data, Error>) {
        self.continuation = continuation
    }
}
