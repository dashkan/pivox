import Foundation
import GRPCCore
import OSLog
import PivoxModels
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

    /// Whether the transcript should follow new content to the
    /// bottom. Starts true. Set to false when the user manually
    /// scrolls up; set back to true when they scroll back to
    /// bottom or send a new message (sending implies "show me the
    /// answer"). The transcript view writes this on user-driven
    /// scroll-position changes; auto-scroll on streaming deltas
    /// and message appends gates on it.
    @Published public var stickToBottom: Bool = true

    /// Latest server-side title for this conversation. Set by the
    /// auto-summarize path after the first turn completes. The
    /// header view observes this and fades in the new title; the
    /// header's bound title elsewhere (from `Conversation.title`)
    /// updates on the next list/get refresh.
    @Published public private(set) var latestTitle: String?

    private let client: ChatClient
    public let conversationName: String
    /// True when the conversation was just created in this session (via the
    /// new-chat flow) and has no server-side history to load. Prevents a
    /// pointless `listMessages` RPC that could race with the first `send()`.
    private let isNew: Bool

    private var streamTask: Task<Void, Never>?
    private var olderCursor: String?

    /// `messages` index of the streaming placeholder while a
    /// response is in flight. We append a real `Pivox_Ai_V1_Message`
    /// to `messages` on the first delta and update its text in
    /// place on subsequent deltas — so the transcript renders the
    /// streaming response as a normal message that grows, not in
    /// a separate scrollable container outside the transcript.
    /// Cleared back to nil when the stream commits / errors.
    private var streamingPlaceholderName: String?

    private static let pageSize: Int32 = 50

    public var canLoadOlder: Bool {
        guard let c = olderCursor else { return false }
        return !c.isEmpty
        // Don't gate on `isLoadingOlder`: that flag toggles during
        // the RPC and, paired with the sentinel's `if canLoadOlder`
        // guard in the view, was unmounting + remounting the
        // sentinel — re-firing its `.onAppear` and pulling every
        // page up-front with no user scrolling. loadOlder() itself
        // still guards against concurrent entry.
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

    public init(client: ChatClient, conversationName: String, isNew: Bool = false) {
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
            let response = try await client.listMessages(request)
            // Server returns newest-first; reverse to oldest→newest for display.
            let ordered = Array(response.messages.reversed())
            Self.prewarmMarkdownCache(for: ordered)
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
            PivoxLog.transcript.debug(
                "loadHistory: +\(ordered.count) msgs, total=\(self.messages.count), nextToken=\(response.nextPageToken.isEmpty ? "nil" : "set")"
            )
        } catch {
            guard state == .loading else { return }
            // NotFound = the conversation was deleted (probably from another
            // session/device). That's not a session error to retry — the
            // resource is gone. Signal the panel so it drops back to New
            // Chat instead of stranding the user on an error card.
            if let rpc = error as? RPCError, rpc.code == .notFound {
                NotificationCenter.default.post(
                    name: .aiChatConversationGone,
                    object: nil,
                    userInfo: ["conversation": conversationName])
                state = .idle
                return
            }
            state = .error(error.localizedDescription)
        }
    }

    /// Fetch one page of older messages and prepend them. View is responsible
    /// for capturing the anchor message id before calling and restoring scroll
    /// after the await returns.
    public func loadOlder() async {
        guard let token = olderCursor, !token.isEmpty else {
            PivoxLog.transcript.debug("loadOlder: skipped (no cursor)")
            return
        }
        guard !isLoadingOlder else {
            PivoxLog.transcript.debug("loadOlder: skipped (already loading)")
            return
        }
        PivoxLog.transcript.debug("loadOlder: starting, token present")
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
            Self.prewarmMarkdownCache(for: ordered)
            messages.insert(contentsOf: ordered, at: 0)
            olderCursor = response.nextPageToken.isEmpty ? nil : response.nextPageToken
            PivoxLog.transcript.debug(
                "loadOlder: +\(ordered.count) msgs, total=\(self.messages.count), nextToken=\(response.nextPageToken.isEmpty ? "nil (no more pages)" : "set")"
            )
        } catch {
            PivoxLog.transcript.error("loadOlder: failed \(error.localizedDescription)")
        }
    }

    /// Pre-parses each message's markdown off the main thread so that
    /// the SwiftUI body hits `MarkdownParser`'s cache instead of
    /// running cmark-gfm synchronously during cell render. This is
    /// what makes `LazyVStack` stutter-free for rich markdown rows.
    private static func prewarmMarkdownCache(for messages: [Pivox_Ai_V1_Message]) {
        for msg in messages {
            // Only assistant messages render markdown; user messages are
            // plain `Text`. Still, warming user messages is a no-op cost.
            let text = msg.parts.compactMap { part -> String? in
                if case .text(let tp) = part.part { return tp.text }
                return nil
            }.joined()
            guard !text.isEmpty else { continue }
            MarkdownParser.parseAsync(text)
        }
    }

    public func send(text: String) {
        // Don't allow sending while already streaming.
        guard state != .streaming else { return }

        // Sending a new prompt is a clear "show me the response"
        // signal — re-engage stick-to-bottom regardless of where
        // the user had scrolled to before.
        stickToBottom = true

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

        let request = Pivox_Ai_V1_GenerateContentRequest.with {
            // Parent is the org segment of the conversation name.
            // Server uses it for routing and tenancy; conversation
            // tells it to load history and persist this turn.
            $0.parent = parentOrgName(from: conversationName)
            $0.conversation = conversationName
            $0.messages = [
                Pivox_Ai_V1_InputMessage.with {
                    $0.role = .user
                    $0.parts = [
                        Pivox_Ai_V1_MessagePart.with {
                            $0.text = Pivox_Ai_V1_TextPart.with { $0.text = text }
                        },
                    ]
                },
            ]
        }

        // First-turn detection: the obvious check is
        // `messages.count == 1` (we just appended the user msg).
        // That's correct for the new-conversation path because
        // `isNew` skips `loadHistory`. The defensive `isNew` arm
        // covers the case where a future change lifts the
        // `loadHistory` skip and history loads in parallel — count
        // would then exceed 1 and auto-summarize would never run
        // for a brand-new conversation. Belt-and-suspenders against
        // a heuristic that would be silently wrong.
        let isFirstTurn = isNew || messages.count == 1
        PivoxLog.chat.info(
            "send: streamGenerateContent starting conversation=\(self.conversationName) firstTurn=\(isFirstTurn)")
        streamTask = Task {
            do {
                let eventStream = client.streamGenerateContent(request)

                for try await serverEvent in eventStream {
                    handle(serverEvent)
                }
                commitInFlight()
                PivoxLog.chat.info("send: stream completed conversation=\(self.conversationName)")
                if isFirstTurn {
                    // Brand-new conversation just finished its first
                    // turn. Trigger auto-summarize in the background;
                    // the title fades in when the response lands.
                    // Server short-circuits if `title_user_set` is
                    // already true (e.g. user typed a title before
                    // sending), so this is safe to fire blindly.
                    Task { await self.autoSummarizeIfNeeded() }
                }
            } catch is CancellationError {
                commitInFlight()
                PivoxLog.chat.debug("send: stream cancelled (re-mount or user navigated away)")
            } catch {
                // gRPC status codes + our server-side error messages
                // don't carry PII so the visible log uses standard
                // interpolation. NSError userInfo (which CAN carry
                // request bodies) goes through `debugSensitive` —
                // DEBUG-only, never reaches release builds.
                PivoxLog.chat.error(
                    "send: stream failed: \(error.localizedDescription)")
                PivoxLog.chat.debugSensitive("send error detail: \(String(reflecting: error))")
                state = .error(error.localizedDescription)
            }
        }
    }

    /// Calls `:summarize` for the current conversation and publishes
    /// the new title via `latestTitle`. Failures are swallowed —
    /// title generation is a side-effect, never blocking the chat
    /// experience. The header falls back to the heuristic prefix
    /// title (or "New Conversation") if this never succeeds.
    private func autoSummarizeIfNeeded() async {
        do {
            let req = Pivox_Ai_V1_SummarizeConversationRequest.with {
                $0.name = self.conversationName
            }
            let updated = try await client.summarizeConversation(req)
            await MainActor.run {
                self.latestTitle = updated.title
            }
        } catch {
            // Intentional silent failure — see docstring.
        }
    }

    /// Extracts the `organizations/{org}` parent prefix from a full
    /// conversation resource name. The server requires `parent` on
    /// `GenerateContentRequest`.
    ///
    /// Post-Phase-7 conversation names are
    /// `organizations/{org}/users/{user}/conversations/{conv}` — the
    /// first two segments still spell the org parent, so taking the
    /// first two segments stays correct.
    private func parentOrgName(from convName: String) -> String {
        let parts = convName.split(separator: "/")
        guard parts.count >= 2 else { return "" }
        return "\(parts[0])/\(parts[1])"
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
        // Drop any events that arrive after a user-initiated cancel.
        // AsyncThrowingStream can buffer deltas between our cancel()
        // call and the iterator actually throwing CancellationError,
        // so without this guard a stray text_delta re-populates
        // inFlightText and commits as a phantom second message.
        guard state == .streaming else { return }
        switch event.event {
        case .textStart:
            inFlightText = ""
            streamingPlaceholderName = nil
        case .textDelta(let delta):
            inFlightText += delta.delta
            applyDeltaToPlaceholder()
        case .textEnd:
            commitInFlight()
        case .done:
            state = .idle
            streamingPlaceholderName = nil
        case .streamError(let err):
            state = .error(err.status.message)
            streamingPlaceholderName = nil
        default:
            break
        }
    }

    /// First delta of a stream: append a placeholder message to
    /// the transcript with the partial text. Subsequent deltas:
    /// update the placeholder's text in place. The transcript
    /// view sees a row that grows, so streaming reads as a normal
    /// message animating in — no separate streaming container.
    private func applyDeltaToPlaceholder() {
        if let name = streamingPlaceholderName,
           let idx = messages.lastIndex(where: { $0.name == name }) {
            messages[idx].parts = [
                Pivox_Ai_V1_MessagePart.with {
                    $0.text = Pivox_Ai_V1_TextPart.with { $0.text = inFlightText }
                }
            ]
            return
        }
        let name = Self.localName()
        streamingPlaceholderName = name
        let placeholder = Pivox_Ai_V1_Message.with {
            $0.name = name
            $0.parts = [
                Pivox_Ai_V1_MessagePart.with {
                    $0.text = Pivox_Ai_V1_TextPart.with { $0.text = inFlightText }
                }
            ]
            $0.role = .assistant
        }
        messages.append(placeholder)
        appendTick &+= 1
    }

    /// Stream finished. Placeholder (if any) is already in the
    /// messages array and IS the final message — we just clear
    /// streaming state. Markdown cache is warmed for the final
    /// text so the next render hits the cache.
    private func commitInFlight() {
        guard !inFlightText.isEmpty else {
            // textEnd without any deltas: state was streaming with
            // no placeholder appended. Just transition back to idle.
            state = .idle
            streamingPlaceholderName = nil
            return
        }
        let committedText = inFlightText
        inFlightText = ""
        state = .idle
        streamingPlaceholderName = nil
        MarkdownParser.parseAsync(committedText)
    }

    private static func localName() -> String { "local/\(UUID().uuidString)" }
}
