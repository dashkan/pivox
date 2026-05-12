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
import Observation
import SwiftUI

/// Closure shape for retrieving a bearer JWT for the given org. The
/// production wiring delegates to `StorageService.shared.token`, but
/// `AuthenticatedAsyncImage` does not import `StorageService` — the
/// closure is the layer-rule contract (per native/CLAUDE.md: Core/
/// must not depend on features).
///
/// `@MainActor`-isolated, not `@Sendable`: every call site is a
/// MainActor view (DashboardView → CollectionWidgetView → RowIconView
/// → AuthenticatedImageLoader). No isolation boundary crosses, so
/// `@Sendable` would be ceremony — and would force test stubs that
/// hold mutable state into either an actor or value-type wrapper for
/// no real safety benefit.
typealias StorageTokenProvider = @MainActor (String) async throws -> String

/// Closure shape for invalidating a cached bearer token for a given
/// org. Called by the loader's single-retry path after a 401 — drops
/// the cached entry so the next `tokenProvider` call re-mints. No-op
/// default in the test seam; production wires
/// `StorageService.shared.invalidate`.
typealias StorageTokenInvalidator = @MainActor (String) async -> Void

/// Phases of the authenticated image fetch — drives both the SwiftUI
/// `body`'s switch and the testable `AuthenticatedImageLoader`'s
/// observable state.
enum AuthenticatedImagePhase: Equatable {
    /// Fetch in flight (or hasn't started yet). Renders the
    /// caller-supplied `placeholder()`.
    case loading

    /// 2xx + decodable bytes. Renders the caller-supplied
    /// `content(Image)`.
    case loaded(Image)

    /// 401 on the second try (after a single invalidate-and-retry).
    /// Renders the caller-supplied `placeholder()` — RowIconView's
    /// wiring picks the iconField fallback as the placeholder so
    /// auth failure looks the same as no-thumbnail-URL.
    case unauthorized

    /// Anything else: non-401 HTTP error, network failure,
    /// non-decodable bytes, token-mint failure. Same render as
    /// `.unauthorized` — the failure mode matters to ops/triage,
    /// not to the row's UI.
    case failed

    static func == (lhs: AuthenticatedImagePhase, rhs: AuthenticatedImagePhase) -> Bool {
        switch (lhs, rhs) {
        case (.loading, .loading), (.unauthorized, .unauthorized), (.failed, .failed):
            return true
        case (.loaded, .loaded):
            // Image instances aren't deeply comparable; equality on
            // .loaded means "both are .loaded with SOME image" —
            // sufficient for the loader-state tests we ship today.
            // If a test ever needs to assert pixel identity, capture
            // the NSImage on the loader directly via a debug
            // accessor (not added speculatively).
            return true
        default:
            return false
        }
    }
}

/// Testable load-state owner for `AuthenticatedAsyncImage`. Extracted
/// from the View so unit tests can drive the state machine without
/// rendering SwiftUI: the View becomes a thin switch over `phase`,
/// and the tests target this class directly.
///
/// Single-retry-on-401 contract:
///
///   1. Fetch `(url, token-from-tokenProvider)` →
///      - `.ok(image)` → `phase = .loaded(image)`. Done.
///      - `.unauthorized` → invalidate the cached token, mint a
///        fresh one, fetch again. If the second fetch is also
///        `.unauthorized`, `phase = .unauthorized`. No further
///        retries — preventing an infinite-loop on a path that's
///        permanently denied (e.g., asset belongs to a different
///        org).
///      - `.failed` → `phase = .failed`. No retry — the failure isn't
///        auth-related, so a fresh token wouldn't help.
///   2. If the `tokenProvider` itself throws, `phase = .failed`. The
///      caller's placeholder renders.
@Observable
@MainActor
final class AuthenticatedImageLoader {
    private(set) var phase: AuthenticatedImagePhase = .loading

    private let fetcher: AuthenticatedFetcher
    private let tokenProvider: StorageTokenProvider
    private let invalidateToken: StorageTokenInvalidator

    init(
        fetcher: AuthenticatedFetcher = URLSessionAuthenticatedFetcher(),
        tokenProvider: @escaping StorageTokenProvider,
        invalidateToken: @escaping StorageTokenInvalidator = { _ in }
    ) {
        self.fetcher = fetcher
        self.tokenProvider = tokenProvider
        self.invalidateToken = invalidateToken
    }

    /// Drive a fresh fetch for `(url, orgID)`. Resets `phase` to
    /// `.loading` first so a re-mounted view (URL change, scroll
    /// recycling) doesn't briefly show the previous row's image.
    ///
    /// Cancellation discipline: every async hop is followed by a
    /// `Task.isCancelled` check before assigning `phase`. SwiftUI's
    /// `.task(id: url)` cancels the in-flight load when the URL
    /// changes — but `URLSession.data(for:)` cancellation only takes
    /// effect at the next suspension point, so the post-await body
    /// can still run on the cancelled task. Without these guards a
    /// late-completing fetch from URL-A could overwrite the phase
    /// after URL-B's load has reset it to `.loading`, painting A's
    /// image into B's row during scroll recycling.
    func load(url: URL, orgID: String) async {
        phase = .loading
        let initialToken: String
        do {
            initialToken = try await tokenProvider(orgID)
        } catch {
            if Task.isCancelled { return }
            phase = .failed
            return
        }
        if Task.isCancelled { return }
        let first = await fetcher.fetch(url: url, token: initialToken)
        if Task.isCancelled { return }
        switch first {
        case .ok(let image):
            phase = .loaded(Image(nsImage: image))
        case .failed:
            phase = .failed
        case .unauthorized:
            // Single retry: invalidate the cached token, mint a
            // fresh one, refetch. If the second attempt also 401s,
            // it's a permanent denial for this (user, org, path) —
            // surface .unauthorized and let the placeholder render.
            await invalidateToken(orgID)
            if Task.isCancelled { return }
            let renewedToken: String
            do {
                renewedToken = try await tokenProvider(orgID)
            } catch {
                if Task.isCancelled { return }
                phase = .failed
                return
            }
            if Task.isCancelled { return }
            let second = await fetcher.fetch(url: url, token: renewedToken)
            if Task.isCancelled { return }
            switch second {
            case .ok(let image):
                phase = .loaded(Image(nsImage: image))
            case .unauthorized:
                phase = .unauthorized
            case .failed:
                phase = .failed
            }
        }
    }
}

/// SwiftUI view that fetches an HTTPS resource with `Authorization:
/// Bearer <jwt>` and renders one of four phases. Drop-in conceptual
/// replacement for `AsyncImage` for endpoints that need bearer auth —
/// the storage agent's `/files/` surface is the v1 consumer.
///
/// Usage:
///
///     AuthenticatedAsyncImage(
///         url: URL(string: row.thumbnailURL)!,
///         orgID: orgSlug,
///         tokenProvider: tokenProvider,
///         invalidateToken: invalidateToken,
///         content: { image in
///             image.resizable().aspectRatio(contentMode: .fill)
///         },
///         placeholder: {
///             iconImage(fallbackIcon)
///         }
///     )
///
/// `.task(id: url)` re-runs `load()` when the URL changes — handles
/// row replacement during scroll recycling. The phase is
/// `.loading` between fires so a stale image from the previous row
/// doesn't bleed through.
///
/// Layer-rule note: this view is in `Core/Foundation/` and does NOT
/// import `StorageService` (or any feature module). The
/// `tokenProvider` + `invalidateToken` closures are the layer-clean
/// contract — wiring at the use site (Dashboards/Renderer/RowIconView
/// today; ImageEditor / Studio surfaces tomorrow) decides which
/// service implements them.
struct AuthenticatedAsyncImage<Content: View, Placeholder: View>: View {
    let url: URL
    let orgID: String
    let content: (Image) -> Content
    let placeholder: () -> Placeholder

    @State private var loader: AuthenticatedImageLoader

    init(
        url: URL,
        orgID: String,
        tokenProvider: @escaping StorageTokenProvider,
        invalidateToken: @escaping StorageTokenInvalidator = { _ in },
        fetcher: AuthenticatedFetcher = URLSessionAuthenticatedFetcher(),
        @ViewBuilder content: @escaping (Image) -> Content,
        @ViewBuilder placeholder: @escaping () -> Placeholder
    ) {
        self.url = url
        self.orgID = orgID
        self.content = content
        self.placeholder = placeholder
        self._loader = State(
            wrappedValue: AuthenticatedImageLoader(
                fetcher: fetcher,
                tokenProvider: tokenProvider,
                invalidateToken: invalidateToken
            )
        )
    }

    var body: some View {
        Group {
            switch loader.phase {
            case .loading:
                placeholder()
            case .loaded(let image):
                content(image)
            case .unauthorized, .failed:
                // The two non-loaded terminal states share a render
                // because the row's UI doesn't differentiate them —
                // both fall through to the iconField fallback.
                // Operations triage distinguishes via the agent-side
                // log (#102 follow-up).
                placeholder()
            }
        }
        .task(id: url) {
            await loader.load(url: url, orgID: orgID)
        }
    }
}

// MARK: - Preview-only stub fetchers
//
// File-scope rather than inline-in-#Preview because the #Preview
// macro's PreviewMacroBodyBuilder rejects local-type declarations
// and explicit `return` statements (SwiftUI 6 / macOS 15+ macro
// constraint).
//
// NOT under `#if DEBUG`: the `#Preview` macro itself is NOT a
// debug-only macro — it expands to code visible to the compiler in
// Release builds too, even though Previews don't render in
// Release. So the types these previews reference must be available
// at Release-build time. Tagging `private` confines them to this
// file; the cost of shipping ~30 lines of unused code in Release is
// negligible.

private struct PreviewLoadedFetcher: AuthenticatedFetcher {
    func fetch(url: URL, token: String) async -> AuthenticatedFetchOutcome {
        let image = NSImage(size: NSSize(width: 256, height: 160))
        image.lockFocus()
        NSColor.systemTeal.setFill()
        NSRect(x: 0, y: 0, width: 256, height: 160).fill()
        image.unlockFocus()
        return .ok(image)
    }
}

private struct PreviewUnauthorizedFetcher: AuthenticatedFetcher {
    func fetch(url: URL, token: String) async -> AuthenticatedFetchOutcome {
        .unauthorized
    }
}

private struct PreviewFailedFetcher: AuthenticatedFetcher {
    func fetch(url: URL, token: String) async -> AuthenticatedFetchOutcome {
        .failed
    }
}

// MARK: - Previews

#Preview("Loading") {
    AuthenticatedAsyncImage(
        url: URL(string: "https://example.com/thumb.webp")!,
        orgID: "preview-org",
        // Token that never resolves keeps the loader in .loading
        // for the lifetime of the preview.
        tokenProvider: { _ in
            try await Task.sleep(nanoseconds: 60_000_000_000)
            return "never-returned"
        },
        content: { image in image.resizable().aspectRatio(contentMode: .fill) },
        placeholder: {
            ZStack {
                RoundedRectangle(cornerRadius: 8).fill(Color.secondary.opacity(0.15))
                ProgressView().controlSize(.small)
            }
            .frame(width: 160, height: 100)
        }
    )
    .padding()
}

#Preview("Loaded") {
    AuthenticatedAsyncImage(
        url: URL(string: "https://example.com/thumb.webp")!,
        orgID: "preview-org",
        tokenProvider: { _ in "stub-token" },
        fetcher: PreviewLoadedFetcher(),
        content: { image in image.resizable().aspectRatio(contentMode: .fill) },
        placeholder: { Color.secondary.opacity(0.15) }
    )
    .frame(width: 160, height: 100)
    .clipShape(RoundedRectangle(cornerRadius: 8))
    .padding()
}

#Preview("Unauthorized — placeholder renders") {
    AuthenticatedAsyncImage(
        url: URL(string: "https://example.com/thumb.webp")!,
        orgID: "preview-org",
        tokenProvider: { _ in "stub-token" },
        fetcher: PreviewUnauthorizedFetcher(),
        content: { image in image.resizable() },
        placeholder: {
            ZStack {
                RoundedRectangle(cornerRadius: 8).fill(Color.secondary.opacity(0.15))
                Image(systemName: "lock").foregroundStyle(.secondary)
            }
            .frame(width: 160, height: 100)
        }
    )
    .padding()
}

#Preview("Failed — placeholder renders") {
    AuthenticatedAsyncImage(
        url: URL(string: "https://example.com/thumb.webp")!,
        orgID: "preview-org",
        tokenProvider: { _ in "stub-token" },
        fetcher: PreviewFailedFetcher(),
        content: { image in image.resizable() },
        placeholder: {
            ZStack {
                RoundedRectangle(cornerRadius: 8).fill(Color.secondary.opacity(0.15))
                Image(systemName: "questionmark.square").foregroundStyle(.secondary)
            }
            .frame(width: 160, height: 100)
        }
    )
    .padding()
}
