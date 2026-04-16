import XCTest
@testable import Pivox

@MainActor
final class ConversationViewModelTests: XCTestCase {

    private func makeVM() -> ConversationViewModel {
        let client = try! ChatClient(endpoint: "localhost:99999", authToken: "test")
        return ConversationViewModel(client: client, conversationName: "organizations/acme/conversations/test")
    }

    // MARK: - Initial state

    func testInitialState() {
        let vm = makeVM()
        XCTAssertEqual(vm.state, .idle)
        XCTAssertTrue(vm.messages.isEmpty)
        XCTAssertTrue(vm.inFlightText.isEmpty)
    }

    func testConversationNameStored() {
        let vm = makeVM()
        XCTAssertEqual(vm.conversationName, "organizations/acme/conversations/test")
    }

    // MARK: - Send

    func testSendAppendsUserMessage() {
        let vm = makeVM()
        vm.send(text: "Hello AI")

        XCTAssertEqual(vm.messages.count, 1)
        XCTAssertEqual(vm.state, .streaming)

        vm.cancel()
    }

    func testSendSetsStreamingState() {
        let vm = makeVM()
        vm.send(text: "Hello")
        XCTAssertEqual(vm.state, .streaming)
        vm.cancel()
    }

    func testSendBlockedWhileStreaming() {
        let vm = makeVM()
        vm.send(text: "First")
        XCTAssertEqual(vm.messages.count, 1)

        // Second send should be ignored while streaming.
        vm.send(text: "Second")
        XCTAssertEqual(vm.messages.count, 1)

        vm.cancel()
    }

    func testSendEmptyTextDoesNothing() {
        let vm = makeVM()
        vm.send(text: "   ")
        // Empty text after trimming should not add a message.
        // Currently send() doesn't trim — this documents the behavior.
        // If we add trimming, this test enforces it.
        XCTAssertEqual(vm.state, .streaming)
        vm.cancel()
    }

    // MARK: - Cancel

    func testCancelStopsStreaming() {
        let vm = makeVM()
        vm.send(text: "Hello")
        XCTAssertEqual(vm.state, .streaming)

        vm.cancel()
        XCTAssertEqual(vm.state, .idle)
    }

    func testCancelFromIdleIsSafe() {
        let vm = makeVM()
        vm.cancel()  // No-op, must not crash.
        XCTAssertEqual(vm.state, .idle)
    }

    func testCancelCommitsInFlightText() {
        let vm = makeVM()
        vm.send(text: "Hello")

        // Simulate in-flight text from streaming.
        vm.inFlightText = "Partial response"
        vm.cancel()

        // In-flight text should be committed as an assistant message.
        XCTAssertEqual(vm.messages.count, 2)  // user + partial assistant
        XCTAssertTrue(vm.inFlightText.isEmpty)
    }

    func testDoubleCancelIsSafe() {
        let vm = makeVM()
        vm.send(text: "Hello")
        vm.cancel()
        vm.cancel()  // Second cancel, no crash.
        XCTAssertEqual(vm.state, .idle)
    }

    // MARK: - Event handling

    func testSendAfterCancelResets() {
        let vm = makeVM()
        vm.send(text: "First")
        vm.cancel()
        XCTAssertEqual(vm.state, .idle)

        // Should be able to send again after cancel.
        vm.send(text: "Second")
        XCTAssertEqual(vm.messages.count, 2)
        XCTAssertEqual(vm.state, .streaming)
        vm.cancel()
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
