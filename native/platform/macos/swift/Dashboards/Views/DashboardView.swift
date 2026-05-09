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

/// Generic dashboard surface. Constructed with a dashboard
/// resource name; observes the per-instance `DashboardViewModel`
/// and dispatches on `state` (.loading, .error, .ready). Knows
/// nothing about clients or services — those flow through the
/// ViewModel.
///
/// Library is one call site of N. Future system catalog entries
/// (Activity, Members) and customer-owned USER_MANAGED dashboards
/// instantiate the same view with a different name; renderer
/// changes propagate to every dashboard simultaneously.
///
/// Init constructs a fresh ViewModel pinned to the supplied name
/// + `DashboardsService.shared.client`. The `@State`-stored
/// ViewModel survives view re-renders within the same name; if
/// the parent passes a different name, SwiftUI re-creates the
/// view (because `name` is a stable identity property worth
/// re-keying on at the call site).
struct DashboardView: View {
    let name: String

    @State private var viewModel: DashboardViewModel

    init(name: String) {
        self.name = name
        _viewModel = State(
            initialValue: DashboardViewModel(
                name: name,
                client: DashboardsService.shared.client
            )
        )
    }

    /// Test / preview constructor — accepts a pre-configured
    /// ViewModel so SwiftUI Previews and unit tests can pin
    /// the state without bringing up a real gRPC client.
    init(viewModel: DashboardViewModel) {
        self.name = viewModel.name
        _viewModel = State(initialValue: viewModel)
    }

    var body: some View {
        Group {
            switch viewModel.state {
            case .loading:
                ProgressView("Loading dashboard…")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            case .error(let message):
                errorBanner(message)
            case .ready(let dashboard, let rowsByTile):
                dashboardBody(dashboard, rowsByTile: rowsByTile)
            }
        }
        // .task(id: name) re-runs load() if `name` changes — the
        // belt to ContentView's .id(orgID) suspenders. Honors the
        // header docstring's "name is a stable identity property
        // worth re-keying on" contract internally rather than
        // depending on every future call site getting .id() right.
        .task(id: name) { await viewModel.load() }
    }

    @ViewBuilder
    private func dashboardBody(
        _ dashboard: Pivox_Api_V1_Dashboard,
        rowsByTile: [Int: [Google_Protobuf_Struct]]
    ) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 4) {
                Text(dashboard.displayName)
                    .font(.title2.weight(.semibold))
                if !dashboard.description_p.isEmpty {
                    Text(dashboard.description_p)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding()

            Divider()

            ForEach(Array(dashboard.gridLayout.tiles.enumerated()), id: \.offset) { index, tile in
                if case .collection = tile.widget.content {
                    CollectionWidgetView(
                        widget: tile.widget.collection,
                        rows: rowsByTile[index] ?? [],
                        onAction: handleAction
                    )
                } else {
                    // v1 only renders CollectionWidget tiles. Other
                    // widget kinds (statistic, chart, pie) get
                    // skipped here — the renderer grows matching
                    // views before any dashboard ships them.
                    EmptyView()
                }
            }
        }
        // Anchor to top-leading of the parent (mainDetail's ZStack
        // has default .center alignment, which would vertically
        // center an intrinsically-sized dashboardBody and produce
        // the floating-table-header look). Filling both axes lets
        // CollectionWidgetView's ScrollView claim the remaining
        // height so the table fills the viewport.
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    @ViewBuilder
    private func errorBanner(_ message: String) -> some View {
        VStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle")
                .font(.system(size: 48, weight: .light))
                .foregroundStyle(.orange)
            Text("Could not load dashboard")
                .font(.title3.weight(.semibold))
            Text(message)
                .font(.callout)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 480)
            Button("Retry") {
                Task { await viewModel.retry() }
            }
            .buttonStyle(.borderedProminent)
            .accessibilityIdentifier("dashboard-retry")
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    private func handleAction(_ action: Pivox_Api_V1_RowAction) {
        // 6c+ wires this into a per-action dispatcher. For now
        // log + no-op so the renderer compiles + previews while
        // the action surface is read-only.
        PivoxLog.dashboards.info("Dashboard action invoked: \(action.key)")
    }
}

// MARK: - SwiftUI Previews

/// `PreviewClient` is a stub `DashboardClient` peer-shape for
/// preview state injection. It isn't wired through the real init
/// path — previews construct a `DashboardViewModel` with a
/// pre-baked `state`, then pass it through the test/preview
/// constructor.
///
/// Three previews, one per state, per the Phase 6b kickoff
/// guidance: `.loading`, `.error(...)`, `.ready(...)`.

#Preview("DashboardView — loading") {
    DashboardView(viewModel: previewViewModel(state: .loading))
        .frame(width: 720, height: 480)
}

#Preview("DashboardView — error with retry") {
    DashboardView(viewModel: previewViewModel(
        state: .error("Network unreachable: could not reach api.pivox.io")
    ))
    .frame(width: 720, height: 480)
}

#Preview("DashboardView — ready (3 mock asset rows)") {
    DashboardView(viewModel: previewViewModel(
        state: .ready(previewDashboard(), [0: previewRows()])
    ))
    .frame(width: 720, height: 480)
}

@MainActor
private func previewViewModel(state: DashboardViewModel.State) -> DashboardViewModel {
    // Inject stub closures rather than reaching for the real
    // DashboardsService.shared.client — the convenience init would
    // construct a real DashboardClient (CloudConfig parse + gRPC
    // channel + runConnections Task) on every preview render, and
    // would fatalError on any preview machine without a usable
    // CloudConfig. The closure-based init is the test/preview
    // seam exactly to avoid that.
    DashboardViewModel(
        name: "organizations/preview/dashboards/library",
        initialState: state,
        getDashboard: { _ in previewDashboard() },
        queryDashboardData: { _, _ in
            var resp = Pivox_Api_V1_QueryDashboardDataResponse()
            resp.rows = previewRows()
            return resp
        }
    )
}

private func previewDashboard() -> Pivox_Api_V1_Dashboard {
    var d = Pivox_Api_V1_Dashboard()
    d.name = "organizations/preview/dashboards/library"
    d.displayName = "Library"
    d.description_p = "Browse every asset in the organization across all spaces."
    d.managementMode = .systemManaged

    var grid = Pivox_Api_V1_GridLayout()
    grid.columns = 12

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
    ] {
        var col = Pivox_Api_V1_Column()
        col.field = field
        col.displayName = name
        col.visible = true
        widget.columns.append(col)
    }

    var tile = Pivox_Api_V1_Tile()
    tile.xPos = 0
    tile.yPos = 0
    tile.width = 12
    tile.height = 8
    var w = Pivox_Api_V1_Widget()
    w.title = "Asset Library"
    w.content = .collection(widget)
    tile.widget = w
    grid.tiles.append(tile)

    d.layout = .gridLayout(grid)
    return d
}

private func previewRows() -> [Google_Protobuf_Struct] {
    let raw: [(String, Pivox_Api_V1_Icon, Int64)] = [
        ("Logo Final.png", .photo, 124_532),
        ("Hero Reel.mp4", .video, 12_500_000),
        ("Soundbed.wav", .audio, 4_120_500),
    ]
    return raw.map { name, icon, size in
        var s = Google_Protobuf_Struct()
        s.fields["display_name"] = stringValue(name)
        s.fields["media_type"] = stringValue(mediaTypeFor(icon))
        s.fields["size_bytes"] = numberValue(Double(size))
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
