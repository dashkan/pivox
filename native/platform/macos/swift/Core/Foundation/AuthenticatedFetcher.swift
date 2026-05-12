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
import Foundation

/// Outcome of an authenticated image fetch. The fetcher distills HTTP
/// status codes + decode results into three categorical signals so
/// `AuthenticatedImageLoader`'s state machine can branch without
/// re-parsing HTTP errors at every call site.
enum AuthenticatedFetchOutcome: Equatable, Sendable {
    /// 2xx + decodable image bytes. The associated `NSImage` is
    /// what the loader hands to SwiftUI's `Image(nsImage:)`.
    case ok(NSImage)
    /// 401 — the bearer token was rejected. Caller decides whether
    /// to invalidate-then-retry (the loader does this exactly once)
    /// or surface as auth-failed.
    case unauthorized
    /// Anything else: 4xx other than 401, 5xx, network failure,
    /// non-decodable bytes. The loader treats this as a terminal
    /// failure for the current `(url, token)` pair; the row's
    /// fallback `iconField` renders.
    case failed
}

/// Abstracts the URL-fetch surface so `AuthenticatedImageLoader`
/// can be exercised with a stub in tests. Production wires
/// `URLSessionAuthenticatedFetcher`; tests pass a stub that returns
/// a canned `AuthenticatedFetchOutcome` per call.
protocol AuthenticatedFetcher: Sendable {
    func fetch(url: URL, token: String) async -> AuthenticatedFetchOutcome
}

/// `URLSession`-backed fetcher. Attaches `Authorization: Bearer
/// <token>` and decodes the response body as `NSImage` on 2xx.
/// Default-config `URLSession.shared` matches the project's "no
/// custom URLSession just for this view" anti-footgun — the auth
/// header is per-request, not per-session.
struct URLSessionAuthenticatedFetcher: AuthenticatedFetcher {
    let session: URLSession

    init(session: URLSession = .shared) {
        self.session = session
    }

    func fetch(url: URL, token: String) async -> AuthenticatedFetchOutcome {
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")

        do {
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                // Non-HTTP response (e.g., file://). The agent always
                // serves over HTTP/HTTPS, so this is a wiring failure
                // — surface as .failed.
                return .failed
            }
            switch http.statusCode {
            case 200...299:
                guard let image = NSImage(data: data) else {
                    // Bytes came back but can't be decoded as an
                    // image — corrupt rendition, wrong content-type,
                    // or a placeholder text/html error page from a
                    // misconfigured upstream. Surface as .failed so
                    // the row's iconField fallback fires.
                    return .failed
                }
                return .ok(image)
            case 401:
                return .unauthorized
            default:
                // Anything else (403, 404, 5xx, …). Auth gate fired
                // a non-401 means the token was valid but the path
                // didn't match a session pattern — treat as
                // permanent-for-this-token; the loader's retry path
                // wouldn't help. Same as a network failure: render
                // the fallback icon.
                return .failed
            }
        } catch {
            // URL load timeout, DNS, TLS, peer reset. The loader
            // distinguishes these from .unauthorized (which is the
            // single retry-eligible signal) by branching on the
            // outcome, not by inspecting the error type here.
            return .failed
        }
    }
}
