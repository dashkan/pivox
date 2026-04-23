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
///  * `Esc`: clears the filter if non-empty, otherwise dismisses the
///    popover.
///
/// ## Other features
///  * Client-side filter by title (case-insensitive substring).
///  * Infinite scroll via page token (last-row-visible triggers loadMore).
///  * Inline rename (pencil → in-place TextField, Return/Esc commits/cancels).
///  * Inline delete confirm (trash → row morphs to "Delete? [Yes] [Cancel]").
///
/// ## Why `List(selection:)`
/// The row layout is a pure SwiftUI `ScrollView + LazyVStack` no more —
/// `List(selection:)` bridges to AppKit's responder chain and gives us
/// arrow-key nav, selection highlight, and VoiceOver row semantics for
/// free. Reinventing those with `.onKeyPress` on a VStack was considered
/// and rejected — too much HIG surface to re-create by hand.
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
            Image(systemName: "magnifyingglass")
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            TextField("Filter by title", text: $searchText)
                .textFieldStyle(.plain)
                .focused($focus, equals: .search)
                .accessibilityLabel("Filter conversations by title")
                // ↓ moves focus into the list on the first row. If
                // filtered is empty, let the event pass through so
                // the system beep signals nothing to navigate to.
                .onKeyPress(.downArrow) {
                    guard let first = filtered.first else { return .ignored }
                    selection = first.name
                    focus = .list
                    return .handled
                }
                // Esc: clear filter first, then dismiss on a second
                // Esc when the filter is already empty.
                .onExitCommand {
                    if !searchText.isEmpty {
                        searchText = ""
                    } else {
                        dismiss()
                    }
                }
            if !searchText.isEmpty {
                IconButton(systemName: "xmark.circle.fill", label: "Clear filter") {
                    searchText = ""
                }
            }
            IconButton(
                systemName: "arrow.clockwise",
                label: "Refresh",
                help: "Refresh"
            ) {
                Task { await viewModel.load() }
            }
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
                .frame(maxWidth: .infinity, alignment: .leading)
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
                        Task { try? await viewModel.delete(name: conv.name) }
                    }
                )
                .tag(conv.name)
                // Infinite scroll: kick off the next page when the
                // last row enters the viewport. Replaces the old
                // GeometryReader-in-background trick which doesn't
                // work cleanly inside a List.
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
        .listStyle(.plain)
        .focused($focus, equals: .list)
        // Return on a focused/selected row opens that conversation.
        // Enter on macOS is treated identically.
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
/// don't reflow the list. All three modes (display, renaming, confirming
/// delete) fit within a single line of content aligned with the title row.
private let rowHeight: CGFloat = 44

private struct HistoryRow: View {
    let conversation: Pivox_Ai_V1_Conversation
    let onOpen: () -> Void
    let onRename: (String) -> Void
    let onDelete: () -> Void

    private enum RowMode: Equatable {
        case display
        case renaming(draft: String)
        case confirmingDelete
    }

    @State private var mode: RowMode = .display
    @FocusState private var renameFocused: Bool

    var body: some View {
        HStack(spacing: 8) {
            switch mode {
            case .display:
                displayContent
            case .renaming(let draft):
                renamingContent(draft: draft)
            case .confirmingDelete:
                deleteConfirmContent
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 4)
        .frame(height: rowHeight)
        .contentShape(Rectangle())
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

            IconButton(systemName: "pencil", label: "Rename \(displayTitle)", help: "Rename") {
                mode = .renaming(draft: conversation.title)
                // Dispatch after the mode transition so the TextField is in the
                // responder chain before we ask for focus. Setting focus in the
                // TextField's own .onAppear is unreliable on this code path.
                DispatchQueue.main.async { renameFocused = true }
            }
            IconButton(systemName: "trash", label: "Delete \(displayTitle)", help: "Delete", role: .destructive) {
                mode = .confirmingDelete
            }
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

    private var deleteConfirmContent: some View {
        HStack(spacing: 6) {
            Text("Delete \"\(displayTitle)\"?")
                .lineLimit(1)
                .truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)

            IconButton(
                systemName: "trash.fill",
                label: "Confirm delete \(displayTitle)",
                help: "Delete",
                role: .destructive
            ) {
                onDelete()
            }
            IconButton(systemName: "xmark.circle", label: "Cancel", help: "Cancel") {
                mode = .display
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
