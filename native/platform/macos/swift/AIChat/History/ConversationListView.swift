import PivoxModels
import SwiftUI

public struct ConversationListView: View {
    @ObservedObject var viewModel: ConversationListViewModel
    let onSelect: (Pivox_Ai_V1_Conversation) -> Void

    public var body: some View {
        Group {
            switch viewModel.state {
            case .idle, .loading:
                ProgressView("Loading conversations...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)

            case .loaded where viewModel.conversations.isEmpty:
                emptyState

            case .loaded:
                conversationList

            case .error(let message):
                errorState(message)
            }
        }
        .task {
            if viewModel.state == .idle {
                await viewModel.load()
            }
        }
    }

    private var conversationList: some View {
        List {
            ForEach(viewModel.conversations, id: \.name) { conv in
                ConversationRow(conversation: conv)
                    .contentShape(Rectangle())
                    .onTapGesture { onSelect(conv) }
                    .contextMenu {
                        Button("Archive") {
                            Task { try? await viewModel.archive(name: conv.name) }
                        }
                        Button("Delete", role: .destructive) {
                            Task { try? await viewModel.delete(name: conv.name) }
                        }
                    }
            }
        }
        .listStyle(.sidebar)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    Task {
                        if let conv = try? await viewModel.create() {
                            onSelect(conv)
                        }
                    }
                } label: {
                    Image(systemName: "plus")
                        .pivoxIconToolbar()
                }
                .help("New conversation")
            }
        }
    }

    private var emptyState: some View {
        VStack(spacing: 12) {
            Image(systemName: "bubble.left.and.bubble.right")
                .font(.largeTitle)
                .foregroundStyle(.tertiary)
            Text("No conversations yet")
                .font(.headline)
            Button("Start your first conversation") {
                Task {
                    if let conv = try? await viewModel.create() {
                        onSelect(conv)
                    }
                }
            }
            .buttonStyle(.borderedProminent)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func errorState(_ message: String) -> some View {
        VStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle")
                .font(.largeTitle)
                .foregroundStyle(.tertiary)
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Button("Retry") {
                Task { await viewModel.load() }
            }
            .buttonStyle(.bordered)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

struct ConversationRow: View {
    let conversation: Pivox_Ai_V1_Conversation

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(displayTitle)
                    .font(.body)
                    .lineLimit(1)
                Spacer()
                if conversation.pinned {
                    Image(systemName: "pin.fill")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            if conversation.hasLastMessageTime {
                Text(conversation.lastMessageTime.date, style: .relative)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
        .opacity(conversation.archived ? 0.5 : 1.0)
    }

    private var displayTitle: String {
        conversation.title.isEmpty ? "Untitled conversation" : conversation.title
    }
}
