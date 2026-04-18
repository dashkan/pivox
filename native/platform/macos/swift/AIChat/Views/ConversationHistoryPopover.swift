import PivoxModels
import SwiftProtobuf
import SwiftUI

/// Dropdown popover listing recent conversations. Opened from the history
/// button in the AI chat header. Supports:
///
///   * Client-side filter by title (case-insensitive substring)
///   * Infinite scroll via page token
///   * Inline rename (click pencil → in-place TextField, return/esc commits/cancels)
///   * Inline delete confirm (click trash → row morphs to "Delete? [Yes] [Cancel]")
///   * Keyboard navigation + VoiceOver labels
public struct ConversationHistoryPopover: View {
    @ObservedObject var viewModel: ConversationListViewModel
    let onSelect: (Pivox_Ai_V1_Conversation) -> Void

    @State private var searchText: String = ""
    @FocusState private var searchFocused: Bool

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
            searchFocused = true
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
                .focused($searchFocused)
                .accessibilityLabel("Filter conversations by title")
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
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
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
                    Divider()
                }

                if viewModel.isLoadingMore {
                    HStack {
                        Spacer()
                        ProgressView()
                            .controlSize(.small)
                            .padding(8)
                        Spacer()
                    }
                }
            }
            .background(
                // Infinite-scroll sentinel — the invisible footer triggers a
                // next-page fetch when it enters the viewport.
                GeometryReader { _ in
                    Color.clear.onAppear {
                        Task { await viewModel.loadMore() }
                    }
                }
            )
        }
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
