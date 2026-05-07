import AppKit
import PivoxModels
import SwiftProtobuf
import SwiftUI

/// Dropdown popover listing recent conversations. Opened from the history
/// button in the AI chat header.
///
/// ## Keyboard
/// Matches macOS HIG for popover-with-search patterns (Spotlight, Safari
/// address bar, Finder Go To Folder):
///
///  * Popover opens → search field focused.
///  * `↓` from search → focus first list row; `↑/↓` arrow-nav rows.
///  * `↑` from first row → bounce back to search (keeps filter editable).
///  * `Return` on a focused row → open that conversation.
///  * `⌘R` anywhere → refresh.
///  * `⌘Delete` on a focused row → delete that conversation.
///  * `Esc`: clears the filter if non-empty, otherwise dismisses the
///    popover.
///
/// ## Other features
///  * Client-side filter by title (case-insensitive substring).
///  * Refresh: small chrome icon next to the search field, ⌘R
///    keyboard shortcut. `.refreshable` was tried and abandoned —
///    macOS list-in-popover doesn't surface the overscroll-pull
///    gesture reliably (Mail / Spotlight / Notes don't even use
///    it), so a visible chrome target plus the conventional
///    ⌘R is the load-bearing path.
///  * Infinite scroll on bottom edge — last row entering viewport
///    fires loadMore.
///  * Inline rename (hover row → pencil → in-place TextField,
///    Return/Esc commits/cancels).
///  * Inline delete with optimistic UI + scale/fade transition. No
///    confirmation dialog (the row's own optimistic disappearance
///    plus the hover-reveal targeting is already a high enough bar
///    against accidents; if we ever want a safety net we'll add an
///    undo banner).
///
/// ## Why `List(selection:)` and `.listStyle(.sidebar)`
/// `List(selection:)` bridges to AppKit's responder chain and gives
/// us arrow-key nav, selection highlight, and VoiceOver row
/// semantics for free. `.sidebar` style picks up the system's
/// vibrancy + selection treatment that matches Mail / Notes /
/// Music popovers — solid system look without per-control styling.
public struct ConversationHistoryPopover: View {
    @ObservedObject var viewModel: ConversationListViewModel
    let onSelect: (Pivox_Ai_V1_Conversation) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var searchText: String = ""
    @State private var selection: String?
    @FocusState private var focus: Focus?

    /// Where keyboard focus currently lives inside the popover.
    /// `nil` = unfocused (transient); the task/onAppear restores it
    /// to `.search` whenever the popover opens.
    private enum Focus: Hashable {
        case search
        case list
    }

    public var body: some View {
        VStack(spacing: 0) {
            searchField
            Divider()
            list
        }
        // System popover material — picks up the surrounding window
        // tint instead of rendering as a flat dark slab. Matches the
        // vibrancy of Mail / Notes / Spotlight.
        .background(.regularMaterial)
        .task {
            if viewModel.conversations.isEmpty && viewModel.state == .idle {
                await viewModel.load()
            }
            focus = .search
        }
    }

    // MARK: - Filter bar

    private var searchField: some View {
        HStack(spacing: 6) {
            NativeSearchField(
                text: $searchText,
                placeholder: "Filter by title",
                onArrowDown: {
                    guard let first = filtered.first else { return }
                    selection = first.name
                    focus = .list
                },
                onCancel: {
                    if !searchText.isEmpty {
                        searchText = ""
                    } else {
                        dismiss()
                    }
                }
            )
            .focused($focus, equals: .search)

            // Refresh affordance. SwiftUI's `.refreshable` modifier
            // has no native macOS UI surface — confirmed: it
            // provides the action to the environment but doesn't
            // wire any gesture, spinner, or shortcut. So we bridge
            // both ends of the affordance ourselves: a visible
            // chrome icon for mouse users plus ⌘R as the
            // conventional keyboard shortcut on the same button so
            // the two paths can never drift.
            IconButton(
                systemName: "arrow.clockwise",
                label: "Refresh",
                help: "Refresh (⌘R)"
            ) {
                Task { await viewModel.load() }
            }
            .keyboardShortcut("r", modifiers: .command)
            .disabled(viewModel.state == .loading)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
    }

    // MARK: - List

    private var filtered: [Pivox_Ai_V1_Conversation] {
        let q = searchText.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !q.isEmpty else { return viewModel.conversations }
        return viewModel.conversations.filter {
            $0.title.lowercased().contains(q)
        }
    }

    @ViewBuilder
    private var list: some View {
        switch viewModel.state {
        case .loading where viewModel.conversations.isEmpty:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .padding(16)
        case .error(let msg) where viewModel.conversations.isEmpty:
            Text(msg)
                .foregroundStyle(.red)
                .font(.footnote)
                .padding(16)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        default:
            rows
        }
    }

    private var rows: some View {
        List(selection: $selection) {
            ForEach(filtered, id: \.name) { conv in
                HistoryRow(
                    conversation: conv,
                    onOpen: { onSelect(conv) },
                    onRename: { newTitle in
                        Task { try? await viewModel.rename(name: conv.name, newTitle: newTitle) }
                    },
                    onDelete: {
                        // Optimistic delete — the row is removed
                        // from `conversations` synchronously inside
                        // `viewModel.delete`, so wrapping in
                        // `withAnimation` plays the row's transition
                        // immediately. The async network call
                        // continues and restores the row on failure.
                        Task {
                            withAnimation(.easeOut(duration: 0.22)) {
                                Task { try? await viewModel.delete(name: conv.name) }
                            }
                        }
                    }
                )
                .tag(conv.name)
                // Scale + fade out on removal — soft "poof" that
                // reads as deliberate without crossing into iOS
                // territory. Insertion stays subtle (opacity only)
                // so refresh doesn't feel jumpy.
                .transition(.asymmetric(
                    insertion: .opacity,
                    removal: .scale(scale: 0.85).combined(with: .opacity)
                ))
                // Infinite scroll: load older when the last row
                // enters the viewport. Pull-to-refresh handles the
                // opposite edge via .refreshable below.
                .onAppear {
                    if conv.name == filtered.last?.name {
                        Task { await viewModel.loadMore() }
                    }
                }
            }

            if viewModel.isLoadingMore {
                HStack {
                    Spacer()
                    ProgressView().controlSize(.small).padding(8)
                    Spacer()
                }
                .listRowSeparator(.hidden)
            }
        }
        .listStyle(.sidebar)
        .focused($focus, equals: .list)
        // ⌘Delete on a focused/selected row deletes it. macOS-native
        // alternative for the hover-reveal trash button — same
        // shortcut Mail uses. Selection drives which row dies.
        .onDeleteCommand {
            guard let name = selection,
                  let conv = filtered.first(where: { $0.name == name })
            else { return }
            Task {
                withAnimation(.easeOut(duration: 0.22)) {
                    Task { try? await viewModel.delete(name: conv.name) }
                }
            }
        }
        // Return on a focused/selected row opens that conversation.
        .onKeyPress(.return) {
            openSelection()
            return .handled
        }
        // ↑ from the first row hops focus back to the search field
        // so the user can keep editing the filter without reaching
        // for the mouse. For any non-first row, `.ignored` lets List
        // handle normal row-wise upward movement.
        .onKeyPress(.upArrow) {
            guard let name = selection,
                  let idx = filtered.firstIndex(where: { $0.name == name }),
                  idx == 0
            else { return .ignored }
            selection = nil
            focus = .search
            return .handled
        }
        // Esc from the list clears the filter if any, otherwise
        // dismisses the popover — same semantics as the search field.
        .onExitCommand {
            if !searchText.isEmpty {
                searchText = ""
                focus = .search
            } else {
                dismiss()
            }
        }
    }

    private func openSelection() {
        guard let name = selection,
              let conv = filtered.first(where: { $0.name == name })
        else { return }
        onSelect(conv)
    }
}

// MARK: - Row

/// Every row is sized to `rowHeight` regardless of mode so mode transitions
/// don't reflow the list.
private let rowHeight: CGFloat = 44

private struct HistoryRow: View {
    let conversation: Pivox_Ai_V1_Conversation
    let onOpen: () -> Void
    let onRename: (String) -> Void
    let onDelete: () -> Void

    private enum RowMode: Equatable {
        case display
        case renaming(draft: String)
    }

    @State private var mode: RowMode = .display
    @State private var isHovered: Bool = false
    @FocusState private var renameFocused: Bool

    var body: some View {
        HStack(spacing: 8) {
            switch mode {
            case .display:
                displayContent
            case .renaming(let draft):
                renamingContent(draft: draft)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 4)
        .frame(height: rowHeight)
        .contentShape(Rectangle())
        // Hover state drives the show/hide of the secondary
        // action icons. Apple lists do this in Mail / Notes /
        // Music — affordances live in the chrome at rest, surface
        // on hover. The animation is short so the icons appear
        // alongside the cursor rather than chasing it.
        .onHover { hovered in
            withAnimation(.easeInOut(duration: 0.12)) {
                isHovered = hovered
            }
        }
        // Click-to-open still works alongside List's own row
        // selection — the tap gesture and List's selection handler
        // run in parallel, so a single click both selects AND opens.
        .onTapGesture {
            if case .display = mode { onOpen() }
        }
    }

    private var displayTitle: String {
        conversation.title.isEmpty ? "Untitled" : conversation.title
    }

    private var displayContent: some View {
        HStack(spacing: 4) {
            VStack(alignment: .leading, spacing: 2) {
                Text(displayTitle)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Text(relativeAge(of: conversation.createTime.date))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            .accessibilityLabel("\(displayTitle), \(relativeAge(of: conversation.createTime.date))")
            .accessibilityHint("Double-tap to open")

            // Hover-reveal action icons. Same component as before
            // (consistent shape, sizing, focus ring) — only the
            // visibility is gated. Both icons get the secondary
            // foreground (no destructive red): macOS lists put
            // destructive role into menu/confirmation buttons, not
            // resting icons — Mail's trash button sits in the
            // toolbar at default tint, the red only shows up in
            // the alert that fires after.
            HStack(spacing: 4) {
                IconButton(systemName: "pencil", label: "Rename \(displayTitle)", help: "Rename") {
                    mode = .renaming(draft: conversation.title)
                    // Dispatch after the mode transition so the TextField is in the
                    // responder chain before we ask for focus. Setting focus in the
                    // TextField's own .onAppear is unreliable on this code path.
                    DispatchQueue.main.async { renameFocused = true }
                }
                IconButton(systemName: "trash", label: "Delete \(displayTitle)", help: "Delete") {
                    onDelete()
                }
            }
            .opacity(isHovered ? 1 : 0)
            // Don't shift layout when the icons hide — the title
            // column owns the leading region via `maxWidth: .infinity`,
            // so the trailing icons just fade in/out in the same
            // reserved trailing strip.
        }
    }

    private func renamingContent(draft: String) -> some View {
        HStack(spacing: 6) {
            TextField(
                "Title",
                text: Binding(
                    get: { draft },
                    set: { mode = .renaming(draft: $0) }
                )
            )
            .textFieldStyle(.roundedBorder)
            .focused($renameFocused)
            .onSubmit { commitRename() }
            .onExitCommand { mode = .display }
            .accessibilityLabel("New title")

            IconButton(systemName: "checkmark.circle.fill", label: "Save", help: "Save") {
                commitRename()
            }
            IconButton(systemName: "xmark.circle", label: "Cancel", help: "Cancel") {
                mode = .display
            }
        }
    }

    private func commitRename() {
        guard case .renaming(let draft) = mode else { return }
        let trimmed = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty, trimmed != conversation.title {
            onRename(trimmed)
        }
        mode = .display
    }
}

// MARK: - NativeSearchField

/// SwiftUI wrapper around `NSSearchField` so the popover's filter
/// bar reads as a real macOS search widget. NSSearchField provides
/// the system magnifying-glass icon, the built-in clear-x button on
/// non-empty content, the focus ring, and accessibility role
/// "search field" — all of which the previous TextField + manual
/// HStack reimplemented inconsistently.
private struct NativeSearchField: NSViewRepresentable {
    @Binding var text: String
    var placeholder: String
    var onArrowDown: () -> Void = {}
    var onCancel: () -> Void = {}

    func makeNSView(context: Context) -> NSSearchField {
        let field = NSSearchField()
        field.placeholderString = placeholder
        field.delegate = context.coordinator
        field.bezelStyle = .roundedBezel
        field.focusRingType = .default
        return field
    }

    func updateNSView(_ nsView: NSSearchField, context: Context) {
        if nsView.stringValue != text {
            nsView.stringValue = text
        }
        nsView.placeholderString = placeholder
        // Keep delegate's parent reference current — SwiftUI may
        // re-create this struct on every state change while the
        // NSView lives across updates.
        context.coordinator.parent = self
    }

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    final class Coordinator: NSObject, NSSearchFieldDelegate {
        var parent: NativeSearchField

        init(_ parent: NativeSearchField) { self.parent = parent }

        override func controlTextDidChange(_ notification: Notification) {
            guard let field = notification.object as? NSSearchField else { return }
            // Setting the binding can trigger a re-entrant SwiftUI
            // update; that's why updateNSView guards with the
            // `stringValue != text` check.
            parent.text = field.stringValue
        }

        // Forward a small set of commands to the SwiftUI host so the
        // popover-level keyboard model (↓ moves focus to the list,
        // Esc clears or dismisses) keeps working. Anything else
        // returns `false` so NSSearchField handles it natively
        // (typing, ⌘A, paste, the built-in clear button, etc.).
        func control(
            _ control: NSControl,
            textView: NSTextView,
            doCommandBy commandSelector: Selector
        ) -> Bool {
            switch commandSelector {
            case #selector(NSResponder.moveDown(_:)):
                parent.onArrowDown()
                return true
            case #selector(NSResponder.cancelOperation(_:)):
                parent.onCancel()
                return true
            default:
                return false
            }
        }
    }
}

// MARK: - Helpers

private extension SwiftProtobuf.Google_Protobuf_Timestamp {
    var date: Date {
        Date(timeIntervalSince1970: TimeInterval(seconds) + TimeInterval(nanos) / 1_000_000_000)
    }
}

private let relativeFormatter: RelativeDateTimeFormatter = {
    let f = RelativeDateTimeFormatter()
    f.unitsStyle = .abbreviated
    return f
}()

private func relativeAge(of date: Date) -> String {
    relativeFormatter.localizedString(for: date, relativeTo: Date())
}
