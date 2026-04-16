import Foundation
import SwiftProtobuf

/// Swift facade wrapping the shared C++ AiChatClient via the C FFI.
public final class ChatClient: @unchecked Sendable {
    private let handle: OpaquePointer

    public init(endpoint: String, authToken: String) throws {
        guard let h = pivox_ai_chat_client_create(endpoint, authToken) else {
            throw ChatError.initFailed
        }
        self.handle = h
    }

    deinit {
        pivox_ai_chat_client_destroy(handle)
    }

    public func setAuthToken(_ token: String) {
        pivox_ai_chat_client_set_auth_token(handle, token)
    }

    /// Opens a server-streaming call with the given client event.
    /// Returns an async sequence of server events.
    public func stream(_ event: Pivox_Ai_V1_ClientEvent) throws -> AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error> {
        let requestData = try event.serializedData()

        return AsyncThrowingStream { continuation in
            let ctx = Unmanaged.passRetained(
                StreamContext(continuation: continuation)
            )

            requestData.withUnsafeBytes { buf in
                pivox_ai_chat_client_start_stream(
                    handle,
                    buf.bindMemory(to: UInt8.self).baseAddress,
                    buf.count,
                    ctx.toOpaque(),
                    // on_event
                    { rawCtx, bytes, size in
                        guard let rawCtx else { return }
                        let streamCtx = Unmanaged<StreamContext>
                            .fromOpaque(rawCtx).takeUnretainedValue()
                        if let bytes, size > 0 {
                            let data = Data(bytes: bytes, count: size)
                            if let event = try? Pivox_Ai_V1_ServerEvent(serializedBytes: data) {
                                streamCtx.continuation.yield(event)
                            }
                        }
                    },
                    // on_error
                    { rawCtx, msg in
                        guard let rawCtx else { return }
                        let streamCtx = Unmanaged<StreamContext>
                            .fromOpaque(rawCtx).takeRetainedValue()
                        let message = msg.map { String(cString: $0) } ?? "unknown error"
                        streamCtx.continuation.finish(
                            throwing: ChatError.streamFailed(message))
                    },
                    // on_complete
                    { rawCtx in
                        guard let rawCtx else { return }
                        let streamCtx = Unmanaged<StreamContext>
                            .fromOpaque(rawCtx).takeRetainedValue()
                        streamCtx.continuation.finish()
                    }
                )
            }

            let handleBits = Int(bitPattern: handle)
            continuation.onTermination = { _ in
                let h = OpaquePointer(bitPattern: handleBits)
                pivox_ai_chat_client_cancel(h)
            }
        }
    }

    /// Executes a unary RPC and returns the raw response bytes.
    func unaryCall(method: String, request: any SwiftProtobuf.Message) async throws -> Data {
        let requestData = try request.serializedData()
        return try await withCheckedThrowingContinuation { cont in
            let contBox = Unmanaged.passRetained(
                ContinuationBox(continuation: cont)
            )

            requestData.withUnsafeBytes { buf in
                pivox_ai_chat_unary_call(
                    handle,
                    method,
                    buf.bindMemory(to: UInt8.self).baseAddress,
                    buf.count,
                    contBox.toOpaque(),
                    { rawCtx, bytes, size in
                        guard let rawCtx else { return }
                        let box = Unmanaged<ContinuationBox>
                            .fromOpaque(rawCtx).takeRetainedValue()
                        if let bytes, size > 0 {
                            box.continuation.resume(
                                returning: Data(bytes: bytes, count: size))
                        } else {
                            box.continuation.resume(returning: Data())
                        }
                    },
                    { rawCtx, msg in
                        guard let rawCtx else { return }
                        let box = Unmanaged<ContinuationBox>
                            .fromOpaque(rawCtx).takeRetainedValue()
                        let message = msg.map { String(cString: $0) } ?? "RPC failed"
                        box.continuation.resume(
                            throwing: ChatError.serverError(message))
                    }
                )
            }
        }
    }
}

private final class StreamContext {
    let continuation: AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error>.Continuation
    init(continuation: AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error>.Continuation) {
        self.continuation = continuation
    }
}

private final class ContinuationBox {
    let continuation: CheckedContinuation<Data, Error>
    init(continuation: CheckedContinuation<Data, Error>) {
        self.continuation = continuation
    }
}
