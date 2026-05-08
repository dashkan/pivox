// Copyright 2025 Pivox
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import PivoxModels
import SwiftProtobuf
import SwiftUI

/// SwiftUI renderer for a `CollectionWidget`. Dispatches on the
/// current `DisplayMode` (TABLE or CARD) and presents the mode
/// toggle when `supportedModes` has more than one entry.
///
/// The rendered surface is read-only in v1: row clicks do not yet
/// dispatch to action handlers (Phase 6c+ wires the per-row action
/// menu). The empty-state surface IS interactive — its
/// `primaryAction` flows through the supplied `onAction` closure
/// so a future "Upload" / "Invite" CTA wires cleanly when Phase 6c
/// adds the action dispatcher.
///
/// Filter / order / pagination UI is deliberately absent for v1
/// — the server's `ListDashboards` rejects non-empty filter /
/// order_by today (per Phase 4b's reject), so surfacing the UI
/// would mislead. When AIP-160 wiring lands (issue #86) the
/// CollectionWidgetView gains a filter / sort header above the
/// table.
struct CollectionWidgetView: View {
    let widget: Pivox_Api_V1_CollectionWidget
    let rows: [Google_Protobuf_Struct]
    let onAction: (Pivox_Api_V1_RowAction) -> Void

    @State private var mode: Pivox_Api_V1_CollectionWidget.DisplayMode

    init(
        widget: Pivox_Api_V1_CollectionWidget,
        rows: [Google_Protobuf_Struct],
        onAction: @escaping (Pivox_Api_V1_RowAction) -> Void = { _ in }
    ) {
        self.widget = widget
        self.rows = rows
        self.onAction = onAction
        // Initial mode: explicit display_mode wins; otherwise first
        // entry of supported_modes; ultimately TABLE per the proto's
        // unspecified-default contract.
        if widget.displayMode != .unspecified {
            _mode = State(initialValue: widget.displayMode)
        } else if let first = widget.supportedModes.first(where: { $0 != .unspecified }) {
            _mode = State(initialValue: first)
        } else {
            _mode = State(initialValue: .table)
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            if shouldShowModeToggle {
                modeToggle
                    .padding(.horizontal)
                    .padding(.vertical, 8)
            }
            if rows.isEmpty, widget.hasEmptyState {
                EmptyStateView(state: widget.emptyState, onAction: onAction)
            } else {
                content
            }
        }
    }

    @ViewBuilder
    private var content: some View {
        switch mode {
        case .table:
            tableMode
        case .card:
            cardMode
        case .unspecified, .UNRECOGNIZED:
            tableMode
        }
    }

    // MARK: - TABLE mode

    @ViewBuilder
    private var tableMode: some View {
        // List-based table because SwiftUI's `Table` requires static
        // column declarations (its `TableColumnBuilder` does not
        // accept `ForEach` over a dynamic column list as of iOS 26 /
        // macOS 26). For v1 the `widget.columns` set is small and
        // List + a header row covers the same surface cleanly. When
        // the renderer needs sort indicators or column resizing,
        // switch to `Table` with a hardcoded column count.
        VStack(spacing: 0) {
            tableHeader
            Divider()
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(indexedRows) { row in
                        tableRow(row.value)
                        Divider()
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var tableHeader: some View {
        HStack(spacing: 12) {
            // Empty leader column for the leading icon.
            Color.clear.frame(width: 36)
            ForEach(visibleColumns, id: \.field) { column in
                Text(columnHeader(column))
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 6)
    }

    @ViewBuilder
    private func tableRow(_ row: Google_Protobuf_Struct) -> some View {
        HStack(spacing: 12) {
            RowIconView(row: row, config: widget.iconConfig, placement: .tableRow)
                .frame(width: 36, alignment: .center)
            ForEach(visibleColumns, id: \.field) { column in
                Text(stringValue(row, field: column.field))
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    // MARK: - CARD mode

    @ViewBuilder
    private var cardMode: some View {
        ScrollView {
            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 180, maximum: 240), spacing: 16)],
                spacing: 16
            ) {
                ForEach(indexedRows) { row in
                    cardForRow(row.value)
                }
            }
            .padding()
        }
    }

    @ViewBuilder
    private func cardForRow(_ row: Google_Protobuf_Struct) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            RowIconView(row: row, config: widget.iconConfig, placement: .cardThumbnail)
                .frame(maxWidth: .infinity)
            Text(stringValue(row, field: primaryDisplayField))
                .font(.callout.weight(.medium))
                .lineLimit(2)
            if let secondary = secondaryDisplayField {
                Text(stringValue(row, field: secondary))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
        .padding(12)
        .background(Color.secondary.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 10))
    }

    // MARK: - Mode toggle

    private var shouldShowModeToggle: Bool {
        // Show the toggle iff the customer has more than one mode
        // they can switch to.
        let nonEmptyModes = widget.supportedModes.filter { $0 != .unspecified }
        return nonEmptyModes.count >= 2
    }

    @ViewBuilder
    private var modeToggle: some View {
        Picker("Display mode", selection: $mode) {
            ForEach(widget.supportedModes.filter { $0 != .unspecified }, id: \.rawValue) { m in
                Image(systemName: iconNameForMode(m))
                    .tag(m)
                    .accessibilityLabel(labelForMode(m))
            }
        }
        .pickerStyle(.segmented)
        .frame(maxWidth: 120)
        .frame(maxWidth: .infinity, alignment: .trailing)
    }

    private func iconNameForMode(_ m: Pivox_Api_V1_CollectionWidget.DisplayMode) -> String {
        switch m {
        case .table: return "list.bullet"
        case .card: return "square.grid.2x2"
        case .unspecified, .UNRECOGNIZED: return "questionmark"
        }
    }

    private func labelForMode(_ m: Pivox_Api_V1_CollectionWidget.DisplayMode) -> String {
        switch m {
        case .table: return "Table"
        case .card: return "Card grid"
        case .unspecified, .UNRECOGNIZED: return "Unknown"
        }
    }

    // MARK: - Column helpers

    private var visibleColumns: [Pivox_Api_V1_Column] {
        // proto bool default is false, but our templates explicitly
        // set `visible = true` on every shipped column. Filter on
        // the bool directly — hand-crafted widgets that omit
        // `visible` get nothing in the table, which surfaces the
        // template bug visibly rather than rendering hidden columns.
        widget.columns.filter { $0.visible }
    }

    private func columnHeader(_ c: Pivox_Api_V1_Column) -> String {
        c.displayName.isEmpty ? c.field : c.displayName
    }

    private var primaryDisplayField: String {
        // CARD mode picks the first visible column as the title.
        // For the Asset library this resolves to `display_name`.
        visibleColumns.first?.field ?? "display_name"
    }

    private var secondaryDisplayField: String? {
        // CARD mode optionally surfaces a second column under the
        // title. For the Asset library this is `media_type`.
        visibleColumns.dropFirst().first?.field
    }

    // MARK: - Row indexing

    /// `IndexedRow` lets `Table` and `LazyVGrid` use a stable
    /// `Identifiable` key without forcing the row Struct itself to
    /// be Identifiable (it isn't — the synthesized `name` field is
    /// what we'd want to key on but Table's `id` parameter wants a
    /// Hashable property of the value type itself).
    private struct IndexedRow: Identifiable {
        let id: Int
        let value: Google_Protobuf_Struct
    }

    private var indexedRows: [IndexedRow] {
        rows.enumerated().map { IndexedRow(id: $0.offset, value: $0.element) }
    }

    // MARK: - Value extraction

    /// Convert a row field into a display string. Numbers render
    /// with reasonable defaults (size bytes humanized; everything
    /// else as integer or decimal). Missing fields render as empty.
    private func stringValue(_ row: Google_Protobuf_Struct, field: String) -> String {
        guard let kind = row.fields[field]?.kind else { return "" }
        switch kind {
        case .nullValue:
            return ""
        case .numberValue(let n):
            if field == "size_bytes" {
                return Self.byteFormatter.string(fromByteCount: Int64(n))
            }
            return n.truncatingRemainder(dividingBy: 1) == 0
                ? String(Int64(n))
                : String(n)
        case .stringValue(let s):
            return s
        case .boolValue(let b):
            return b ? "true" : "false"
        case .structValue, .listValue:
            return ""
        }
    }

    private static let byteFormatter: ByteCountFormatter = {
        let f = ByteCountFormatter()
        f.allowedUnits = [.useKB, .useMB, .useGB, .useTB]
        f.countStyle = .file
        return f
    }()
}

#Preview("CARD mode — Asset library") {
    var widget = sampleAssetWidget()
    widget.displayMode = .card
    return CollectionWidgetView(widget: widget, rows: sampleAssetRows())
        .frame(width: 720, height: 480)
}

#Preview("TABLE mode — Asset library") {
    var widget = sampleAssetWidget()
    widget.displayMode = .table
    return CollectionWidgetView(widget: widget, rows: sampleAssetRows())
        .frame(width: 720, height: 480)
}

#Preview("Empty state") {
    let widget = sampleAssetWidget()
    return CollectionWidgetView(widget: widget, rows: []) { action in
        print("preview action: \(action.key)")
    }
    .frame(width: 720, height: 480)
}

// MARK: - Preview fixtures

private func sampleAssetWidget() -> Pivox_Api_V1_CollectionWidget {
    var widget = Pivox_Api_V1_CollectionWidget()
    widget.displayMode = .card
    widget.supportedModes = [.card, .table]

    var icon = Pivox_Api_V1_IconConfig()
    icon.sourceField = "thumbnail_url"
    icon.iconField = "icon"
    icon.fallbackIcon = .document
    widget.iconConfig = icon

    for (field, name) in [
        ("display_name", "Name"),
        ("media_type", "Type"),
        ("size_bytes", "Size"),
        ("create_time", "Added"),
    ] {
        var col = Pivox_Api_V1_Column()
        col.field = field
        col.displayName = name
        col.visible = true
        widget.columns.append(col)
    }

    var empty = Pivox_Api_V1_EmptyState()
    empty.title = "No assets yet"
    empty.subtitle = "Upload or import files to see them here."
    empty.icon = .photo
    var primary = Pivox_Api_V1_RowAction()
    primary.key = "upload_assets"
    primary.label = "Upload"
    primary.icon = .upload
    empty.primaryAction = primary
    widget.emptyState = empty

    return widget
}

private func sampleAssetRows() -> [Google_Protobuf_Struct] {
    let raw: [(String, Pivox_Api_V1_Icon, Int64)] = [
        ("Logo Final.png", .photo, 124_532),
        ("Hero Reel.mp4", .video, 12_500_000),
        ("Soundbed.wav", .audio, 4_120_500),
        ("Brief.pdf", .document, 92_000),
    ]
    return raw.map { (name, icon, size) in
        var s = Google_Protobuf_Struct()
        s.fields["display_name"] = stringValue(name)
        s.fields["media_type"] = stringValue(mediaTypeFor(icon))
        s.fields["size_bytes"] = numberValue(Double(size))
        s.fields["create_time"] = stringValue("2026-05-01T12:34:56Z")
        s.fields["icon"] = numberValue(Double(icon.rawValue))
        return s
    }
}

private func mediaTypeFor(_ icon: Pivox_Api_V1_Icon) -> String {
    switch icon {
    case .photo: return "IMAGE"
    case .video: return "VIDEO"
    case .audio: return "AUDIO"
    case .document: return "DOCUMENT"
    default: return ""
    }
}

private func stringValue(_ s: String) -> Google_Protobuf_Value {
    var v = Google_Protobuf_Value()
    v.stringValue = s
    return v
}

private func numberValue(_ n: Double) -> Google_Protobuf_Value {
    var v = Google_Protobuf_Value()
    v.numberValue = n
    return v
}
