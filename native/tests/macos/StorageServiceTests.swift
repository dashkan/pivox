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

import XCTest

@testable import Pivox

/// Tests for `StorageService` — the per-(user, org) JWT cache layer
/// in front of `StorageClient.createSession`. Tests inject a stub
/// `mintFn` and `userIDProvider` so cache behavior is observable
/// without a real gRPC channel.
///
/// Coverage matrix maps 1:1 to the dispatch's required cases:
///
///   - lazy mint on first token() call per (user, org)
///   - second call within TTL returns cached token, no mint
///   - call within (expiresAt - 60s) returns cached, no mint
///   - call past (expiresAt - 60s) refreshes — new mint
///   - invalidate(forOrg) drops cache, next call re-mints
///   - reset() drops all cached sessions
///   - switching active user re-mints for same org (CRITICAL —
///     pins the cache key shape against the stale-@State-on-org-
///     switch class of bug)
///   - sign-out clears all cached sessions
@MainActor
final class StorageServiceTests: XCTestCase {
    /// A stub mint that returns deterministic tokens + a caller-
    /// controlled expiry, and counts how many times it was called.
    /// Keeps test arithmetic obvious — `mints[orgID]` is the per-org
    /// invocation count.
    final class StubMint {
        private(set) var mints: [String: Int] = [:]
        private(set) var callOrder: [String] = []
        var nextExpiry: Date

        init(expiresIn: TimeInterval = 3600) {
            self.nextExpiry = Date().addingTimeInterval(expiresIn)
        }

        func fn() -> @MainActor (String) async throws -> SessionResult {
            return { [weak self] orgID in
                guard let self else {
                    throw NSError(domain: "test", code: 0)
                }
                self.mints[orgID, default: 0] += 1
                self.callOrder.append(orgID)
                return SessionResult(
                    token: "token-for-\(orgID)-mint-\(self.mints[orgID]!)",
                    expiresAt: self.nextExpiry
                )
            }
        }
    }

    // MARK: - lazy mint

    func testToken_FirstCall_Mints() async throws {
        let stub = StubMint()
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: stub.fn()
        )

        let token = try await svc.token(forOrg: "meridian-broad")

        XCTAssertEqual(token, "token-for-meridian-broad-mint-1")
        XCTAssertEqual(stub.mints["meridian-broad"], 1)
    }

    // MARK: - cache hit within TTL

    func testToken_SecondCallWithinTTL_ReturnsCached() async throws {
        let stub = StubMint(expiresIn: 3600)
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: stub.fn()
        )

        let first = try await svc.token(forOrg: "meridian-broad")
        let second = try await svc.token(forOrg: "meridian-broad")

        XCTAssertEqual(first, second, "second call must reuse the first mint's token")
        XCTAssertEqual(stub.mints["meridian-broad"], 1, "no second mint")
    }

    // MARK: - refresh threshold

    func testToken_BeforeRefreshThreshold_StaysCached() async throws {
        // Token expires in 90s; refresh threshold is 60s; so a call
        // right now should still see >30s remaining and skip the
        // refresh.
        let stub = StubMint(expiresIn: 90)
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: stub.fn()
        )

        _ = try await svc.token(forOrg: "meridian-broad")
        let second = try await svc.token(forOrg: "meridian-broad")

        XCTAssertEqual(second, "token-for-meridian-broad-mint-1")
        XCTAssertEqual(stub.mints["meridian-broad"], 1)
    }

    func testToken_PastRefreshThreshold_Refreshes() async throws {
        // Token expires in 30s; refresh threshold is 60s; so the
        // cached entry is already inside the refresh window when
        // we look at it, and the second call re-mints.
        let stub = StubMint(expiresIn: 30)
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: stub.fn()
        )

        let first = try await svc.token(forOrg: "meridian-broad")
        let second = try await svc.token(forOrg: "meridian-broad")

        XCTAssertEqual(first, "token-for-meridian-broad-mint-1")
        XCTAssertEqual(second, "token-for-meridian-broad-mint-2")
        XCTAssertEqual(stub.mints["meridian-broad"], 2,
            "near-expiry cached entry must trigger a fresh mint")
    }

    // MARK: - invalidate

    func testInvalidate_DropsCache_NextCallRemints() async throws {
        let stub = StubMint(expiresIn: 3600)
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: stub.fn()
        )

        _ = try await svc.token(forOrg: "meridian-broad")
        svc.invalidate(forOrg: "meridian-broad")
        let after = try await svc.token(forOrg: "meridian-broad")

        XCTAssertEqual(after, "token-for-meridian-broad-mint-2",
            "invalidate must drop the cached entry; next token() re-mints")
        XCTAssertEqual(stub.mints["meridian-broad"], 2)
    }

    func testInvalidate_OtherOrgsUnaffected() async throws {
        let stub = StubMint(expiresIn: 3600)
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: stub.fn()
        )

        _ = try await svc.token(forOrg: "alpha")
        _ = try await svc.token(forOrg: "beta")
        svc.invalidate(forOrg: "alpha")
        _ = try await svc.token(forOrg: "alpha") // re-mints
        _ = try await svc.token(forOrg: "beta")  // still cached

        XCTAssertEqual(stub.mints["alpha"], 2, "alpha invalidated and re-minted")
        XCTAssertEqual(stub.mints["beta"], 1, "beta invalidation not affected")
    }

    // MARK: - reset

    func testReset_DropsAll() async throws {
        let stub = StubMint(expiresIn: 3600)
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: stub.fn()
        )

        _ = try await svc.token(forOrg: "alpha")
        _ = try await svc.token(forOrg: "beta")
        svc.reset()
        _ = try await svc.token(forOrg: "alpha")
        _ = try await svc.token(forOrg: "beta")

        XCTAssertEqual(stub.mints["alpha"], 2, "alpha re-minted after reset()")
        XCTAssertEqual(stub.mints["beta"], 2, "beta re-minted after reset()")
    }

    // MARK: - cache key includes user identity (CRITICAL)

    func testToken_SwitchingActiveUser_SameOrg_Remints() async throws {
        let stub = StubMint(expiresIn: 3600)
        // The userIDProvider returns whatever currentUser refers to
        // at the moment it's called — exactly mirroring AuthService's
        // mutable session.
        var currentUser = "user-a"
        let svc = StorageService(
            userIDProvider: { currentUser },
            stubMint: stub.fn()
        )

        let asUserA = try await svc.token(forOrg: "meridian-broad")
        currentUser = "user-b"
        let asUserB = try await svc.token(forOrg: "meridian-broad")

        XCTAssertEqual(asUserA, "token-for-meridian-broad-mint-1")
        XCTAssertEqual(asUserB, "token-for-meridian-broad-mint-2",
            "active-user switch must re-mint even for the same org — " +
            "the cache key includes user identity")
        XCTAssertEqual(stub.mints["meridian-broad"], 2)
    }

    func testToken_SwitchingBackToFirstUser_HitsOriginalCache() async throws {
        // Same scenario as above but switch back to user-a — the
        // user-a cache entry from the first mint should still be
        // there (assuming TTL hasn't elapsed). Pins the per-(user,
        // org) cache shape rather than a single-user cache that
        // gets clobbered on every switch.
        let stub = StubMint(expiresIn: 3600)
        var currentUser = "user-a"
        let svc = StorageService(
            userIDProvider: { currentUser },
            stubMint: stub.fn()
        )

        let asUserAFirst = try await svc.token(forOrg: "meridian-broad")
        currentUser = "user-b"
        _ = try await svc.token(forOrg: "meridian-broad")
        currentUser = "user-a"
        let asUserAAgain = try await svc.token(forOrg: "meridian-broad")

        XCTAssertEqual(asUserAFirst, asUserAAgain,
            "switching back to user-a must hit user-a's original " +
            "cached token — proves the cache key is per-(user, org)")
        XCTAssertEqual(stub.mints["meridian-broad"], 2,
            "only two mints total: user-a's first call, user-b's call. " +
            "user-a's second call hits cache.")
    }

    // MARK: - sign-out (covered by reset)

    func testSignOut_ClearsAllSessions() async throws {
        // Sign-out is exposed via reset() (called by the
        // .userDidSignOut observer wired in init). This test pins
        // the contract: after reset(), no cached entry survives —
        // so a re-signed-in user can't accidentally inherit a
        // previous user's token.
        let stub = StubMint(expiresIn: 3600)
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: stub.fn()
        )

        _ = try await svc.token(forOrg: "alpha")
        _ = try await svc.token(forOrg: "beta")
        // Sign-out path:
        svc.reset()
        // New user signs in — same StorageService instance survives
        // (it's a singleton) but every cached entry is gone.
        _ = try await svc.token(forOrg: "alpha")

        XCTAssertEqual(stub.mints["alpha"], 2,
            "reset() must drop the alpha entry; next token() re-mints")
    }

    // MARK: - error handling

    func testToken_NoActiveUser_Throws() async throws {
        let stub = StubMint(expiresIn: 3600)
        let svc = StorageService(
            userIDProvider: { nil },
            stubMint: stub.fn()
        )

        do {
            _ = try await svc.token(forOrg: "meridian-broad")
            XCTFail("expected token() to throw when no user is active")
        } catch StorageServiceError.noActiveUser {
            // expected — caller can't compose a per-(user, org) cache
            // key without a user identity.
        }
        XCTAssertEqual(stub.mints["meridian-broad", default: 0], 0,
            "no mint attempted when there's no user")
    }

    func testToken_MintFails_PropagatesError() async throws {
        struct StubError: Error {}
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: { _ in throw StubError() }
        )

        do {
            _ = try await svc.token(forOrg: "meridian-broad")
            XCTFail("expected token() to propagate the mint error")
        } catch is StubError {
            // expected
        }
    }

    func testToken_MintFails_DoesNotCacheError() async throws {
        // Pin the contract: a failed mint must NOT poison the cache.
        // The next call retries the mint; if a regression accidentally
        // cached a partial/empty result on error, the second call
        // would silently skip the mint and return garbage.
        struct StubError: Error {}
        var calls = 0
        let svc = StorageService(
            userIDProvider: { "user-a" },
            stubMint: { _ in
                calls += 1
                if calls == 1 { throw StubError() }
                return SessionResult(
                    token: "recovered-token",
                    expiresAt: Date().addingTimeInterval(3600)
                )
            }
        )

        do {
            _ = try await svc.token(forOrg: "meridian-broad")
            XCTFail("first call must throw")
        } catch is StubError {
            // expected
        }
        let second = try await svc.token(forOrg: "meridian-broad")

        XCTAssertEqual(second, "recovered-token",
            "a failed mint must not be cached — next call retries the mint")
        XCTAssertEqual(calls, 2, "two mint attempts: failed first, succeeded second")
    }
}
