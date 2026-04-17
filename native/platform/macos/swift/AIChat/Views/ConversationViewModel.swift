import Foundation
import SwiftUI

@MainActor
public final class ConversationViewModel: ObservableObject {
    @Published public var messages: [Pivox_Ai_V1_Message] = []
    @Published public var inFlightText: String = ""
    @Published public var state: ConversationState = .idle
    /// Bumped whenever a message is appended to the tail. The view observes
    /// this to scroll to bottom; prepends (loadOlder) don't bump it so scroll
    /// position is preserved.
    @Published public private(set) var appendTick: Int = 0
    @Published public private(set) var isLoadingOlder: Bool = false

    private let client: any ChatClientProtocol
    public let conversationName: String
    /// True when the conversation was just created in this session (via the
    /// new-chat flow) and has no server-side history to load. Prevents a
    /// pointless `listMessages` RPC that could race with the first `send()`.
    private let isNew: Bool

    private var streamTask: Task<Void, Never>?
    private var olderCursor: String?

    private static let pageSize: Int32 = 50

    public var canLoadOlder: Bool {
        guard let c = olderCursor else { return false }
        return !c.isEmpty && !isLoadingOlder
    }

    public enum ConversationState: Equatable {
        case idle
        case loading
        case streaming
        case error(String)

        public static func == (lhs: ConversationState, rhs: ConversationState) -> Bool {
            switch (lhs, rhs) {
            case (.idle, .idle), (.loading, .loading), (.streaming, .streaming):
                return true
            case (.error(let a), .error(let b)):
                return a == b
            default:
                return false
            }
        }
    }

    public init(client: any ChatClientProtocol, conversationName: String, isNew: Bool = false) {
        self.client = client
        self.conversationName = conversationName
        self.isNew = isNew
    }

    public func loadHistory() async {
        // Brand-new conversations have no server history yet. Skipping avoids
        // a racy listMessages call that could return the just-sent user
        // message and cause a UI duplicate.
        guard !isNew else { return }
        // Allow entry from .idle (first load) or .error (retry). Block if
        // already loading or a stream is in flight.
        switch state {
        case .idle, .error:
            break
        case .loading, .streaming:
            return
        }
        state = .loading
        do {
            let request = Pivox_Ai_V1_ListMessagesRequest.with {
                $0.parent = conversationName
                $0.pageSize = Self.pageSize
            }
            print("[chat] listMessages → \(conversationName) pageSize=\(Self.pageSize)")
            let response = try await client.listMessages(request)
            print("[chat] listMessages ← \(response.messages.count) messages, nextToken=\(response.nextPageToken.isEmpty ? "<none>" : "<present>")")
            // Server returns newest-first; reverse to oldest→newest for display.
            let ordered = Array(response.messages.reversed())
            // Re-check after suspension — send() may have run while we awaited.
            if state == .loading {
                messages = ordered
                state = .idle
                appendTick &+= 1
            } else {
                // User sent a message while we were loading. Prepend history
                // before the user's message instead of discarding it.
                messages.insert(contentsOf: ordered, at: 0)
            }
            olderCursor = response.nextPageToken.isEmpty ? nil : response.nextPageToken
        } catch {
            print("[chat] listMessages error: \(error)")
            guard state == .loading else { return }
            state = .error(error.localizedDescription)
        }
    }

    /// Fetch one page of older messages and prepend them. View is responsible
    /// for capturing the anchor message id before calling and restoring scroll
    /// after the await returns.
    public func loadOlder() async {
        guard let token = olderCursor, !token.isEmpty else { return }
        guard !isLoadingOlder else { return }
        isLoadingOlder = true
        defer { isLoadingOlder = false }
        do {
            let request = Pivox_Ai_V1_ListMessagesRequest.with {
                $0.parent = conversationName
                $0.pageSize = Self.pageSize
                $0.pageToken = token
            }
            let response = try await client.listMessages(request)
            let ordered = Array(response.messages.reversed())
            messages.insert(contentsOf: ordered, at: 0)
            olderCursor = response.nextPageToken.isEmpty ? nil : response.nextPageToken
        } catch {
            // Swallow — older-page fetch failure shouldn't poison the session.
            // The sentinel will re-trigger on next scroll attempt.
        }
    }

    public func send(text: String) {
        // Don't allow sending while already streaming.
        guard state != .streaming else { return }

        // Add user message to the local list immediately.
        let userMsg = Pivox_Ai_V1_Message.with {
            $0.name = Self.localName()
            $0.parts = [
                Pivox_Ai_V1_MessagePart.with {
                    $0.text = Pivox_Ai_V1_TextPart.with { $0.text = text }
                },
            ]
            $0.role = .user
        }
        messages.append(userMsg)
        appendTick &+= 1

        state = .streaming
        inFlightText = ""

        let event = Pivox_Ai_V1_ClientEvent.with {
            $0.message = Pivox_Ai_V1_UserMessage.with {
                $0.conversation = conversationName
                $0.parts = [
                    Pivox_Ai_V1_MessagePart.with {
                        $0.text = Pivox_Ai_V1_TextPart.with { $0.text = text }
                    },
                ]
            }
        }

        streamTask = Task {
            do {
                let eventStream = try client.stream(event)

                for try await serverEvent in eventStream {
                    handle(serverEvent)
                }
                commitInFlight()
            } catch is CancellationError {
                commitInFlight()
            } catch {
                state = .error(error.localizedDescription)
            }
        }
    }

    public func cancel() {
        streamTask?.cancel()
        streamTask = nil
        if state == .streaming {
            commitInFlight()
            state = .idle
        }
    }

    private func handle(_ event: Pivox_Ai_V1_ServerEvent) {
        switch event.event {
        case .textStart:
            inFlightText = ""
        case .textDelta(let delta):
            inFlightText += delta.delta
        case .textEnd:
            commitInFlight()
        case .done:
            state = .idle
        case .streamError(let err):
            state = .error(err.status.message)
        default:
            break
        }
    }

    private func commitInFlight() {
        guard !inFlightText.isEmpty else { return }
        let assistantMsg = Pivox_Ai_V1_Message.with {
            $0.name = Self.localName()
            $0.parts = [
                Pivox_Ai_V1_MessagePart.with {
                    $0.text = Pivox_Ai_V1_TextPart.with { $0.text = inFlightText }
                },
            ]
            $0.role = .assistant
        }
        messages.append(assistantMsg)
        appendTick &+= 1
        inFlightText = ""
        state = .idle
    }

    private static func localName() -> String { "local/\(UUID().uuidString)" }
}
