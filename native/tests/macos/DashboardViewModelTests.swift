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
import XCTest

@testable import Pivox

/// Tests for `DashboardViewModel`. The VM owns the load → render
/// state machine for one `DashboardView`; tests inject stub
/// fetch closures (no real `DashboardClient`) and assert state
/// transitions.
///
/// Per the Phase 6b kickoff (decision B-i): ViewModel + stub
/// closures subsume client-level coverage. The closures are the
/// surface — what `DashboardClient` does past invoking them is
/// the gRPC layer's concern. When `DashboardClient` grows real
/// surface (caching, dedup, retry policy), client-level tests
/// earn their keep then.
@MainActor
final class DashboardViewModelTests: XCTestCase {
    // MARK: - load() happy path

    func testLoad_HappyPath_TransitionsToReady() async {
        let dashboard = makeDashboard(
            name: "organizations/acme/dashboards/library",
            tiles: [makeAssetTile()]
        )
        let queryResp = makeQueryResponse(rowCount: 3)

        let vm = DashboardViewModel(
            name: "organizations/acme/dashboards/library",
            getDashboard: { _ in dashboard },
            queryDashboardData: { _, _ in queryResp }
        )

        await vm.load()

        guard case .ready(let loaded, let rows) = vm.state else {
            XCTFail("expected .ready, got \(vm.state)")
            return
        }
        XCTAssertEqual(loaded.name, dashboard.name)
        XCTAssertEqual(rows[0]?.count, 3)
    }

    // MARK: - load() error path

    func testLoad_GetDashboardThrows_TransitionsToError() async {
        struct StubError: Error, CustomStringConvertible {
            var description: String { "network unreachable" }
        }
        let vm = DashboardViewModel(
            name: "organizations/acme/dashboards/library",
            getDashboard: { _ in throw StubError() },
            queryDashboardData: { _, _ in
                XCTFail("queryDashboardData should not run when getDashboard throws")
                return Pivox_Api_V1_QueryDashboardDataResponse()
            }
        )

        await vm.load()

        guard case .error(let message) = vm.state else {
            XCTFail("expected .error, got \(vm.state)")
            return
        }
        XCTAssertTrue(
            message.contains("network unreachable"),
            "error message must surface the underlying failure: \(message)"
        )
    }

    // MARK: - load() per-tile failure

    func testLoad_QueryDataThrows_KeepsDashboardReadyWithEmptyRows() async {
        // Per-tile failure must NOT blank the whole dashboard.
        // The tile gets an empty rows array (renderer's
        // empty-state surface fires); the dashboard stays `.ready`.
        let dashboard = makeDashboard(
            name: "organizations/acme/dashboards/library",
            tiles: [makeAssetTile()]
        )
        struct StubError: Error {}
        let vm = DashboardViewModel(
            name: "organizations/acme/dashboards/library",
            getDashboard: { _ in dashboard },
            queryDashboardData: { _, _ in throw StubError() }
        )

        await vm.load()

        guard case .ready(_, let rows) = vm.state else {
            XCTFail("expected .ready (one bad tile must not blank the dashboard), got \(vm.state)")
            return
        }
        XCTAssertEqual(rows[0]?.count, 0,
            "failed tile must report empty rows so the renderer's empty-state path renders")
    }

    // MARK: - retry()

    func testRetry_AfterError_RecoversToReady() async {
        let dashboard = makeDashboard(
            name: "organizations/acme/dashboards/library",
            tiles: [makeAssetTile()]
        )
        var callCount = 0
        let vm = DashboardViewModel(
            name: "organizations/acme/dashboards/library",
            getDashboard: { _ in
                callCount += 1
                if callCount == 1 {
                    struct E: Error {}
                    throw E()
                }
                return dashboard
            },
            queryDashboardData: { _, _ in makeQueryResponse(rowCount: 2) }
        )

        await vm.load()
        guard case .error = vm.state else {
            XCTFail("expected .error after first load, got \(vm.state)")
            return
        }

        await vm.retry()
        guard case .ready(_, let rows) = vm.state else {
            XCTFail("expected .ready after retry, got \(vm.state)")
            return
        }
        XCTAssertEqual(rows[0]?.count, 2)
        XCTAssertEqual(callCount, 2, "retry must invoke getDashboard a second time")
    }

    // MARK: - parentFromName

    func testParentFromName_OrgScoped() {
        XCTAssertEqual(
            DashboardViewModel.parentFromName("organizations/acme/dashboards/library"),
            "organizations/acme"
        )
    }

    func testParentFromName_SpaceScoped() {
        XCTAssertEqual(
            DashboardViewModel.parentFromName("organizations/acme/spaces/dev/dashboards/sprint"),
            "organizations/acme/spaces/dev"
        )
    }

    func testParentFromName_MalformedReturnsInputUnchanged() {
        // Defensive: malformed names pass through verbatim. The
        // QueryDashboardData call rejects with InvalidArgument
        // and the .error path handles. Don't crash on bad input.
        XCTAssertEqual(
            DashboardViewModel.parentFromName("not/a/dashboard/name"),
            "not/a/dashboard/name"
        )
    }

    // MARK: - non-collection tiles are skipped

    func testLoad_SkipsTilesWithoutCollectionContent() async {
        // A tile with non-collection content (statistic, chart, etc.)
        // must not trigger a queryDashboardData call. The dashboard
        // still resolves .ready; the non-collection tile just
        // doesn't appear in the rows map.
        var statTile = Pivox_Api_V1_Tile()
        var statWidget = Pivox_Api_V1_Widget()
        statWidget.content = .statistic(Pivox_Api_V1_StatisticWidget())
        statTile.widget = statWidget

        let dashboard = makeDashboard(
            name: "organizations/acme/dashboards/x",
            tiles: [statTile, makeAssetTile()]
        )

        var queryCalls = 0
        let vm = DashboardViewModel(
            name: "organizations/acme/dashboards/x",
            getDashboard: { _ in dashboard },
            queryDashboardData: { _, _ in
                queryCalls += 1
                return makeQueryResponse(rowCount: 1)
            }
        )

        await vm.load()

        guard case .ready(_, let rows) = vm.state else {
            XCTFail("expected .ready, got \(vm.state)")
            return
        }
        XCTAssertEqual(queryCalls, 1, "exactly one queryDashboardData call (the collection tile)")
        XCTAssertNil(rows[0], "statistic tile (index 0) has no rows entry")
        XCTAssertEqual(rows[1]?.count, 1, "collection tile (index 1) has rows")
    }
}

// MARK: - Helpers

@MainActor
private func makeDashboard(
    name: String,
    tiles: [Pivox_Api_V1_Tile]
) -> Pivox_Api_V1_Dashboard {
    var d = Pivox_Api_V1_Dashboard()
    d.name = name
    d.displayName = "Test Dashboard"
    var grid = Pivox_Api_V1_GridLayout()
    grid.columns = 12
    grid.tiles = tiles
    d.layout = .gridLayout(grid)
    return d
}

@MainActor
private func makeAssetTile() -> Pivox_Api_V1_Tile {
    var widget = Pivox_Api_V1_CollectionWidget()
    var dataSource = Pivox_Api_V1_DataSource()
    var query = Pivox_Api_V1_ResourceQuery()
    query.resourceType = "pivox.assets/Asset"
    dataSource.source = .resourceQuery(query)
    widget.dataSource = dataSource

    var w = Pivox_Api_V1_Widget()
    w.content = .collection(widget)

    var tile = Pivox_Api_V1_Tile()
    tile.width = 12
    tile.height = 8
    tile.widget = w
    return tile
}

@MainActor
private func makeQueryResponse(rowCount: Int) -> Pivox_Api_V1_QueryDashboardDataResponse {
    var resp = Pivox_Api_V1_QueryDashboardDataResponse()
    resp.rows = (0..<rowCount).map { i in
        var s = Google_Protobuf_Struct()
        var v = Google_Protobuf_Value()
        v.stringValue = "row-\(i)"
        s.fields["display_name"] = v
        return s
    }
    return resp
}
