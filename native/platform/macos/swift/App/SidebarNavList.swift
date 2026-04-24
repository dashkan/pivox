import AppKit
import SwiftUI

/// AppKit-backed source list sidebar. Matches the macOS system
/// sidebar look (Music, Mail, Finder): translucent material
/// background (inherited from NavigationSplitView's sidebar
/// column), subtle gray selection pill, accent-colored text + icon
/// on the selected row when the window is active, neutral gray
/// when inactive.
///
/// Row content is hosted per-row via `NSHostingView` so row visuals
/// stay in SwiftUI (theme tokens, `Image(systemName:)`, etc.) while
/// `NSTableView` owns keyboard nav, selection, first-responder
/// behavior, and the inactive-window dim transition.
///
/// ## Why AppKit here
/// SwiftUI's `List(.sidebar)` is a thin wrapper with no access to:
///   - Row background color (at rest OR hover).
///   - Selection highlight customization — you get the system pill,
///     take it or leave it.
///   - Icon size beyond `imageScale(.large)` — capped ~15pt, while
///     system sidebars sit at 17–18pt.
struct SidebarNavList: NSViewRepresentable {
    @Binding var selectedItem: SidebarItem?
    @Environment(\.pivoxTheme) private var theme

    /// Bottom inset reserved for the ProfileBar overlay so list
    /// rows stop scrolling before they reach the bar.
    static let profileBarOverlayHeight: CGFloat = 52

    func makeCoordinator() -> Coordinator {
        Coordinator(selection: $selectedItem)
    }

    func makeNSView(context: Context) -> NSScrollView {
        let tableView = NSTableView()
        tableView.headerView = nil
        tableView.backgroundColor = .clear
        tableView.style = .sourceList
        tableView.selectionHighlightStyle = .regular
        tableView.allowsEmptySelection = false
        tableView.allowsMultipleSelection = false
        tableView.rowSizeStyle = .custom
        tableView.intercellSpacing = NSSize(width: 0, height: 2)
        tableView.gridStyleMask = []
        tableView.usesAutomaticRowHeights = false
        tableView.floatsGroupRows = false

        let column = NSTableColumn(identifier: .sectionColumn)
        column.resizingMask = .autoresizingMask
        tableView.addTableColumn(column)

        tableView.dataSource = context.coordinator
        tableView.delegate = context.coordinator
        context.coordinator.tableView = tableView

        let scrollView = NSScrollView()
        scrollView.hasVerticalScroller = true
        scrollView.drawsBackground = false
        scrollView.borderType = .noBorder
        scrollView.documentView = tableView
        scrollView.automaticallyAdjustsContentInsets = false
        scrollView.contentInsets = NSEdgeInsets(
            top: 0,
            left: 0,
            bottom: Self.profileBarOverlayHeight,
            right: 0)

        context.coordinator.selectionFillColor = NSColor(theme.sidebarSelectionFill)

        tableView.reloadData()
        DispatchQueue.main.async {
            context.coordinator.applyExternalSelection(selectedItem)
        }
        return scrollView
    }

    func updateNSView(_ nsView: NSScrollView, context: Context) {
        context.coordinator.selectionFillColor = NSColor(theme.sidebarSelectionFill)
        context.coordinator.applyExternalSelection(selectedItem)
    }

    @MainActor
    final class Coordinator: NSObject, NSTableViewDataSource, NSTableViewDelegate {
        private let selection: Binding<SidebarItem?>
        weak var tableView: NSTableView?

        /// Flat row model for the sidebar. Interleaves real sections,
        /// DEBUG-only group headers (non-selectable, rendered in
        /// source-list header style), and DEBUG-only dummy rows that
        /// exist to stress-test scrolling.
        enum Entry {
            case section(AppSection)
            case groupHeader(String)
            case dummy(name: String, icon: String)
        }

        /// Resolves the flat row sequence. In release builds this is
        /// just `AppSection.allCases`. In DEBUG we add two extra
        /// groups of filler items so we can preview section
        /// grouping + scroll overflow.
        private let entries: [Entry] = {
            var list: [Entry] = AppSection.allCases.map { .section($0) }
            #if DEBUG
            list.append(.groupHeader("Library"))
            let libraryIcons = [
                "photo.on.rectangle.angled", "person.circle", "music.mic",
                "square.stack", "music.note", "film",
            ]
            for (i, icon) in libraryIcons.enumerated() {
                list.append(.dummy(name: "Library Item \(i + 1)", icon: icon))
            }
            list.append(.groupHeader("Playlists"))
            let playlistNames = [
                "All Playlists", "Favorite Songs", "90's Music",
                "Music Videos", "My Top Rated", "Recently Added",
                "Recently Played", "Top 25 Most Played",
                "Chill Mix", "Focus Mix", "Road Trip", "Workout",
                "Study Session", "Late Night",
            ]
            for name in playlistNames {
                list.append(.dummy(name: name, icon: "music.note.list"))
            }
            #endif
            return list
        }()

        var selectionFillColor: NSColor = .secondarySystemFill {
            didSet {
                guard let tableView else { return }
                tableView.enumerateAvailableRowViews { rowView, _ in
                    guard let row = rowView as? SidebarRowView else { return }
                    row.selectionFillColor = selectionFillColor
                    if row.isSelected { row.needsDisplay = true }
                }
            }
        }

        private var isProgrammaticSelectionInProgress = false

        init(selection: Binding<SidebarItem?>) {
            self.selection = selection
        }

        // MARK: - External selection sync

        func applyExternalSelection(_ item: SidebarItem?) {
            guard let tableView else { return }
            let targetRow: Int?
            if case .section(let section) = item,
               let idx = entries.firstIndex(where: {
                   if case .section(let s) = $0, s == section { return true }
                   return false
               }) {
                targetRow = idx
            } else {
                targetRow = nil
            }
            if let targetRow, tableView.selectedRow != targetRow {
                isProgrammaticSelectionInProgress = true
                tableView.selectRowIndexes(IndexSet(integer: targetRow),
                                           byExtendingSelection: false)
                isProgrammaticSelectionInProgress = false
            } else if targetRow == nil, tableView.selectedRow >= 0 {
                isProgrammaticSelectionInProgress = true
                tableView.deselectAll(nil)
                isProgrammaticSelectionInProgress = false
            }
        }

        // MARK: - NSTableViewDataSource

        func numberOfRows(in tableView: NSTableView) -> Int {
            entries.count
        }

        // MARK: - NSTableViewDelegate

        func tableView(_ tableView: NSTableView, isGroupRow row: Int) -> Bool {
            if case .groupHeader = entries[row] { return true }
            return false
        }

        func selectionShouldChange(in tableView: NSTableView) -> Bool {
            true
        }

        func tableView(_ tableView: NSTableView, shouldSelectRow row: Int) -> Bool {
            // Group headers aren't selectable — matches Music/Mail
            // behavior where "Library" / "Playlists" labels are just
            // headers, never active rows.
            switch entries[row] {
            case .groupHeader: return false
            case .section, .dummy: return true
            }
        }

        func tableView(_ tableView: NSTableView, rowViewForRow row: Int) -> NSTableRowView? {
            let view = SidebarRowView()
            view.selectionFillColor = selectionFillColor
            return view
        }

        func tableView(_ tableView: NSTableView, viewFor column: NSTableColumn?, row: Int) -> NSView? {
            switch entries[row] {
            case .section(let section):
                return SidebarCellView(
                    icon: section.icon,
                    title: section.rawValue)
            case .dummy(let name, let icon):
                return SidebarCellView(icon: icon, title: name)
            case .groupHeader(let title):
                return SidebarGroupHeaderView(title: title)
            }
        }

        func tableView(_ tableView: NSTableView, heightOfRow row: Int) -> CGFloat {
            switch entries[row] {
            case .groupHeader:
                return 28
            default:
                return 32
            }
        }

        func tableViewSelectionDidChange(_ notification: Notification) {
            guard !isProgrammaticSelectionInProgress, let tableView else { return }
            let row = tableView.selectedRow
            guard row >= 0, row < entries.count else {
                selection.wrappedValue = nil
                return
            }
            switch entries[row] {
            case .section(let section):
                selection.wrappedValue = .section(section)
            case .dummy, .groupHeader:
                // DEBUG filler / non-selectable: don't propagate to
                // the app's selection binding.
                break
            }
        }
    }
}

// MARK: - Row view (selection chrome)

private final class SidebarRowView: NSTableRowView {
    var selectionFillColor: NSColor = .secondarySystemFill

    override func drawSelection(in dirtyRect: NSRect) {
        guard isSelected else { return }
        selectionFillColor.setFill()
        let rect = bounds.insetBy(dx: 6, dy: 1)
        let path = NSBezierPath(roundedRect: rect, xRadius: 6, yRadius: 6)
        path.fill()
    }

    override var isSelected: Bool {
        didSet { refreshHostedContent() }
    }

    override var isEmphasized: Bool {
        didSet { refreshHostedContent() }
    }

    private func refreshHostedContent() {
        for sub in subviews {
            if let cell = sub as? SidebarCellView {
                cell.applySelection(isSelected: isSelected,
                                    isEmphasized: isEmphasized)
            }
        }
    }
}

// MARK: - Cell view (hosts SwiftUI row)

private final class SidebarCellView: NSTableCellView {
    private let icon: String
    private let title: String
    private let host: NSHostingView<SidebarRow>

    init(icon: String, title: String) {
        self.icon = icon
        self.title = title
        self.host = NSHostingView(rootView: SidebarRow(
            icon: icon,
            title: title,
            isSelected: false,
            isEmphasized: false))
        super.init(frame: .zero)
        host.translatesAutoresizingMaskIntoConstraints = false
        addSubview(host)
        NSLayoutConstraint.activate([
            host.leadingAnchor.constraint(equalTo: leadingAnchor),
            host.trailingAnchor.constraint(equalTo: trailingAnchor),
            host.topAnchor.constraint(equalTo: topAnchor),
            host.bottomAnchor.constraint(equalTo: bottomAnchor),
        ])
    }

    required init?(coder: NSCoder) { fatalError("not implemented") }

    func applySelection(isSelected: Bool, isEmphasized: Bool) {
        host.rootView = SidebarRow(
            icon: icon,
            title: title,
            isSelected: isSelected,
            isEmphasized: isEmphasized)
    }
}

/// Group-header cell — small all-caps-ish label in secondary color.
/// Non-selectable (see `shouldSelectRow`). Rendered in a native-
/// looking sidebar section style rather than as a full row with
/// accent treatment.
private final class SidebarGroupHeaderView: NSTableCellView {
    init(title: String) {
        super.init(frame: .zero)
        let label = NSTextField(labelWithString: title)
        label.font = NSFont.systemFont(ofSize: 11, weight: .semibold)
        label.textColor = .secondaryLabelColor
        // Same truncation policy as row content — ellipsis, never
        // wrap — so narrow sidebar widths collapse headers cleanly
        // instead of pushing row heights around.
        label.usesSingleLineMode = true
        label.lineBreakMode = .byTruncatingTail
        label.maximumNumberOfLines = 1
        label.translatesAutoresizingMaskIntoConstraints = false
        addSubview(label)
        NSLayoutConstraint.activate([
            label.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 18),
            label.trailingAnchor.constraint(lessThanOrEqualTo: trailingAnchor, constant: -8),
            label.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -4),
        ])
    }

    required init?(coder: NSCoder) { fatalError("not implemented") }
}

// MARK: - Row content (SwiftUI)

private struct SidebarRow: View {
    let icon: String
    let title: String
    let isSelected: Bool
    let isEmphasized: Bool

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: icon)
                .font(.system(size: 17, weight: .regular))
                .frame(width: 22)
            Text(title)
                .font(.body)
                // Single line — when the sidebar is dragged narrow,
                // truncate with an ellipsis instead of wrapping to a
                // second line. Matches Music/Mail sidebar behavior.
                .lineLimit(1)
                .truncationMode(.tail)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 10)
        .foregroundStyle(foreground)
    }

    private var foreground: Color {
        guard isSelected else { return .primary }
        return isEmphasized ? Color.accentColor : Color.secondary
    }
}

private extension NSUserInterfaceItemIdentifier {
    static let sectionColumn = NSUserInterfaceItemIdentifier("section")
}
