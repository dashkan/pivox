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

import AppKit
import XCTest

@testable import Pivox

/// Tests for `AuthenticatedImageLoader` — the testable load-state
/// owner extracted from `AuthenticatedAsyncImage`. The View itself is
/// a thin switch over `loader.phase`; behavioral coverage lives here.
/// The View's render outputs are covered by SwiftUI Previews in
/// `AuthenticatedAsyncImage.swift`.
///
/// Coverage matrix maps to the 6c.4 dispatch's required cases:
///   - 200 → .loaded
///   - 401 → invalidate-and-retry → second 401 → .unauthorized
///     (single retry, no infinite loop)
///   - 401 → invalidate-and-retry → second 200 → .loaded
///   - non-401 failure → .failed (no retry)
///   - tokenProvider throws → .failed
///   - URL change re-fires load (covered by calling load() twice
///     with different urls and asserting fresh outcomes)
@MainActor
final class AuthenticatedAsyncImageTests: XCTestCase {

    // MARK: - 200 path

    func testLoad_OK_TransitionsToLoaded() async {
        let fetcher = StubFetcher(outcomes: [.ok(syntheticImage())])
        let tokens = StubTokens(tokens: ["t1"])
        let loader = AuthenticatedImageLoader(
            fetcher: fetcher,
            tokenProvider: tokens.provide,
            invalidateToken: tokens.invalidate
        )

        await loader.load(url: URL(string: "https://example.com/a.webp")!, orgID: "org-a")

        guard case .loaded = loader.phase else {
            XCTFail("expected .loaded, got \(loader.phase)")
            return
        }
        XCTAssertEqual(fetcher.fetchCount, 1, "single fetch on the happy path")
        XCTAssertEqual(tokens.invalidationCalls, 0, "happy path must not invalidate")
    }

    // MARK: - 401 + retry paths

    func testLoad_401ThenOK_InvalidatesAndRetries_TransitionsToLoaded() async {
        let fetcher = StubFetcher(outcomes: [.unauthorized, .ok(syntheticImage())])
        let tokens = StubTokens(tokens: ["stale-token", "fresh-token"])
        let loader = AuthenticatedImageLoader(
            fetcher: fetcher,
            tokenProvider: tokens.provide,
            invalidateToken: tokens.invalidate
        )

        await loader.load(url: URL(string: "https://example.com/a.webp")!, orgID: "org-a")

        guard case .loaded = loader.phase else {
            XCTFail("expected .loaded after invalidate-and-retry, got \(loader.phase)")
            return
        }
        XCTAssertEqual(fetcher.fetchCount, 2, "first 401 must trigger exactly one retry")
        XCTAssertEqual(tokens.invalidationCalls, 1, "exactly one invalidate between the two fetches")
        XCTAssertEqual(fetcher.tokensSeen, ["stale-token", "fresh-token"],
            "second fetch must use the post-invalidate token")
    }

    func testLoad_401Then401_NoInfiniteLoop_TransitionsToUnauthorized() async {
        // Two consecutive 401s. The loader's contract is "single
        // retry"; the second 401 transitions to .unauthorized
        // rather than another retry. This is the load-bearing
        // anti-infinite-loop test.
        let fetcher = StubFetcher(outcomes: [.unauthorized, .unauthorized])
        let tokens = StubTokens(tokens: ["t1", "t2"])
        let loader = AuthenticatedImageLoader(
            fetcher: fetcher,
            tokenProvider: tokens.provide,
            invalidateToken: tokens.invalidate
        )

        await loader.load(url: URL(string: "https://example.com/a.webp")!, orgID: "org-a")

        XCTAssertEqual(loader.phase, .unauthorized,
            "second 401 must surface as .unauthorized — no third fetch")
        XCTAssertEqual(fetcher.fetchCount, 2, "exactly two fetches: original + single retry")
    }

    // MARK: - non-401 failures

    func testLoad_FailedFromFetcher_TransitionsToFailed_NoRetry() async {
        // Non-401 (network, 5xx, undecodable bytes) is terminal —
        // a fresh token wouldn't help, so no retry.
        let fetcher = StubFetcher(outcomes: [.failed])
        let tokens = StubTokens(tokens: ["t1"])
        let loader = AuthenticatedImageLoader(
            fetcher: fetcher,
            tokenProvider: tokens.provide,
            invalidateToken: tokens.invalidate
        )

        await loader.load(url: URL(string: "https://example.com/a.webp")!, orgID: "org-a")

        XCTAssertEqual(loader.phase, .failed)
        XCTAssertEqual(fetcher.fetchCount, 1, "non-401 failure must NOT retry")
        XCTAssertEqual(tokens.invalidationCalls, 0, "no invalidate on non-401 failure")
    }

    // MARK: - token provider failure

    func testLoad_TokenProviderThrows_TransitionsToFailed() async {
        let fetcher = StubFetcher(outcomes: [])
        let tokens = StubTokens(tokens: [], throwOnNextProvide: true)
        let loader = AuthenticatedImageLoader(
            fetcher: fetcher,
            tokenProvider: tokens.provide,
            invalidateToken: tokens.invalidate
        )

        await loader.load(url: URL(string: "https://example.com/a.webp")!, orgID: "org-a")

        XCTAssertEqual(loader.phase, .failed,
            "tokenProvider failures land as .failed — same UX as a network error")
        XCTAssertEqual(fetcher.fetchCount, 0, "no fetch attempted when token mint failed")
    }

    func testLoad_TokenProviderThrowsOnRetry_TransitionsToFailed() async {
        // First mint succeeds → fetch returns 401 → invalidate →
        // second mint throws. The loader must surface .failed (NOT
        // .unauthorized) — the failure mode is mint-failed, not
        // auth-rejected.
        let fetcher = StubFetcher(outcomes: [.unauthorized])
        let tokens = StubTokens(tokens: ["t1"], throwOnNthProvide: 2)
        let loader = AuthenticatedImageLoader(
            fetcher: fetcher,
            tokenProvider: tokens.provide,
            invalidateToken: tokens.invalidate
        )

        await loader.load(url: URL(string: "https://example.com/a.webp")!, orgID: "org-a")

        XCTAssertEqual(loader.phase, .failed)
        XCTAssertEqual(fetcher.fetchCount, 1, "second fetch never fires when re-mint throws")
        XCTAssertEqual(tokens.invalidationCalls, 1, "invalidate ran before the failed re-mint")
    }

    // MARK: - URL change re-fires

    func testLoad_DifferentURL_RunsFreshFetch() async {
        // Pin the per-URL freshness contract that .task(id: url)
        // relies on: a second load() call with a different URL
        // triggers a fresh fetch (not cached), and phase resets to
        // .loading at the start of each call so the previous URL's
        // image doesn't bleed through under scroll recycling.
        let fetcher = StubFetcher(outcomes: [.ok(syntheticImage()), .ok(syntheticImage())])
        let tokens = StubTokens(tokens: ["t1", "t2"])
        let loader = AuthenticatedImageLoader(
            fetcher: fetcher,
            tokenProvider: tokens.provide,
            invalidateToken: tokens.invalidate
        )

        await loader.load(url: URL(string: "https://example.com/a.webp")!, orgID: "org-a")
        guard case .loaded = loader.phase else {
            XCTFail("expected .loaded after first load")
            return
        }
        await loader.load(url: URL(string: "https://example.com/b.webp")!, orgID: "org-a")
        guard case .loaded = loader.phase else {
            XCTFail("expected .loaded after second load")
            return
        }

        XCTAssertEqual(fetcher.fetchCount, 2, "second URL must trigger a fresh fetch")
        XCTAssertEqual(fetcher.urlsSeen.count, 2, "fetcher saw two distinct URLs")
        XCTAssertEqual(fetcher.urlsSeen[0].path, "/a.webp")
        XCTAssertEqual(fetcher.urlsSeen[1].path, "/b.webp")
    }

    // MARK: - Test helpers

    /// Build a synthetic NSImage that NSImage(data:) round-trips —
    /// 256x160 solid color, hand-rendered.
    private func syntheticImage() -> NSImage {
        let image = NSImage(size: NSSize(width: 256, height: 160))
        image.lockFocus()
        NSColor.systemTeal.setFill()
        NSRect(x: 0, y: 0, width: 256, height: 160).fill()
        image.unlockFocus()
        return image
    }
}

// MARK: - Test stubs

/// Records every fetch call + the token presented; returns canned
/// outcomes from the supplied list in order. `@MainActor` isolation
/// makes the mutable state (`outcomes`, `fetchCount`, etc.) safe
/// without `MainActor.run` hops or `@unchecked Sendable`: every
/// caller into `fetch(...)` already runs on MainActor (the loader
/// is `@MainActor`-isolated; tests dispatch from `@MainActor`-
/// isolated XCTestCase methods).
@MainActor
private final class StubFetcher: AuthenticatedFetcher {
    private var outcomes: [AuthenticatedFetchOutcome]
    private(set) var fetchCount: Int = 0
    private(set) var tokensSeen: [String] = []
    private(set) var urlsSeen: [URL] = []

    init(outcomes: [AuthenticatedFetchOutcome]) {
        self.outcomes = outcomes
    }

    func fetch(url: URL, token: String) async -> AuthenticatedFetchOutcome {
        fetchCount += 1
        tokensSeen.append(token)
        urlsSeen.append(url)
        guard !outcomes.isEmpty else { return .failed }
        return outcomes.removeFirst()
    }
}

/// Records every tokenProvider + invalidateToken call; returns
/// tokens from the supplied list in order. Optionally throws on the
/// first provide() (for token-mint-failed test) or on the Nth (for
/// retry-mint-failed test).
@MainActor
private final class StubTokens {
    private var tokens: [String]
    private var provideCount = 0
    private(set) var invalidationCalls = 0
    private let throwOnNextProvide: Bool
    private let throwOnNthProvide: Int?

    struct StubTokenError: Error {}

    init(
        tokens: [String],
        throwOnNextProvide: Bool = false,
        throwOnNthProvide: Int? = nil
    ) {
        self.tokens = tokens
        self.throwOnNextProvide = throwOnNextProvide
        self.throwOnNthProvide = throwOnNthProvide
    }

    func provide(_ orgID: String) async throws -> String {
        provideCount += 1
        if throwOnNextProvide && provideCount == 1 { throw StubTokenError() }
        if let n = throwOnNthProvide, provideCount == n { throw StubTokenError() }
        guard !tokens.isEmpty else { throw StubTokenError() }
        return tokens.removeFirst()
    }

    func invalidate(_ orgID: String) async {
        invalidationCalls += 1
    }
}
