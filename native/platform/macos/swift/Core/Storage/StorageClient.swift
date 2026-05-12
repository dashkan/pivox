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
import PivoxModels
import SwiftProtobuf

/// SessionResult carries the bearer JWT + its expiry as returned by
/// `CreateStorageSession`. Native callers (the upcoming
/// `AuthenticatedAsyncImage` in 6c.4) attach `token` as
/// `Authorization: Bearer <token>` on `/files/` fetches; the storage
/// agent's strict-prefix parser at `internal/storageagent/http.go`
/// consumes it.
///
/// `expiresAt` is the JWT's `exp` claim translated to a Foundation
/// `Date`. The `StorageService` cache layer reads it to decide
/// when to refresh.
struct SessionResult: Equatable, Sendable {
    let token: String
    let expiresAt: Date
}

/// Pivox cloud gRPC client for the StorageGateways service. Pure
/// Swift (grpc-swift-2). Wraps
/// `Pivox_Storage_V1_StorageGateways.Client` and shares the
/// plaintext-HTTP/2 + Firebase-auth-interceptor pattern that
/// `ChatClient` / `DashboardClient` use.
///
/// Singular `StorageClient` (not "StorageGatewaysClient" or
/// similar) matches the project's single-purpose gRPC client
/// naming.
///
/// Stateless past init: every method is a single RPC. Connection
/// lifecycle is owned by `StorageService` (one client per app,
/// `cancel()` on sign-out). Per-(user, org) session caching lives
/// in `StorageService`, not here.
///
/// Test seam: `MintFn` is a closure shaped like the underlying
/// gRPC call. The production `init()` wires it to the real
/// generated client; the test `init(mintFn:)` accepts a stub. This
/// matches the closure-injection pattern from
/// `DashboardViewModel` — exercising the wrapper's request-shape
/// composition + response-mapping logic without binding a real
/// gRPC channel in unit tests.
@MainActor
final class StorageClient {
    /// Closure shape that produces a `CreateStorageSession`
    /// response from a request — what production wires to the real
    /// gRPC client and tests stub directly.
    typealias MintFn = @MainActor (Pivox_Storage_V1_CreateStorageSessionRequest) async throws
        -> Pivox_Storage_V1_CreateStorageSessionResponse

    private let grpc: GRPCClient<HTTP2ClientTransport.Posix>?
    private let mintFn: MintFn
    private var runTask: Task<Void, Error>?
    private var didShutdown = false

    /// Production constructor — creates a real gRPC channel pointed
    /// at the configured Pivox cloud endpoint. Throws if
    /// `CloudConfig` can't parse the endpoint (same blast radius
    /// as every other gRPC client init).
    init() throws {
        let (host, port) = try CloudConfig.parsedEndpoint()
        let transport = try HTTP2ClientTransport.Posix(
            target: .dns(host: host, port: port),
            transportSecurity: CloudConfig.transportSecurity
        )
        let grpc = GRPCClient(
            transport: transport,
            interceptors: [FirebaseAuthInterceptor()]
        )
        let storage = Pivox_Storage_V1_StorageGateways.Client(wrapping: grpc)
        self.grpc = grpc
        self.mintFn = { req in
            try await storage.createStorageSession(req)
        }
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
                PivoxLog.storage.error(
                    "StorageClient gRPC connection ended: \(String(describing: error))"
                )
                throw error
            }
        }
    }

    /// Test-only constructor. Injects a stub `MintFn` so the unit
    /// test can capture the request the wrapper composes and
    /// fabricate a response. The real gRPC channel is bypassed —
    /// `cancel()` is a no-op on this construction path.
    init(mintFn: @escaping MintFn) {
        self.grpc = nil
        self.mintFn = mintFn
    }

    /// Cancel the background connection task and tear down the
    /// channel. Idempotent — safe to call multiple times. The
    /// `didShutdown` guard prevents the second call from invoking
    /// `beginGracefulShutdown()` twice (grpc-swift-2 requires
    /// exactly one call).
    func cancel() {
        guard !didShutdown else { return }
        didShutdown = true
        runTask?.cancel()
        runTask = nil
        grpc?.beginGracefulShutdown()
    }

    /// Mint a storage session for the given org. The returned
    /// `SessionResult.token` is the JWT to attach as `Authorization:
    /// Bearer` on subsequent `/files/` fetches; `expiresAt` is the
    /// JWT's `exp` claim time.
    ///
    /// `ttl` overrides the server's default session lifetime (1h);
    /// the server clamps to a configured maximum (typically 8h)
    /// silently.
    func createSession(
        forOrg orgID: String,
        ttl: TimeInterval? = nil
    ) async throws -> SessionResult {
        var req = Pivox_Storage_V1_CreateStorageSessionRequest()
        req.parent = "organizations/\(orgID)"
        if let ttl {
            var duration = SwiftProtobuf.Google_Protobuf_Duration()
            // Drop sub-second precision: storage sessions are
            // second-resolution per the proto spec; the server's
            // TTL clamp is also second-grained.
            duration.seconds = Int64(ttl)
            req.ttl = duration
        }
        let resp = try await mintFn(req)
        // Drop expiry.nanos: same second-grained contract on the
        // response side. JWT `exp` is a Unix-second integer per
        // RFC 7519; the proto Timestamp's nanos field is always 0
        // for storage sessions.
        return SessionResult(
            token: resp.token,
            expiresAt: Date(timeIntervalSince1970: TimeInterval(resp.expiry.seconds))
        )
    }
}
