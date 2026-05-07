import PivoxModels
import SwiftUI

/// Header title for the active conversation. Click (or double-click)
/// to edit; commits on blur or Return; cancels on Esc. No save
/// button by design — the field is the affordance.
///
/// Three sources feed the displayed title, in priority order:
///
///   1. The user's in-progress edit (the `TextField` content).
///   2. `viewModel.latestTitle` — the auto-summarize result that
///      lands shortly after the first turn completes. This wins
///      over the server's stored title because it's strictly more
///      up-to-date during the brief window before a `GetConversation`
///      refresh.
///   3. The server-loaded `conversation.title` (or the heuristic
///      "New Conversation" fallback when no conversation has been
///      fetched yet).
///
/// Commit posts an `UpdateConversation` with `update_mask=["title"]`
/// — the server flips `title_user_set` to true as a side effect, so
/// later auto-summarize calls become no-ops and the user's curated
/// title sticks.
struct ConversationTitleHeader: View {
    let client: ChatClient
    let conversationName: String
    @ObservedObject var viewModel: ConversationViewModel

    @Environment(\.pivoxTheme) private var theme
    @State private var serverTitle: String = ""
    @State private var draft: String = ""
    @State private var isEditing: Bool = false
    @State private var hovered: Bool = false
    @FocusState private var fieldFocused: Bool

    /// Monotonically-increasing edit counter. Each `commit()`
    /// increments it and captures the new value before firing the
    /// `UpdateConversation` RPC. The async response only applies if
    /// its captured ID still equals `latestEditID` — so a slow
    /// in-flight RPC from edit N can't clobber edit N+1's
    /// optimistic state when the responses arrive out of order.
    @State private var latestEditID: Int = 0

    var body: some View {
        Group {
            if isEditing {
                TextField("Conversation title", text: $draft, onCommit: commit)
                    .textFieldStyle(.plain)
                    .font(theme.headingFont)
                    .foregroundStyle(theme.textPrimary)
                    .multilineTextAlignment(.center)
                    .focused($fieldFocused)
                    .onChange(of: fieldFocused) { _, focused in
                        // onCommit fires for Return; blur (focus lost)
                        // also commits. Esc handled via .onExitCommand.
                        if !focused { commit() }
                    }
                    .onExitCommand { cancel() }
                    .frame(maxWidth: 280)
            } else {
                // Three layered discoverability cues for what's
                // otherwise an invisible click-to-edit affordance:
                // (1) text I-beam cursor on hover — macOS HIG signal
                // for "this is editable text"; (2) subtle background
                // tint on hover so the hit area is visible;
                // (3) tooltip explaining the action. Without these,
                // users have no way to know the title is clickable.
                // Ghost-text-field affordance: title text alone at
                // rest (no glyph means no SF-Symbol-vs-text optical
                // misalignment), with a faint rounded border + fill
                // appearing on hover. The I-beam cursor is the
                // macOS-native "this is editable text" cue, and the
                // tooltip spells it out for users who don't read
                // cursor changes. Matches the resting state's
                // visual quietness while still being discoverable
                // the moment a user hovers.
                Text(displayTitle)
                    .font(theme.headingFont)
                    .foregroundStyle(theme.textPrimary)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .frame(height: 32)
                    .padding(.horizontal, theme.spacingSM)
                    .background(
                        RoundedRectangle(cornerRadius: theme.radiusSM)
                            .fill(hovered ? theme.hoverFill : Color.clear)
                    )
                    .overlay(
                        RoundedRectangle(cornerRadius: theme.radiusSM)
                            .strokeBorder(
                                hovered ? theme.border : Color.clear,
                                lineWidth: 1)
                    )
                    // Tooltip shows the full (untruncated) title for
                    // users hitting "…" truncation. Editability is
                    // already conveyed by the I-beam cursor and
                    // hover border, so no verbal hint here.
                    .help(displayTitle)
                    .frame(maxWidth: 280)
                    .contentShape(Rectangle())
                    .onTapGesture(count: 1) { startEditing() }
                    .onHover { isHover in
                        hovered = isHover
                        if isHover {
                            NSCursor.iBeam.push()
                        } else {
                            NSCursor.pop()
                        }
                    }
            }
        }
        .task(id: conversationName) { await loadTitle() }
        .onChange(of: viewModel.latestTitle) { _, _ in
            // Auto-summary just landed — refresh from the server so
            // we have the canonical resource state (including
            // title_user_set, etag) for any subsequent edits.
            Task { await loadTitle() }
        }
    }

    private var displayTitle: String {
        if let t = viewModel.latestTitle, !t.isEmpty { return t }
        if !serverTitle.isEmpty { return serverTitle }
        return "New Conversation"
    }

    private func startEditing() {
        draft = displayTitle == "New Conversation" ? "" : displayTitle
        isEditing = true
        DispatchQueue.main.async { fieldFocused = true }
    }

    private func commit() {
        defer { isEditing = false }
        let trimmed = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        // Empty input → keep existing title rather than committing
        // a blank. Spec was explicit: never allow empty.
        guard !trimmed.isEmpty, trimmed != serverTitle else { return }

        latestEditID &+= 1
        let editID = latestEditID
        let convName = conversationName
        let newTitle = trimmed
        Task {
            do {
                let req = Pivox_Ai_V1_UpdateConversationRequest.with {
                    $0.conversation = Pivox_Ai_V1_Conversation.with {
                        $0.name = convName
                        $0.title = newTitle
                    }
                    $0.updateMask = .with { $0.paths = ["title"] }
                }
                let updated = try await client.updateConversation(req)
                await MainActor.run {
                    // Drop responses from superseded edits — the
                    // user has already typed a newer title, and
                    // applying this stale response would clobber
                    // their optimistic state.
                    guard self.latestEditID == editID else { return }
                    self.serverTitle = updated.title
                }
            } catch {
                // Revert local optimistic state on failure, but
                // only if no newer edit is in flight — otherwise
                // the newer commit's optimistic value is correct
                // and reverting would discard valid user input.
                await MainActor.run {
                    guard self.latestEditID == editID else { return }
                }
                await loadTitle()
            }
        }
        // Optimistic update so the user sees the title change
        // immediately, even before the RPC round-trip completes.
        serverTitle = newTitle
    }

    private func cancel() {
        isEditing = false
        draft = ""
    }

    private func loadTitle() async {
        do {
            let req = Pivox_Ai_V1_GetConversationRequest.with {
                $0.name = conversationName
            }
            let conv = try await client.getConversation(req)
            await MainActor.run { self.serverTitle = conv.title }
        } catch {
            // Silent: header falls back to "New Conversation".
        }
    }
}
