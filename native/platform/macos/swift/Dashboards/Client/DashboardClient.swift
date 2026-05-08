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
import GRPCCore
import GRPCNIOTransportHTTP2
import GRPCProtobuf
import OSLog
import PivoxModels
import SwiftProtobuf

/// Pivox cloud gRPC client for the Dashboards service. Pure Swift
/// (grpc-swift-2). Wraps the generated
/// `Pivox_Api_V1_Dashboards.Client` and shares the same plaintext
/// HTTP/2 + Firebase-auth-interceptor pattern as `ChatClient` and
/// `OrgsClient`.
///
/// Singular `DashboardClient` (not "Dashboards…") matches
/// `ChatClient` so naming is consistent across single-purpose
/// gRPC client wrappers.
///
/// Stateless past init: every method is a wrapper around a single
/// gRPC call. Connection lifecycle is owned by `DashboardsService`
/// (one client per app, reset on sign-out). `DashboardViewModel`
/// holds the per-screen state.
@MainActor
final class DashboardClient {
    private let grpc: GRPCClient<HTTP2ClientTransport.Posix>
    private let dashboards: Pivox_Api_V1_Dashboards.Client<HTTP2ClientTransport.Posix>
    private var runTask: Task<Void, Error>?
    private var didShutdown = false

    init() throws {
        let (host, port) = try CloudConfig.parsedEndpoint()
        let transport = try HTTP2ClientTransport.Posix(
            target: .dns(host: host, port: port),
            transportSecurity: CloudConfig.transportSecurity
        )
        self.grpc = GRPCClient(
            transport: transport,
            interceptors: [FirebaseAuthInterceptor()]
        )
        self.dashboards = Pivox_Api_V1_Dashboards.Client(wrapping: grpc)
        // Surface the underlying error if the channel dies for a
        // non-cancellation reason (TLS handshake, DNS, peer reset).
        // Without the catch the throw would vanish into the
        // unobserved Task and the next RPC would either hang or
        // fail with an opaque error.
        self.runTask = Task { [grpc] in
            do {
                try await grpc.runConnections()
            } catch is CancellationError {
                // expected on cancel()
            } catch {
                PivoxLog.dashboards.error(
                    "DashboardClient gRPC connection ended: \(String(describing: error))"
                )
                throw error
            }
        }
    }

    /// Cancel the background connection task and tear down the
    /// channel. Idempotent — safe to call multiple times; the
    /// `didShutdown` guard prevents the second call from invoking
    /// `beginGracefulShutdown()` twice (grpc-swift-2 requires
    /// exactly one call).
    func cancel() {
        guard !didShutdown else { return }
        didShutdown = true
        runTask?.cancel()
        runTask = nil
        grpc.beginGracefulShutdown()
    }

    // MARK: - RPCs

    /// Lists dashboards under a parent. The parent may be an org
    /// (`organizations/{org}`) — returns the system-curated catalog
    /// of SYSTEM_MANAGED dashboards — or a space
    /// (`organizations/{org}/spaces/{space}`) — returns the
    /// user-owned USER_MANAGED dashboards persisted there.
    func listDashboards(
        parent: String,
        pageSize: Int32 = 0,
        pageToken: String = ""
    ) async throws -> Pivox_Api_V1_ListDashboardsResponse {
        var req = Pivox_Api_V1_ListDashboardsRequest()
        req.parent = parent
        req.pageSize = pageSize
        req.pageToken = pageToken
        return try await dashboards.listDashboards(req)
    }

    /// Returns one dashboard by name. Org-scoped names resolve
    /// through the server's in-memory catalog; space-scoped names
    /// resolve through the dashboards table.
    func getDashboard(name: String) async throws -> Pivox_Api_V1_Dashboard {
        var req = Pivox_Api_V1_GetDashboardRequest()
        req.name = name
        return try await dashboards.getDashboard(req)
    }

    /// Resolves a `ResourceQuery` into rows of data for rendering
    /// inside a `CollectionWidget`. v1 supports
    /// `query.resourceType = "pivox.assets/Asset"`; other resource
    /// types return Unimplemented at the server. The opaque
    /// `pageToken` round-trips verbatim from a prior response —
    /// callers MUST NOT decode or synthesize tokens.
    func queryDashboardData(
        parent: String,
        query: Pivox_Api_V1_ResourceQuery,
        pageSize: Int32 = 0,
        pageToken: String = ""
    ) async throws -> Pivox_Api_V1_QueryDashboardDataResponse {
        var req = Pivox_Api_V1_QueryDashboardDataRequest()
        req.parent = parent
        req.query = query
        req.pageSize = pageSize
        req.pageToken = pageToken
        return try await dashboards.queryDashboardData(req)
    }

    // MARK: - Mutations
    //
    // CreateDashboard / UpdateDashboard / DeleteDashboard are
    // exposed for completeness even though the v1 macOS UX surfaces
    // only the SYSTEM_MANAGED catalog. They wire the customer-side
    // CRUD path that ships server-side in Phase 4b; UX consumers
    // (a future "create custom dashboard" flow) call them directly.

    /// Creates a USER_MANAGED dashboard under a space parent.
    /// `dashboardID` is required — server-side auto-generation is
    /// deferred (see issue #88).
    func createDashboard(
        parent: String,
        dashboardID: String,
        dashboard: Pivox_Api_V1_Dashboard
    ) async throws -> Pivox_Api_V1_Dashboard {
        var req = Pivox_Api_V1_CreateDashboardRequest()
        req.parent = parent
        req.dashboardID = dashboardID
        req.dashboard = dashboard
        return try await dashboards.createDashboard(req)
    }

    /// Updates an existing dashboard. The supplied `dashboard.etag`
    /// is required for optimistic concurrency (per AIP-154).
    func updateDashboard(
        dashboard: Pivox_Api_V1_Dashboard,
        updateMask: Google_Protobuf_FieldMask? = nil
    ) async throws -> Pivox_Api_V1_Dashboard {
        var req = Pivox_Api_V1_UpdateDashboardRequest()
        req.dashboard = dashboard
        if let updateMask {
            req.updateMask = updateMask
        }
        return try await dashboards.updateDashboard(req)
    }

    /// Soft-deletes a USER_MANAGED dashboard. SYSTEM_MANAGED
    /// dashboards reject with `FailedPrecondition`.
    func deleteDashboard(name: String) async throws -> Pivox_Api_V1_Dashboard {
        var req = Pivox_Api_V1_DeleteDashboardRequest()
        req.name = name
        return try await dashboards.deleteDashboard(req)
    }
}

/// Errors surfaced by `DashboardClient` to UI code. Surface area is
/// deliberately narrow — gRPC status errors propagate as
/// `RPCError` from grpc-swift-2 and the renderer is the right place
/// to translate them into customer-visible messages.
enum DashboardClientError: Error, CustomStringConvertible {
    /// The server reported the requested resource type as
    /// unimplemented. v1 supports only `pivox.assets/Asset` for
    /// `QueryDashboardData`; surface this distinctly so the UI can
    /// hide the widget rather than display a generic error.
    case resourceTypeUnimplemented(String)

    var description: String {
        switch self {
        case .resourceTypeUnimplemented(let resourceType):
            return "Resource type \(resourceType) is not supported in this client version."
        }
    }
}
