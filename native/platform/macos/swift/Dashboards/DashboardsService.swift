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

/// App-lifetime owner of the single `DashboardClient`. The gRPC
/// channel is heavy (HTTP/2 + auth interceptor + background
/// runConnections Task) and must outlive any `DashboardView`
/// instance — that's the entire reason this service exists.
///
/// What this service IS:
///   - lazy `var client: DashboardClient` — one instance per app.
///   - `reset()` — tears the client down on sign-out so the gRPC
///     channel doesn't carry an old user's auth token across
///     identities.
///
/// What this service is NOT:
///   - A connection state machine. There is no `connect()`,
///     `retryConnect()`, or `initError`. View / ViewModel layers
///     do not participate in the connection lifecycle. If the
///     client's underlying channel dies, the next RPC surfaces
///     a structured gRPC error and the ViewModel's
///     `state = .error(...)` path renders the retry button.
///   - A view-state observable. `DashboardViewModel` holds
///     per-screen state. This service is pure plumbing.
@MainActor
final class DashboardsService {
    static let shared = DashboardsService()

    private var _client: DashboardClient?

    /// The single `DashboardClient` instance. Lazily constructed
    /// on first access; if construction throws (CloudConfig parse
    /// failure — same blast radius as every other gRPC client)
    /// the caller crashes loudly. The alternative — returning an
    /// optional and forcing every call site to handle init failure
    /// — would mask a wiring bug that's already fatal for the
    /// rest of the app.
    var client: DashboardClient {
        if let c = _client { return c }
        do {
            let c = try DashboardClient()
            _client = c
            return c
        } catch {
            // CloudConfig parse failure means the app can't reach
            // any Pivox service — auth, chat, dashboards, all dead.
            // Crash here so the failure mode is loud rather than a
            // silently-broken dashboard surface.
            fatalError("DashboardsService: DashboardClient init failed: \(error)")
        }
    }

    /// Tear down the client — call on sign-out. Idempotent.
    func reset() {
        _client?.cancel()
        _client = nil
    }

    private init() {}
}
