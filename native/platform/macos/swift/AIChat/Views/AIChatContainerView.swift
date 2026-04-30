import OSLog
import PivoxModels
import SwiftUI

extension Notification.Name {
    /// Posted by the ⌘⇧A hotkey to ask the chat panel to grab focus
    /// on its message input. Toggling the panel via the toolbar button
    /// does *not* post this — focus stays on the button there, so a
    /// keyboard user activating the toggle with Space isn't thrown
    /// across the window.
    static let aiChatFocusRequested = Notification.Name("pivox.aiChat.focusRequested")

    /// Posted by the jump-to-latest pill in the transcript when the
    /// user clicks it (and by the ⌘↓ keyboard shortcut). The
    /// transcript coordinator listens, scrolls to bottom, and
    /// re-engages stick-to-bottom.
    static let aiChatJumpToLatest = Notification.Name("pivox.aiChat.jumpToLatest")

    /// Posted by the ⌘↑ keyboard shortcut. The transcript
    /// coordinator advances toward the top in tiered steps — first
    /// to just above the pagination trigger, then a single explicit
    /// `loadOlder()` if more pages exist, finally to absolute top
    /// when no older pages remain. Tiered so a single keypress
    /// never cascades multiple pagination loads.
    static let aiChatScrollUp = Notification.Name("pivox.aiChat.scrollUp")

    /// Posted by `ConversationViewModel.loadHistory()` when the
    /// server returns NotFound — the conversation was deleted in
    /// another session. The panel drops back to New Chat rather
    /// than stranding the user on an error card. `userInfo`
    /// carries the gone resource name under "conversation".
    static let aiChatConversationGone = Notification.Name("pivox.aiChat.conversationGone")
}

/// AI chat surface. Renders whichever stage the shared
/// `AIChatService` is in: progress while connecting, error if
/// init failed, the chat panel once a `ChatClient` is available.
///
/// The view itself owns no `ChatClient` — that's lifted into
/// `AIChatService.shared` so a dock ↔ detach mode swap can
/// re-mount the container without tearing down the gRPC channel.
struct AIChatContainerView: View {
    private var service = AIChatService.shared

    var body: some View {
        Group {
            if let client = service.client {
                AIChatPanel(client: client, orgName: service.orgName)
            } else if let initError = service.initError {
                Text("Chat unavailable: \(initError)")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ProgressView("Connecting...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .task {
            await service.connect()
        }
    }
}

/// The chat panel: starts with a new conversation, can switch to history.
struct AIChatPanel: View {
    let client: ChatClient
    let orgName: String

    @State private var conversationName: String?
    @State private var pendingMessage: String?
    @State private var showHistoryPopover = false
    // Absorbs the default first-responder selection when the panel
    // first mounts so no visible focus ring lands on the history
    // IconButton (first focusable child). Kept non-visible via
    // `.focusEffectDisabled()`. User Tab / arrow nav takes over
    // once they actually press a key.
    @FocusState private var panelFocused: Bool
    // Owned at the panel level so the popover reuses the same loaded list
    // across open/close cycles — no refetch on every open.
    @StateObject private var historyVM: ConversationListViewModel

    private static let lastConversationKey = "ai_chat_last_conversation"

    init(client: ChatClient, orgName: String) {
        self.client = client
        self.orgName = orgName
        _historyVM = StateObject(wrappedValue: ConversationListViewModel(client: client, orgName: orgName))
        // Restore last-opened conversation across launches. If the
        // server has since deleted it, ConversationView's loadHistory
        // will surface the error and the user picks another from
        // history or starts new.
        // Only restore if the saved name belongs to the current org —
        // a fresh DB or an org switch leaves a stale pointer that
        // would 404 / PermissionDenied on first load.
        let saved = AppStateBridge.shared().loadString(forKey: Self.lastConversationKey)
        let prefix = "organizations/\(orgName)/"
        let valid = (saved?.isEmpty == false) && (saved?.hasPrefix(prefix) == true)
        _conversationName = State(initialValue: valid ? saved : nil)
    }

    var body: some View {
        VStack(spacing: 0) {
            // Header bar
            HStack {
                IconButton(systemName: "clock.arrow.circlepath", label: "Show conversation history", help: "Conversation history") {
                    showHistoryPopover.toggle()
                }
                .popover(isPresented: $showHistoryPopover, arrowEdge: .top) {
                    ConversationHistoryPopover(viewModel: historyVM) { conv in
                        conversationName = conv.name
                        pendingMessage = nil
                        showHistoryPopover = false
                        // Conversation selection is a commit — user
                        // chose THIS conversation, next action is
                        // compose. Focus input regardless of whether
                        // they clicked or pressed Return; differs
                        // from the toolbar toggle (where kb Space
                        // might be a transient "peek").
                        DispatchQueue.main.async {
                            NotificationCenter.default.post(
                                name: .aiChatFocusRequested, object: nil)
                        }
                    }
                    // Match popover width to the AI panel by sizing against
                    // the enclosing window reference dimension. 360pt
                    // approximates the panel's right-side width in practice.
                    .frame(minWidth: 340, idealWidth: 360, maxWidth: 420, minHeight: 280, idealHeight: 400, maxHeight: 600)
                }

                Spacer()

                if let name = conversationName,
                   // Pass the same `isNew` semantics here as
                   // `ActiveConversationView` uses below — without
                   // this, the title header runs first in the
                   // render pass and creates the VM with
                   // `isNew=false`, after which `ActiveConversationView`
                   // gets a cached VM that's already wrong.
                   // `loadHistory()` then runs on a brand-new
                   // conversation and races the just-sent first
                   // message.
                   let vm = AIChatService.shared.viewModel(
                    for: name, isNew: pendingMessage != nil) {
                    ConversationTitleHeader(
                        client: client,
                        conversationName: name,
                        viewModel: vm)
                } else {
                    Text("New Conversation")
                        .font(.headline)
                }

                Spacer()

                IconButton(systemName: "plus.bubble", label: "New conversation", help: "New conversation") {
                    conversationName = nil
                    pendingMessage = nil
                    // "New conversation" is a commit: user intends
                    // to start composing right now. Focus the input
                    // regardless of mouse vs keyboard activation
                    // (same reasoning as history-popover selection).
                    DispatchQueue.main.async {
                        NotificationCenter.default.post(
                            name: .aiChatFocusRequested, object: nil)
                    }
                }

                // Mode-toggle pair. We render whichever direction
                // makes sense for the current mode in the SwiftUI
                // header (rather than putting either action in the
                // floating panel's NSToolbar) so both buttons use
                // the same IconButton component, the same theme
                // size, and visually match every other icon in
                // this row. NSToolbar items in `.utilityWindow`
                // panels render small by AppKit design — HIG-correct
                // for tool palettes — but mixing those small icons
                // with the rest of the header's IconButton sizing
                // looks inconsistent.
                if AIChatState.shared.mode == .docked {
                    IconButton(
                        systemName: "arrow.up.right.square",
                        label: "Open AI Chat in a floating window",
                        help: "Open in window"
                    ) {
                        AppDelegate.shared?.detachAIChat()
                    }
                } else {
                    IconButton(
                        systemName: "arrow.down.left.square",
                        label: "Move AI Chat back into the main window",
                        help: "Show in main window"
                    ) {
                        AppDelegate.shared?.dockAIChat()
                    }
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)

            Divider()

            if let name = conversationName {
                ActiveConversationView(
                    client: client,
                    conversationName: name,
                    initialMessage: pendingMessage,
                    isNew: pendingMessage != nil
                )
                .id(name)
            } else {
                NewChatView(client: client, orgName: orgName) { name, message in
                    pendingMessage = message
                    conversationName = name
                }
            }
        }
        // No `.background(...)` here — the parent
        // (`InlineAIChatPanel`'s wrapper, or the detached
        // `AIChatWindowController`) owns the backdrop. The float
        // layout uses `.thinMaterial` for Liquid Glass; setting an
        // opaque background here would paint on top and defeat
        // the bleed-through. Push layout sets a solid backdrop in
        // its wrapper.
        .focusable()
        .focused($panelFocused)
        .focusEffectDisabled()
        .onAppear { panelFocused = true }
        .onChange(of: conversationName) { _, newName in
            // Empty string clears the persisted value (saved as blank)
            // so a fresh launch lands on New Chat instead of the last
            // conversation the user explicitly dismissed.
            AppStateBridge.shared().save(newName ?? "",
                                         forKey: Self.lastConversationKey)
        }
        .onReceive(NotificationCenter.default.publisher(for: .aiChatConversationGone)) { note in
            // Server says the conversation no longer exists. Match
            // against the *currently displayed* name so a stale
            // notification from a previously-viewed conversation
            // doesn't yank the user out of a different one. Fall
            // back to New Chat.
            guard let gone = note.userInfo?["conversation"] as? String,
                  gone == conversationName else { return }
            conversationName = nil
            pendingMessage = nil
        }
    }
}

/// Reads the long-lived `ConversationViewModel` for a given
/// conversation from `AIChatService` instead of owning a
/// `@StateObject`. This is what makes dock/detach safe for an
/// in-flight stream: when the view tree re-mounts after a mode
/// swap, the same view model — including its active streaming
/// Task — is still alive in the service and the new view simply
/// re-attaches to it.
struct ActiveConversationView: View {
    @ObservedObject var viewModel: ConversationViewModel
    private let initialMessage: String?

    init?(client: ChatClient, conversationName: String, initialMessage: String? = nil, isNew: Bool = false) {
        guard let vm = AIChatService.shared.viewModel(
            for: conversationName, isNew: isNew) else { return nil }
        self.viewModel = vm
        self.initialMessage = initialMessage
    }

    var body: some View {
        ConversationView(viewModel: viewModel)
            .onAppear {
                // Send before ConversationView's .task fires loadHistory().
                // send() is synchronous — sets state to .streaming immediately.
                // loadHistory() will see state != .idle and skip.
                if let msg = initialMessage, !msg.isEmpty {
                    viewModel.send(text: msg)
                }
            }
    }
}

/// The initial view when no conversation exists — just a prompt input.
struct NewChatView: View {
    let client: ChatClient
    let orgName: String
    let onCreated: (String, String) -> Void

    @State private var inputText = ""
    @State private var isCreating = false
    @FocusState private var inputFocused: Bool

    var body: some View {
        VStack {
            Spacer()

            Image(systemName: "bubble.left.and.text.bubble.right")
                .font(.largeTitle)
                .foregroundStyle(.tertiary)
                .padding(.bottom, 8)

            Text("Start a conversation")
                .font(.title3)
                .foregroundStyle(.secondary)

            Spacer()

            // Prompt input at the bottom. New-chat flow has no
            // streaming state of its own — the conversation is
            // created on send, and from there `ConversationView`
            // takes over with the live composer.
            ShimmerPromptField(
                text: $inputText,
                placeholder: "Message...",
                isEnabled: !isCreating,
                isStreaming: false,
                onSubmit: sendFirst,
                onCancel: {},
                focused: $inputFocused,
                toolItems: { ChatAttachmentMenuButton() })
                .padding(12)
                .onReceive(NotificationCenter.default.publisher(for: .aiChatFocusRequested)) { _ in
                    // Only the hotkey path posts this; clicking or
                    // Space-activating the toolbar button opens the
                    // chat without stealing focus.
                    DispatchQueue.main.async { inputFocused = true }
                }
        }
    }

    private func sendFirst() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !isCreating else { return }
        isCreating = true

        Task {
            do {
                let userID = await AIChatService.shared.pivoxUserID()
                if userID.isEmpty {
                    // Empty claim usually means the ID token was minted
                    // before the syncIdentity blocking-fn deployment, or
                    // the token cached on the client predates a Pivox
                    // identity-id rotation (e.g. dev DB reseed).
                    PivoxLog.chat.error(
                        "sendFirst: pivox_user_id claim missing — token refresh needed")
                    isCreating = false
                    return
                }
                let request = Pivox_Ai_V1_CreateConversationRequest.with {
                    // Per-user parent post-Phase-7. The user-uuid
                    // comes from the `pivox_user_id` ID-token claim.
                    $0.parent = "organizations/\(orgName)/users/\(userID)"
                    $0.conversation = Pivox_Ai_V1_Conversation.with {
                        $0.title = String(text.prefix(50))
                    }
                }
                let conv = try await client.createConversation(request)
                PivoxLog.chat.info("sendFirst: createConversation OK name=\(conv.name)")
                inputText = ""
                onCreated(conv.name, text)
            } catch {
                // gRPC status codes + our server-side error messages
                // don't carry PII so the visible log uses standard
                // interpolation. NSError userInfo (which CAN carry
                // request bodies) goes through `debugSensitive` —
                // DEBUG-only, never reaches release builds.
                PivoxLog.chat.error(
                    "sendFirst: createConversation failed: \(error.localizedDescription)")
                PivoxLog.chat.debugSensitive("sendFirst error detail: \(String(reflecting: error))")
                isCreating = false
            }
        }
    }
}
