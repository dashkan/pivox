import XCTest
@testable import Pivox

final class ChatClientTests: XCTestCase {

    // MARK: - Initialization

    func testInitWithValidEndpoint() throws {
        let client = try ChatClient(endpoint: "localhost:99999", authToken: "test-token")
        XCTAssertNotNil(client)
    }

    func testInitWithEmptyEndpoint() {
        XCTAssertNoThrow(try ChatClient(endpoint: "", authToken: ""))
    }

    // MARK: - Auth

    func testSetAuthToken() throws {
        let client = try ChatClient(endpoint: "localhost:99999", authToken: "initial")
        client.setAuthToken("refreshed-token")
        client.setAuthToken("")
    }

    // MARK: - Send

    // MARK: - Stream

    func testStreamReturnsAsyncSequence() throws {
        let client = try ChatClient(endpoint: "localhost:99999", authToken: "test")
        let event = Pivox_Ai_V1_ClientEvent.with {
            $0.message = Pivox_Ai_V1_UserMessage.with {
                $0.conversation = "organizations/acme/conversations/test"
                $0.parts = [
                    Pivox_Ai_V1_MessagePart.with {
                        $0.text = Pivox_Ai_V1_TextPart.with { $0.text = "Hello" }
                    },
                ]
            }
        }
        let stream = try client.stream(event)
        XCTAssertNotNil(stream)
    }

    // MARK: - Proto round-trip

    func testProtoTypeRoundTrip() throws {
        let conv = Pivox_Ai_V1_Conversation.with {
            $0.name = "organizations/acme/conversations/test"
            $0.title = "Test Chat"
            $0.messageCount = 5
        }
        let data = try conv.serializedData()
        let decoded = try Pivox_Ai_V1_Conversation(serializedBytes: data)
        XCTAssertEqual(decoded.name, "organizations/acme/conversations/test")
        XCTAssertEqual(decoded.title, "Test Chat")
        XCTAssertEqual(decoded.messageCount, 5)
    }

    func testServerEventParsing() throws {
        let event = Pivox_Ai_V1_ServerEvent.with {
            $0.textDelta = Pivox_Ai_V1_TextDelta.with { $0.delta = "Hello world" }
        }
        let data = try event.serializedData()
        let parsed = try Pivox_Ai_V1_ServerEvent(serializedBytes: data)
        XCTAssertEqual(parsed.textDelta.delta, "Hello world")
    }

    // MARK: - FFI callback: unary response

    func testUnaryCallbackResumesWithEmptyData() {
        // Simulates the C++ on_response callback with 0 bytes (empty response).
        // This is the exact scenario that caused the hung continuation bug.
        let expectation = expectation(description: "continuation resumes")

        Task {
            let data: Data = try await withCheckedThrowingContinuation { cont in
                let box = Unmanaged.passRetained(ContinuationBoxForTest(continuation: cont))

                // Simulate C++ calling on_response with null pointer and 0 size.
                let rawCtx = box.toOpaque()
                simulateOnResponse(rawCtx: rawCtx, bytes: nil, size: 0)
            }
            XCTAssertEqual(data.count, 0)
            expectation.fulfill()
        }

        wait(for: [expectation], timeout: 2.0)
    }

    func testUnaryCallbackResumesWithData() {
        // Simulates the C++ on_response callback with actual bytes.
        let expectation = expectation(description: "continuation resumes with data")
        let testBytes: [UInt8] = [0x0a, 0x05, 0x48, 0x65, 0x6c, 0x6c, 0x6f]

        Task {
            let data: Data = try await withCheckedThrowingContinuation { cont in
                let box = Unmanaged.passRetained(ContinuationBoxForTest(continuation: cont))
                let rawCtx = box.toOpaque()
                testBytes.withUnsafeBufferPointer { buf in
                    simulateOnResponse(rawCtx: rawCtx, bytes: buf.baseAddress, size: buf.count)
                }
            }
            XCTAssertEqual(data.count, testBytes.count)
            XCTAssertEqual([UInt8](data), testBytes)
            expectation.fulfill()
        }

        wait(for: [expectation], timeout: 2.0)
    }

    func testUnaryCallbackResumesWithError() {
        // Simulates the C++ on_error callback.
        let expectation = expectation(description: "continuation throws error")

        Task {
            do {
                let _: Data = try await withCheckedThrowingContinuation { cont in
                    let box = Unmanaged.passRetained(ContinuationBoxForTest(continuation: cont))
                    let rawCtx = box.toOpaque()
                    simulateOnError(rawCtx: rawCtx, message: "Unauthenticated")
                }
                XCTFail("Expected error, got success")
            } catch {
                XCTAssertTrue(error is ChatError)
                expectation.fulfill()
            }
        }

        wait(for: [expectation], timeout: 2.0)
    }
}

// MARK: - Test helpers

/// Mirrors the real ContinuationBox from ChatClient.swift.
private final class ContinuationBoxForTest {
    let continuation: CheckedContinuation<Data, Error>
    init(continuation: CheckedContinuation<Data, Error>) {
        self.continuation = continuation
    }
}

/// Simulates the C++ on_response callback path — same logic as ChatClient.swift's closure.
private func simulateOnResponse(rawCtx: UnsafeMutableRawPointer, bytes: UnsafePointer<UInt8>?, size: Int) {
    let box = Unmanaged<ContinuationBoxForTest>.fromOpaque(rawCtx).takeRetainedValue()
    if let bytes, size > 0 {
        box.continuation.resume(returning: Data(bytes: bytes, count: size))
    } else {
        box.continuation.resume(returning: Data())
    }
}

/// Simulates the C++ on_error callback path.
private func simulateOnError(rawCtx: UnsafeMutableRawPointer, message: String) {
    let box = Unmanaged<ContinuationBoxForTest>.fromOpaque(rawCtx).takeRetainedValue()
    box.continuation.resume(throwing: ChatError.serverError(message))
}
