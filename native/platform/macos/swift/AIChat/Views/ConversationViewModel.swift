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
        state = .loading
        do {
            let request = Pivox_Ai_V1_ListMessagesRequest.with {
                $0.parent = conversationName
                $0.pageSize = 100
            }
            let response = try await client.listMessages(request)
            messages = response.messages
            state = .idle
        } catch {
            state = .error(error.localizedDescription)
        }
    }

    public func send(text: String) {
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

        do {
            try client.send(event)
        } catch {
            state = .error(error.localizedDescription)
            return
        }

        // Add the user message to the local list immediately.
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
        streamTask = Task { await pumpStream() }
    }

    public func cancel() {
        streamTask?.cancel()
        streamTask = nil
        if state == .streaming {
            state = .idle
        }
    }

    private func pumpStream() async {
        do {
            for try await event in client.stream() {
                handle(event)
            }
            commitInFlight()
        } catch is CancellationError {
            commitInFlight()
        } catch {
            state = .error(error.localizedDescription)
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
