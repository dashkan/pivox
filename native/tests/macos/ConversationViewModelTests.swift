import XCTest
@testable import Pivox

@MainActor
final class ConversationViewModelTests: XCTestCase {

    func testInitialState() {
        let client = try! ChatClient(endpoint: "localhost:99999", authToken: "test")
        let vm = ConversationViewModel(client: client, conversationName: "organizations/acme/conversations/test")

        XCTAssertEqual(vm.state, .idle)
        XCTAssertTrue(vm.messages.isEmpty)
        XCTAssertTrue(vm.inFlightText.isEmpty)
    }

    func testSendAppendsUserMessage() {
        let client = try! ChatClient(endpoint: "localhost:99999", authToken: "test")
        let vm = ConversationViewModel(client: client, conversationName: "organizations/acme/conversations/test")

        vm.send(text: "Hello AI")

        // User message should be added immediately.
        XCTAssertEqual(vm.messages.count, 1)
        XCTAssertEqual(vm.state, .streaming)

        // Clean up the stream task.
        vm.cancel()
    }

    func testCancelStopsStreaming() {
        let client = try! ChatClient(endpoint: "localhost:99999", authToken: "test")
        let vm = ConversationViewModel(client: client, conversationName: "organizations/acme/conversations/test")

        vm.send(text: "Hello")
        XCTAssertEqual(vm.state, .streaming)

        vm.cancel()
        XCTAssertEqual(vm.state, .idle)
    }

    func testConversationNameStored() {
        let client = try! ChatClient(endpoint: "localhost:99999", authToken: "test")
        let name = "organizations/acme/conversations/abc123"
        let vm = ConversationViewModel(client: client, conversationName: name)

        XCTAssertEqual(vm.conversationName, name)
    }
}

@MainActor
final class ConversationListViewModelTests: XCTestCase {

    func testInitialState() {
        let client = try! ChatClient(endpoint: "localhost:99999", authToken: "test")
        let vm = ConversationListViewModel(client: client, orgName: "acme")

        XCTAssertEqual(vm.state, .idle)
        XCTAssertTrue(vm.conversations.isEmpty)
    }
}
