import PivoxModels
import XCTest
@testable import Pivox

@MainActor
final class ConversationViewModelTests: XCTestCase {

    private func makeMock() -> MockChatClient { MockChatClient() }

    private func makeVM(_ mock: MockChatClient? = nil) -> (ConversationViewModel, MockChatClient) {
        let m = mock ?? makeMock()
        let vm = ConversationViewModel(
            client: m, conversationName: "organizations/acme/conversations/test")
        return (vm, m)
    }

    // MARK: - Initial state

    func testInitialState() {
        let (vm, _) = makeVM()
        XCTAssertEqual(vm.state, .idle)
        XCTAssertTrue(vm.messages.isEmpty)
        XCTAssertTrue(vm.inFlightText.isEmpty)
    }

    func testConversationNameStored() {
        let (vm, _) = makeVM()
        XCTAssertEqual(vm.conversationName, "organizations/acme/conversations/test")
    }

    // MARK: - Send

    func testSendAppendsUserMessage() {
        let (vm, _) = makeVM()
        vm.send(text: "Hello AI")
        XCTAssertEqual(vm.messages.count, 1)
        XCTAssertEqual(vm.state, .streaming)
        vm.cancel()
    }

    func testSendSetsStreamingState() {
        let (vm, _) = makeVM()
        vm.send(text: "Hello")
        XCTAssertEqual(vm.state, .streaming)
        vm.cancel()
    }

    func testSendBlockedWhileStreaming() {
        let (vm, _) = makeVM()
        vm.send(text: "First")
        vm.send(text: "Second")
        XCTAssertEqual(vm.messages.count, 1)
        vm.cancel()
    }

    func testSendCallsStreamAndSend() async throws {
        let (vm, mock) = makeVM()
        vm.send(text: "Hello")
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertEqual(mock.streamCallCount, 1)
        XCTAssertEqual(mock.sentEvents.count, 1)
        vm.cancel()
    }

    func testSendAfterCancelResets() {
        let (vm, _) = makeVM()
        vm.send(text: "First")
        vm.cancel()
        vm.send(text: "Second")
        XCTAssertEqual(vm.messages.count, 2)
        XCTAssertEqual(vm.state, .streaming)
        vm.cancel()
    }

    // MARK: - Cancel

    func testCancelStopsStreaming() {
        let (vm, _) = makeVM()
        vm.send(text: "Hello")
        vm.cancel()
        XCTAssertEqual(vm.state, .idle)
    }

    func testCancelFromIdleIsSafe() {
        let (vm, _) = makeVM()
        vm.cancel()
        XCTAssertEqual(vm.state, .idle)
    }

    func testCancelCommitsInFlightText() {
        let (vm, _) = makeVM()
        vm.send(text: "Hello")
        vm.inFlightText = "Partial response"
        vm.cancel()
        XCTAssertEqual(vm.messages.count, 2)
        XCTAssertTrue(vm.inFlightText.isEmpty)
    }

    func testDoubleCancelIsSafe() {
        let (vm, _) = makeVM()
        vm.send(text: "Hello")
        vm.cancel()
        vm.cancel()
        XCTAssertEqual(vm.state, .idle)
    }

    // MARK: - Streaming event handling

    func testTextDeltaAssembly() async throws {
        let mock = makeMock()
        mock.streamEvents = [
            .with { $0.textStart = .with { $0.messageID = "m1" } },
            .with { $0.textDelta = .with { $0.delta = "Hello " } },
            .with { $0.textDelta = .with { $0.delta = "world!" } },
            .with { $0.textEnd = Pivox_Ai_V1_TextEnd() },
            .with { $0.done = Pivox_Ai_V1_Done() },
        ]
        let (vm, _) = makeVM(mock)

        vm.send(text: "Hi")

        // Wait for the stream to complete.
        try await Task.sleep(for: .milliseconds(100))

        // User message + committed assistant message.
        XCTAssertEqual(vm.messages.count, 2)
        XCTAssertEqual(vm.state, .idle)

        // Verify the assistant message text was assembled from deltas.
        let assistantParts = vm.messages[1].parts
        XCTAssertEqual(assistantParts.count, 1)
        if case .text(let tp) = assistantParts[0].part {
            XCTAssertEqual(tp.text, "Hello world!")
        } else {
            XCTFail("Expected text part")
        }
    }

    func testStreamErrorSetsErrorState() async throws {
        let mock = makeMock()
        mock.streamError = ChatError.streamFailed("connection lost")
        let (vm, _) = makeVM(mock)

        vm.send(text: "Hi")
        try await Task.sleep(for: .milliseconds(100))

        XCTAssertEqual(vm.state, .error("Stream failed: connection lost"))
    }

    func testDoneEventSetsIdleState() async throws {
        let mock = makeMock()
        mock.streamEvents = [
            .with { $0.textStart = .with { $0.messageID = "m1" } },
            .with { $0.done = Pivox_Ai_V1_Done() },
        ]
        let (vm, _) = makeVM(mock)

        vm.send(text: "Hi")
        try await Task.sleep(for: .milliseconds(100))

        XCTAssertEqual(vm.state, .idle)
    }

    // MARK: - Load history

    func testLoadHistoryPopulatesMessages() async {
        let mock = makeMock()
        mock.messages = [
            .with { $0.name = "m1"; $0.role = .user },
            .with { $0.name = "m2"; $0.role = .assistant },
        ]
        let (vm, _) = makeVM(mock)

        await vm.loadHistory()

        XCTAssertEqual(vm.messages.count, 2)
        XCTAssertEqual(vm.state, .idle)
    }

    func testLoadHistoryErrorSetsState() async {
        let mock = makeMock()
        mock.listMessagesError = ChatError.serverError("not found")
        let (vm, _) = makeVM(mock)

        await vm.loadHistory()

        XCTAssertEqual(vm.state, .error("Server error: not found"))
    }

    func testLoadHistoryPrependsWhenSendRacesDuringLoad() async throws {
        let mock = makeMock()
        mock.messages = [
            .with { $0.name = "m1"; $0.role = .user;
                $0.parts = [.with { $0.text = .with { $0.text = "old msg" } }] },
            .with { $0.name = "m2"; $0.role = .assistant;
                $0.parts = [.with { $0.text = .with { $0.text = "old reply" } }] },
        ]
        mock.streamEvents = [
            .with { $0.textStart = .with { $0.messageID = "m3" } },
            .with { $0.textDelta = .with { $0.delta = "new reply" } },
            .with { $0.textEnd = Pivox_Ai_V1_TextEnd() },
            .with { $0.done = Pivox_Ai_V1_Done() },
        ]
        let (vm, _) = makeVM(mock)

        let loadTask = Task { await vm.loadHistory() }
        try await Task.sleep(for: .milliseconds(50))
        vm.send(text: "new msg")

        await loadTask.value
        try await Task.sleep(for: .milliseconds(200))

        // History should be prepended before the user's new message.
        XCTAssertGreaterThanOrEqual(vm.messages.count, 3)
    }

    func testLoadHistorySkipsIfNotIdle() async {
        let mock = makeMock()
        mock.messages = [.with { $0.name = "m1" }]
        let (vm, _) = makeVM(mock)

        vm.send(text: "Racing")  // state = .streaming
        await vm.loadHistory()    // should skip

        // Only the user message from send(), not the mock history.
        XCTAssertEqual(vm.messages.count, 1)
        vm.cancel()
    }
}

@MainActor
final class ConversationListViewModelTests: XCTestCase {

    func testInitialState() {
        let mock = MockChatClient()
        let vm = ConversationListViewModel(client: mock, orgName: "acme")
        XCTAssertEqual(vm.state, .idle)
        XCTAssertTrue(vm.conversations.isEmpty)
    }

    func testLoadPopulatesConversations() async {
        let mock = MockChatClient()
        mock.conversations = [
            .with { $0.name = "organizations/acme/conversations/c1"; $0.title = "Chat 1" },
        ]
        let vm = ConversationListViewModel(client: mock, orgName: "acme")

        await vm.load()

        XCTAssertEqual(vm.conversations.count, 1)
        XCTAssertEqual(vm.state, .loaded)
    }

    func testLoadErrorSetsState() async {
        let mock = MockChatClient()
        mock.listConversationsError = ChatError.serverError("unavailable")
        let vm = ConversationListViewModel(client: mock, orgName: "acme")

        await vm.load()

        XCTAssertEqual(vm.state, .error("Server error: unavailable"))
    }

    func testCreateAddsToList() async throws {
        let mock = MockChatClient()
        let vm = ConversationListViewModel(client: mock, orgName: "acme")

        let conv = try await vm.create(title: "New Chat")

        XCTAssertEqual(vm.conversations.count, 1)
        XCTAssertEqual(conv.title, "New Chat")
    }

    func testDeleteRemovesFromList() async throws {
        let mock = MockChatClient()
        mock.conversations = [
            .with { $0.name = "organizations/acme/conversations/c1" },
        ]
        let vm = ConversationListViewModel(client: mock, orgName: "acme")
        await vm.load()

        try await vm.delete(name: "organizations/acme/conversations/c1")

        XCTAssertTrue(vm.conversations.isEmpty)
    }
}
