import Foundation
import SwiftUI

@MainActor
public final class ConversationViewModel: ObservableObject {
    @Published public var messages: [Pivox_Ai_V1_Message] = []
    @Published public var inFlightText: String = ""
    @Published public var state: ConversationState = .idle

    private let client: ChatClient
    public let conversationName: String

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

    public init(client: ChatClient, conversationName: String) {
        self.client = client
        self.conversationName = conversationName
    }

    public func loadHistory() async {
        guard state == .idle else { return }
        state = .loading
        do {
            let request = Pivox_Ai_V1_ListMessagesRequest.with {
                $0.parent = conversationName
                $0.pageSize = 100
            }
            let response = try await client.listMessages(request)
            // Re-check after suspension — send() may have run while we awaited.
            guard state == .loading else { return }
            messages = response.messages
            state = .idle
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
            // Open bidi stream, send message, pump events.
            let eventStream = client.stream()

            do {
                try client.send(event)
            } catch {
                state = .error(error.localizedDescription)
                return
            }

            do {
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
