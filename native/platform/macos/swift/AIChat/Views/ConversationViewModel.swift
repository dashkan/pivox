import Foundation
import SwiftUI

@MainActor
public final class ConversationViewModel: ObservableObject {
    @Published public var messages: [Pivox_Ai_V1_Message] = []
    @Published public var inFlightText: String = ""
    @Published public var state: ConversationState = .idle

    private let client: any ChatClientProtocol
    public let conversationName: String
    /// True when the conversation was just created in this session (via the
    /// new-chat flow) and has no server-side history to load. Prevents a
    /// pointless `listMessages` RPC that could race with the first `send()`.
    private let isNew: Bool

    private var streamTask: Task<Void, Never>?

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
        guard state == .idle else { return }
        state = .loading
        do {
            let request = Pivox_Ai_V1_ListMessagesRequest.with {
                $0.parent = conversationName
                $0.pageSize = 100
            }
            let response = try await client.listMessages(request)
            // Re-check after suspension — send() may have run while we awaited.
            if state == .loading {
                messages = response.messages
                state = .idle
            } else {
                // User sent a message while we were loading. Prepend history
                // before the user's message instead of discarding it.
                messages.insert(contentsOf: response.messages, at: 0)
            }
        } catch {
            guard state == .loading else { return }
            state = .error(error.localizedDescription)
        }
    }

    public func send(text: String) {
        // Don't allow sending while already streaming.
        guard state != .streaming else { return }

        // Add user message to the local list immediately.
        let userMsg = Pivox_Ai_V1_Message.with {
            $0.parts = [
                Pivox_Ai_V1_MessagePart.with {
                    $0.text = Pivox_Ai_V1_TextPart.with { $0.text = text }
                },
            ]
            $0.role = .user
        }
        messages.append(userMsg)

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
            $0.parts = [
                Pivox_Ai_V1_MessagePart.with {
                    $0.text = Pivox_Ai_V1_TextPart.with { $0.text = inFlightText }
                },
            ]
            $0.role = .assistant
        }
        messages.append(assistantMsg)
        inFlightText = ""
        state = .idle
    }
}
