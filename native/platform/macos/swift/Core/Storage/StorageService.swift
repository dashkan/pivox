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

/// Errors surfaced by `StorageService` to its callers.
enum StorageServiceError: Error, Equatable, CustomStringConvertible {
    /// `token(forOrg:)` was called when no Pivox user was active.
    /// The cache key shape is `(userID, orgID)` — without a user
    /// identity, even a successful mint can't be cached safely.
    /// Callers should sign in (or surface an auth-required UI)
    /// before retrying.
    case noActiveUser

    var description: String {
        switch self {
        case .noActiveUser:
            return "no active user — sign in before requesting a storage session"
        }
    }
}

/// App-lifetime owner of the per-(user, org) storage-session JWT
/// cache. Sits in front of `StorageClient.createSession` so the
/// upcoming `AuthenticatedAsyncImage` (6c.4) can ask for a bearer
/// token on every `/files/` fetch without hammering the controller
/// — first call mints, subsequent calls within the TTL hit cache,
/// near-expiry calls refresh, 401 responses invalidate.
///
/// What this service IS:
///   - lazy session minting per (active user, org) on first
///     `token(forOrg:)` call.
///   - in-memory cache, refreshed past `expiresAt -
///     refreshThreshold` (60s).
///   - `invalidate(forOrg:)` for the 401-driven refresh path.
///   - `reset()` for sign-out.
///   - sign-out observer wired in `init()` — observing
///     `.userDidSignOut` so we don't have to depend upward on
///     `AuthService` (the layer-direction rule from `AGENTS.md`).
///
/// What this service is NOT:
///   - A URL composer. Storage URLs are emitted by the controller
///     (Phase 6c.2's `composeStorageURL`) into the dashboard JSON;
///     the native side only fetches them. This service supplies
///     the bearer token, nothing else.
///   - An actor. `@MainActor` matches `AIChatService` /
///     `DashboardsService`; with single-threaded MainActor
///     isolation, the cache map can be a plain dict — no `NSLock`
///     needed.
///
/// Cache key shape: `(userID, orgID)`. Including userID is
/// load-bearing — a fast user-switch (sign out one user, sign in
/// another) without a cache key change would let user B inherit
/// user A's token. The "switching active user re-mints" test in
/// `StorageServiceTests` pins this against future regression.
@MainActor
final class StorageService {
    static let shared = StorageService()

    private struct CacheKey: Hashable {
        let userID: String
        let orgID: String
    }

    private struct CachedSession {
        let token: String
        let expiresAt: Date
    }

    /// Refresh threshold — sessions expiring within this window get
    /// re-minted on the next `token(forOrg:)` call rather than
    /// served from cache. 60s mirrors the typical UX latency budget:
    /// a token that's about to expire mid-fetch would 401 and force
    /// the caller to invalidate-then-retry; refreshing proactively
    /// closes that window.
    static let refreshThreshold: TimeInterval = 60

    private var sessions: [CacheKey: CachedSession] = [:]
    private let userIDProvider: @MainActor () -> String?

    /// Test-only stub mint. When non-nil, `mint(forOrg:)`
    /// dispatches here instead of through the lazily-constructed
    /// `_client`. Tests inject this to bypass the real gRPC
    /// channel.
    private let stubMint: (@MainActor (String) async throws -> SessionResult)?

    /// Lazy-constructed gRPC client (production path only). nil on
    /// the test path.
    private var _client: StorageClient?

    private var signOutObserver: NSObjectProtocol?

    /// Production constructor. Wires the active-user provider to
    /// `AuthService.shared.currentUser?.uid` (Firebase UID) and
    /// the mint path to a lazily-constructed `StorageClient`. The
    /// sign-out notification observer is registered in init so the
    /// cache flushes the moment the user signs out — without that,
    /// a fast re-sign-in could see another user's tokens.
    convenience init() {
        self.init(
            userIDProvider: { AuthService.shared.currentUser?.uid },
            stubMint: nil
        )
    }

    /// Test-injectable constructor. Pass a `userIDProvider` that
    /// reflects the test's notion of "current user" and a
    /// `stubMint` that fabricates a `SessionResult` per call.
    init(
        userIDProvider: @escaping @MainActor () -> String?,
        stubMint: (@MainActor (String) async throws -> SessionResult)? = nil
    ) {
        self.userIDProvider = userIDProvider
        self.stubMint = stubMint

        // Tear down all cached tokens on sign-out — same rationale
        // as `AIChatService`'s observer (cache binding to a
        // previous user's identity must not survive across user
        // identities). Observer fires on the main thread; reset()
        // is @MainActor so we hop explicitly.
        self.signOutObserver = NotificationCenter.default.addObserver(
            forName: .userDidSignOut, object: nil, queue: .main
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.reset()
            }
        }
    }

    deinit {
        if let observer = signOutObserver {
            NotificationCenter.default.removeObserver(observer)
        }
    }

    // MARK: - Public API

    /// Returns a bearer JWT for the given org, scoped to the
    /// currently-active user. Mints lazily on first call per
    /// (user, org); subsequent calls within the TTL window
    /// (`expiresAt - refreshThreshold`) hit cache; calls past that
    /// window re-mint.
    ///
    /// Throws `StorageServiceError.noActiveUser` when called with
    /// no signed-in user. Propagates any error from the mint
    /// function (gRPC-layer faults, controller-side rejections).
    func token(forOrg orgID: String) async throws -> String {
        guard let userID = userIDProvider(), !userID.isEmpty else {
            throw StorageServiceError.noActiveUser
        }
        let key = CacheKey(userID: userID, orgID: orgID)
        if let cached = sessions[key],
            cached.expiresAt > Date().addingTimeInterval(Self.refreshThreshold)
        {
            return cached.token
        }
        let result = try await mint(forOrg: orgID)
        sessions[key] = CachedSession(
            token: result.token, expiresAt: result.expiresAt)
        return result.token
    }

    /// Drop the cached entry for (current user, org). The next
    /// `token(forOrg:)` call re-mints. Called by the upcoming
    /// `AuthenticatedAsyncImage` (6c.4) on a 401 response — the
    /// agent's auth gate rejected the token, so it's stale or
    /// revoked; force a fresh mint on the retry.
    ///
    /// No-op when no user is active (nothing to invalidate).
    func invalidate(forOrg orgID: String) {
        guard let userID = userIDProvider(), !userID.isEmpty else { return }
        sessions.removeValue(forKey: CacheKey(userID: userID, orgID: orgID))
    }

    /// Drop every cached session and tear down the gRPC channel.
    /// Called on sign-out (via the `.userDidSignOut` observer
    /// wired in `init`). Idempotent.
    func reset() {
        sessions.removeAll()
        _client?.cancel()
        _client = nil
    }

    // MARK: - Internals

    /// Dispatch to either the test stub or the lazily-constructed
    /// production client. Production-path errors include
    /// `CloudConfig` parse failure (same blast radius as every
    /// other gRPC client construction).
    private func mint(forOrg orgID: String) async throws -> SessionResult {
        if let stubMint {
            return try await stubMint(orgID)
        }
        let client: StorageClient
        if let existing = _client {
            client = existing
        } else {
            client = try StorageClient()
            _client = client
        }
        return try await client.createSession(forOrg: orgID)
    }
}
