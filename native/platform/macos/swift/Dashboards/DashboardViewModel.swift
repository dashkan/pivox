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

import Foundation
import Observation
import OSLog
import PivoxModels
import SwiftProtobuf

/// Per-instance view model owning the load → render lifecycle for
/// one `DashboardView`. Generic across dashboard names — Library
/// is a call site (`name:
/// "organizations/{org}/dashboards/library"`); future system
/// catalog entries (Activity, Members) and customer-owned
/// USER_MANAGED dashboards (`organizations/{org}/spaces/{space}/dashboards/{id}`)
/// instantiate the same VM with a different name.
///
/// State machine (`State` enum):
///
///   `.loading`  — initial state and during retry.
///   `.ready(d, rows)` — dashboard fetched, per-tile data resolved.
///   `.error(s)` — fetch failed; UI shows banner + retry.
///
/// Tile data is a `[Int: [Google_Protobuf_Struct]]` keyed by tile
/// index in `dashboard.gridLayout.tiles`. Tiles whose data fetch
/// failed get an empty array (the renderer's empty-state surface
/// catches that path); the dashboard as a whole stays `.ready` so
/// one bad widget doesn't blank the screen.
@Observable
@MainActor
final class DashboardViewModel {
    enum State {
        case loading
        case ready(Pivox_Api_V1_Dashboard, [Int: [Google_Protobuf_Struct]])
        case error(String)
    }

    /// Closure-based fetch function injected at construct time so
    /// tests can stub responses without standing up a real
    /// `DashboardClient`. Production wires these through
    /// `DashboardClient.getDashboard` / `queryDashboardData` via
    /// the convenience `init(name:client:initialState:)` below.
    typealias GetDashboardFunc = (_ name: String) async throws
        -> Pivox_Api_V1_Dashboard

    typealias QueryDataFunc = (
        _ parent: String,
        _ query: Pivox_Api_V1_ResourceQuery
    ) async throws -> Pivox_Api_V1_QueryDashboardDataResponse

    private(set) var state: State

    let name: String
    private let getDashboardFunc: GetDashboardFunc
    private let queryDataFunc: QueryDataFunc

    /// Test / preview / advanced init. Inject the two RPC closures
    /// directly — the ViewModel doesn't need a real
    /// `DashboardClient` past these two function values.
    init(
        name: String,
        initialState: State = .loading,
        getDashboard: @escaping GetDashboardFunc,
        queryDashboardData: @escaping QueryDataFunc
    ) {
        self.name = name
        self.state = initialState
        self.getDashboardFunc = getDashboard
        self.queryDataFunc = queryDashboardData
    }

    /// Production init — wires the closures through a real
    /// `DashboardClient`.
    convenience init(
        name: String,
        client: DashboardClient,
        initialState: State = .loading
    ) {
        self.init(
            name: name,
            initialState: initialState,
            getDashboard: { try await client.getDashboard(name: $0) },
            queryDashboardData: { try await client.queryDashboardData(parent: $0, query: $1) }
        )
    }

    /// Fetch the dashboard + every tile's data. Resets state to
    /// `.loading` on entry so retry surfaces feel responsive.
    func load() async {
        state = .loading
        do {
            let dashboard = try await getDashboardFunc(name)
            let rows = await loadAllTileData(dashboard)
            state = .ready(dashboard, rows)
        } catch {
            PivoxLog.dashboards.error(
                "Dashboard \(self.name) load failed: \(String(describing: error))"
            )
            state = .error(String(describing: error))
        }
    }

    /// Re-run `load()`. Same shape — `load()` already resets state
    /// to `.loading` on entry — but kept as a distinct method so
    /// retry call sites are explicit and discoverable in the view.
    func retry() async { await load() }

    // MARK: - Tile data

    private func loadAllTileData(
        _ dashboard: Pivox_Api_V1_Dashboard
    ) async -> [Int: [Google_Protobuf_Struct]] {
        let parent = Self.parentFromName(name)
        var out: [Int: [Google_Protobuf_Struct]] = [:]
        // Load tiles serially. Library has exactly one tile in v1;
        // when future system dashboards have multiple, swap to a
        // TaskGroup. Serial keeps error reporting simple in the
        // meantime.
        for (index, tile) in dashboard.gridLayout.tiles.enumerated() {
            guard case .collection = tile.widget.content else { continue }
            let dataSource = tile.widget.collection.dataSource
            guard case .resourceQuery = dataSource.source else { continue }
            do {
                let resp = try await queryDataFunc(parent, dataSource.resourceQuery)
                out[index] = resp.rows
            } catch {
                PivoxLog.dashboards.error(
                    "Dashboard \(self.name) tile \(index) data fetch failed: \(String(describing: error))"
                )
                // Per-tile failure: leave rows empty so the
                // renderer's empty-state surface fires; don't
                // bubble the error up and blank the whole
                // dashboard for one bad widget.
                out[index] = []
            }
        }
        return out
    }

    /// Derive the parent resource path from a dashboard name. The
    /// `QueryDashboardData` RPC's `parent` is the dashboard's
    /// scope (org or space), not the dashboard itself, so we strip
    /// the trailing `/dashboards/{id}` segment.
    ///
    ///   `organizations/{org}/dashboards/{id}`
    ///   →  `organizations/{org}`
    ///
    ///   `organizations/{org}/spaces/{space}/dashboards/{id}`
    ///   →  `organizations/{org}/spaces/{space}`
    static func parentFromName(_ name: String) -> String {
        // Walk back past the last "/dashboards/{id}" pair. If the
        // shape doesn't match (defensive), return the input — the
        // QueryDashboardData call will reject with InvalidArgument
        // and the error propagates through the .error path, which
        // is the right behavior for malformed names.
        let parts = name.split(separator: "/")
        guard parts.count >= 4,
              parts[parts.count - 2] == "dashboards"
        else {
            return name
        }
        return parts.dropLast(2).joined(separator: "/")
    }
}
