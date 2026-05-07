import PivoxModels
import SwiftUI

/// Role-aware chat message container.
///
/// - **User**: right-aligned pill bubble, max 520pt wide. Action row
///   below offers edit + copy.
/// - **Assistant**: left-aligned, full-width, markdown-rendered with no
///   bubble chrome. Action row below follows Gemini's shape — thumbs
///   up / thumbs down / redo / copy, always visible with tooltips.
///
/// Feedback buttons are UI-only in v1 (no BE wiring yet). Redo fires the
/// `onRegenerate` callback which the view resolves by resending the last
/// user message that preceded this assistant reply.
/// How the assistant's (and user's) action row participates in
/// the row's layout.
///
/// The distinction matters because the previous boolean
/// `pinActions` only toggled the row's *opacity* — the row was
/// always present in the layout and reserved its height. That's
/// fine for the hover-reveal and pinned cases, but breaks the
/// "we don't want this row at all right now" case (streaming
/// placeholder): the row reserved its space, the icons were
/// invisible due to opacity, and the user saw an empty gap below
/// the streaming text. Tri-state keeps "pinned" and "hidden"
/// from being expressible together.
enum MessageActionsVisibility {
    /// Action row is always visible. Layout includes it.
    case pinned
    /// Action row is in the layout but hidden until row hover.
    case hoverReveal
    /// Action row is omitted from the layout entirely. No
    /// reserved height, no hover affordance. Used for the
    /// streaming-placeholder case where there's nothing
    /// actionable about a half-rendered response.
    case suppressed
}

struct Message: View {
    let message: Pivox_Ai_V1_Message
    /// How the action row participates in this message's layout.
    /// See `MessageActionsVisibility` for the three cases.
    let actionsVisibility: MessageActionsVisibility
    /// Called when the user confirms an edit on a user message. Receives
    /// the edited text. `nil` disables the edit affordance.
    let onEditSubmit: ((String) -> Void)?
    /// Called when the user hits "redo" on an assistant message. No args —
    /// the container has enough context to find the prior user turn.
    /// `nil` disables the redo affordance.
    let onRegenerate: (() -> Void)?

    @State private var editing = false
    @State private var editedText = ""

    // Feedback — UI-only today. When BE support lands, these wire into a
    // `ReportMessageFeedback` RPC or similar; for now they toggle
    // locally so the user can see the click register.
    @State private var thumbsUp = false
    @State private var thumbsDown = false

    init(message: Pivox_Ai_V1_Message,
         actionsVisibility: MessageActionsVisibility = .hoverReveal,
         onEditSubmit: ((String) -> Void)? = nil,
         onRegenerate: (() -> Void)? = nil) {
        self.message = message
        self.actionsVisibility = actionsVisibility
        self.onEditSubmit = onEditSubmit
        self.onRegenerate = onRegenerate
    }

    var body: some View {
        let isUser = message.role == .user
        if isUser {
            userRow
        } else {
            assistantColumn
        }
    }

    /// User turn: hover-revealed action icons *adjacent to* the bubble
    /// on its left edge (Gemini-style), not pushed to the panel's far
    /// left. Icons align with the top of the bubble so they sit next
    /// to the first line of text. The outer Spacer flexes to fill
    /// whatever's left of the panel, keeping the icon+bubble group
    /// right-aligned.
    private var userRow: some View {
        HStack(alignment: .top, spacing: 6) {
            Spacer(minLength: 0)
            if !editing, actionsVisibility != .suppressed {
                HStack(spacing: 2) {
                    IconButton(systemName: "doc.on.doc",
                               label: "Copy prompt") { copy() }
                    IconButton(systemName: "pencil",
                               label: "Edit prompt",
                               action: beginEdit)
                }
                // Push icons down so they line up with the first text
                // line inside the bubble (bubble has .padding(.vertical, 10);
                // this offsets the icon center to roughly that baseline).
                .padding(.top, 5)
                // Always hit-testable so macOS tooltips can fire during
                // hover. Opacity alone controls visual reveal — the
                // icons sit at width 0 of the bubble's leading edge
                // when hidden, so reflow on hover is invisible.
                .opacity(actionsVisibility == .pinned || isHovered ? 1 : 0)
                .animation(.easeInOut(duration: 0.12), value: isHovered)
            }
            // Bubble sizes to content. SwiftUI's .frame(maxWidth:)
            // forces its *frame* to expand to that width regardless of
            // the child's desired size (then centers inside), which
            // put a big gap between icons and bubble. Natural sizing
            // + the HStack Spacer keeps icons+bubble as a tight
            // right-aligned group. Long prompts still fit — the
            // HStack gives the bubble all remaining horizontal space
            // and Text wraps.
            userContent
        }
        .contentShape(Rectangle())
        .onHover { isHovered = $0 }
    }

    /// Assistant turn: full-width markdown body with an action row below
    /// (thumbs up / down / redo / copy). Last assistant message pins the
    /// row; earlier ones reveal on hover so old turns stay visually
    /// quiet. Streaming placeholders suppress the row entirely so the
    /// half-rendered response doesn't sit above an empty action-strip
    /// gap. Layout order matches Gemini's bubble.
    private var assistantColumn: some View {
        VStack(alignment: .leading, spacing: 6) {
            MarkdownView(textContent)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
            // Suppressed → action row is omitted from the layout so
            // its height isn't reserved. Pinned/hoverReveal → row
            // is in the layout; opacity drives visual reveal so
            // hover doesn't reflow the cell.
            if actionsVisibility != .suppressed {
                assistantActions
                    .padding(.leading, -4)
                    .opacity(actionsVisibility == .pinned || isHovered ? 1 : 0)
                    .animation(.easeInOut(duration: 0.12), value: isHovered)
            }
        }
        .contentShape(Rectangle())
        .onHover { isHovered = $0 }
    }

    /// Hover state — only used for the user row (assistant actions are
    /// always visible). Lives on Message because both the bubble and
    /// the action HStack need to share it.
    @State private var isHovered = false

    @ViewBuilder
    private var userContent: some View {
        if editing {
            userEditor
        } else {
            userBubble
        }
    }

    private var userBubble: some View {
        // Content-sized bubble — no outer frame expansion. Parent row's
        // Spacer handles right-alignment; width cap is applied on the
        // HStack wrapper so long prompts wrap at 520pt.
        Text(textContent)
            .textSelection(.enabled)
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .background(Color.secondary.opacity(0.18))
            .clipShape(RoundedRectangle(cornerRadius: 14))
    }

    private var userEditor: some View {
        VStack(alignment: .trailing, spacing: 6) {
            // Multi-line TextField (axis: .vertical) keeps onSubmit
            // semantics: plain Enter submits, Shift+Enter inserts a
            // newline. SwiftUI's TextEditor doesn't expose onSubmit
            // because it treats Enter as a newline unconditionally —
            // which is why the previous version never submitted on
            // Enter. lineLimit caps visual growth before scroll kicks
            // in.
            TextField("", text: $editedText, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.body)
                .lineLimit(1...12)
                .padding(.horizontal, 10)
                .padding(.vertical, 8)
                .background(Color.secondary.opacity(0.18))
                .clipShape(RoundedRectangle(cornerRadius: 14))
                .onSubmit { submitEdit() }
                .onExitCommand { cancelEdit() }

            HStack(spacing: 8) {
                Button("Cancel", action: cancelEdit)
                    .keyboardShortcut(.cancelAction)
                Button("Send") { submitEdit() }
                    .disabled(editedText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .controlSize(.small)
        }
    }

    @ViewBuilder
    private var assistantActions: some View {
        MessageActions {
            IconButton(systemName: thumbsUp ? "hand.thumbsup.fill" : "hand.thumbsup",
                          label: "Good response",
                          isOn: thumbsUp) { toggleThumbs(up: true) }
            IconButton(systemName: thumbsDown ? "hand.thumbsdown.fill" : "hand.thumbsdown",
                          label: "Bad response",
                          isOn: thumbsDown) { toggleThumbs(up: false) }
            if onRegenerate != nil {
                IconButton(systemName: "arrow.clockwise",
                              label: "Redo",
                              action: { onRegenerate?() })
            }
            IconButton(systemName: "doc.on.doc",
                          label: "Copy response") { copy() }
        }
    }

    // MARK: - Actions

    private func copy() { MessagePasteboard.copy(textContent) }

    private func toggleThumbs(up: Bool) {
        // Feedback is mutually exclusive — voting up clears down and
        // vice versa. Tapping the same direction again un-votes.
        if up {
            thumbsUp.toggle()
            if thumbsUp { thumbsDown = false }
        } else {
            thumbsDown.toggle()
            if thumbsDown { thumbsUp = false }
        }
    }

    private func beginEdit() {
        editedText = textContent
        editing = true
    }

    private func cancelEdit() {
        editing = false
        editedText = ""
    }

    private func submitEdit() {
        let trimmed = editedText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, let cb = onEditSubmit else { return }
        editing = false
        editedText = ""
        cb(trimmed)
    }

    private var textContent: String {
        message.parts.compactMap { part in
            if case .text(let tp) = part.part { return tp.text }
            return nil
        }.joined()
    }
}

/// Streaming variant — renders a partial assistant text with the
/// incomplete-markdown fixer applied, so unclosed fences/lists/emphasis
/// don't visually explode mid-generation. No action row: actions appear
/// only after the turn commits.
struct InFlightAssistantMessage: View {
    let text: String

    var body: some View {
        HStack(alignment: .top) {
            MarkdownView(text, streaming: true)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
            Spacer(minLength: 0)
        }
    }
}

// MARK: - Previews

#if DEBUG

/// Preview helpers — minimal `Pivox_Ai_V1_Message` builders so the
/// canvas doesn't need a live conversation to render. Keep these
/// constrained to DEBUG so release builds don't carry preview-only
/// scaffolding.
private extension Pivox_Ai_V1_Message {
    static func sampleUser(_ text: String) -> Pivox_Ai_V1_Message {
        Pivox_Ai_V1_Message.with {
            $0.name = "preview/user/\(UUID().uuidString)"
            $0.role = .user
            $0.parts = [
                Pivox_Ai_V1_MessagePart.with {
                    $0.text = Pivox_Ai_V1_TextPart.with { $0.text = text }
                },
            ]
        }
    }

    static func sampleAssistant(_ markdown: String) -> Pivox_Ai_V1_Message {
        Pivox_Ai_V1_Message.with {
            $0.name = "preview/assistant/\(UUID().uuidString)"
            $0.role = .assistant
            $0.parts = [
                Pivox_Ai_V1_MessagePart.with {
                    $0.text = Pivox_Ai_V1_TextPart.with { $0.text = markdown }
                },
            ]
        }
    }
}

#Preview("User — short prompt") {
    Message(
        message: .sampleUser("What's the difference between an actor and a class in Swift 6?"),
        actionsVisibility: .hoverReveal
    )
    .padding()
    .frame(width: 360)
}

#Preview("Assistant — pinned (latest reply)") {
    Message(
        message: .sampleAssistant("""
        The key difference: **actors** isolate their state to one task at a time.

        - Classes share state freely; you must lock manually.
        - Actors serialize access through the actor's mailbox.
        - Crossing an actor boundary requires `await`.
        """),
        actionsVisibility: .pinned,
        onRegenerate: {}
    )
    .padding()
    .frame(width: 360)
}

#Preview("Assistant — hover-reveal (older reply)") {
    Message(
        message: .sampleAssistant("Earlier in the conversation. Hover to see actions."),
        actionsVisibility: .hoverReveal
    )
    .padding()
    .frame(width: 360)
}

#Preview("Assistant — suppressed (streaming)") {
    Message(
        message: .sampleAssistant("Mid-stream — the action row is omitted from the layout, so there's no reserved gap below the partial response."),
        actionsVisibility: .suppressed
    )
    .padding()
    .frame(width: 360)
}

/// Exercises the full `Message → MarkdownView → CodeBlockView`
/// rendering path — prose + a fenced code block in one assistant
/// turn, the most common shape of a real chat response. The
/// standalone `CodeBlockView` previews in `MarkdownView.swift`
/// only cover the code-block in isolation; this one catches
/// integration regressions (spacing between prose and the code
/// block, action-row placement under variable content height,
/// fade-mask interaction with the surrounding bubble).
#Preview("Assistant — prose + code block") {
    Message(
        message: .sampleAssistant("""
        Here's a small Swift helper that resolves the auth token,
        refreshing if expired:

        ```swift
        func resolveToken(refresh: Bool = false) async throws -> String {
            if let cached = tokenCache.current, !refresh {
                return cached
            }
            let fresh = try await session.refresh()
            tokenCache.store(fresh, ttl: .minutes(30))
            return fresh
        }
        ```

        Call `resolveToken(refresh: true)` to force a refresh.
        """),
        actionsVisibility: .pinned
    )
    .padding()
    .frame(width: 360)
}

#Preview("Assistant — markdown stress test") {
    Message(
        message: .sampleAssistant("""
        ## Header
        Body with **bold** and *italic* and `inline code`.

        - Bullet
        - Another bullet
          - Nested

        > A blockquote.

        Closing paragraph with [a link](https://example.com).
        """),
        actionsVisibility: .pinned
    )
    .padding()
    .frame(width: 360)
}

#endif
