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
struct Message: View {
    let message: Pivox_Ai_V1_Message
    /// When true, the assistant action row is pinned visible (used for
    /// the latest assistant turn). When false, the row appears on hover
    /// only. User actions are always hover-revealed regardless.
    let pinActions: Bool
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
         pinActions: Bool = false,
         onEditSubmit: ((String) -> Void)? = nil,
         onRegenerate: (() -> Void)? = nil) {
        self.message = message
        self.pinActions = pinActions
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
            if !editing {
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
                // hover. Opacity alone controls visual reveal.
                .opacity(isHovered ? 1 : 0)
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
    /// quiet. Layout order matches Gemini's bubble.
    private var assistantColumn: some View {
        VStack(alignment: .leading, spacing: 6) {
            MarkdownView(textContent)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
            assistantActions
                .padding(.leading, -4)
                .opacity(pinActions || isHovered ? 1 : 0)
                .animation(.easeInOut(duration: 0.12), value: isHovered)
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
