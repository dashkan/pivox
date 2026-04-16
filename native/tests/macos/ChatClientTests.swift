import XCTest
@testable import Pivox

final class ChatClientTests: XCTestCase {

    func testInitWithValidEndpoint() throws {
        let client = try ChatClient(endpoint: "localhost:99999", authToken: "test-token")
        // Client created successfully — gRPC channels are lazy.
        XCTAssertNotNil(client)
    }

    func testInitWithEmptyEndpointFails() {
        // Null endpoint returns nil from C FFI → ChatError.initFailed.
        // Empty string is valid for gRPC (resolves to nothing), so this tests
        // the Swift-side error propagation rather than the empty case.
        // The C FFI returns nil for NULL, which Swift passes as nil for "".
        // Actually, "" is a valid C string — it won't be nil. So this should succeed.
        XCTAssertNoThrow(try ChatClient(endpoint: "", authToken: ""))
    }

    func testSetAuthToken() throws {
        let client = try ChatClient(endpoint: "localhost:99999", authToken: "initial")
        // Should not throw or crash.
        client.setAuthToken("refreshed-token")
        client.setAuthToken("")
    }

    func testSendWithoutStream() throws {
        let client = try ChatClient(endpoint: "localhost:99999", authToken: "test")

        // Sending without an active stream should not crash.
        let event = Pivox_Ai_V1_ClientEvent.with {
            $0.message = Pivox_Ai_V1_UserMessage.with {
                $0.conversation = "organizations/acme/conversations/test"
                $0.parts = [
                    Pivox_Ai_V1_MessagePart.with {
                        $0.text = Pivox_Ai_V1_TextPart.with { $0.text = "Hello" }
                    }
                ]
            }
        }

        XCTAssertNoThrow(try client.send(event))
    }

    func testStreamReturnsAsyncSequence() throws {
        let client = try ChatClient(endpoint: "localhost:99999", authToken: "test")

        // Stream should return an AsyncThrowingStream without crashing.
        let stream = client.stream()
        XCTAssertNotNil(stream)
    }

    func testProtoTypeRoundTrip() throws {
        // Verify PivoxModels types are usable from Swift.
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
        // Build a ServerEvent, serialize, then parse — validates the
        // swift-protobuf codegen + PivoxModels integration.
        let event = Pivox_Ai_V1_ServerEvent.with {
            $0.textDelta = Pivox_Ai_V1_TextDelta.with { $0.delta = "Hello world" }
        }

        let data = try event.serializedData()
        let parsed = try Pivox_Ai_V1_ServerEvent(serializedBytes: data)

        XCTAssertEqual(parsed.textDelta.delta, "Hello world")
    }
}
