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

/// `URLSessionAuthenticatedFetcher` tests using the standard
/// URLProtocol-stub pattern: register a custom protocol on a
/// dedicated `URLSession.configuration` and let it intercept every
/// request the fetcher makes. This lets us assert on the outgoing
/// `URLRequest` and shape the canned response without binding a real
/// network socket.
@MainActor
final class AuthenticatedFetcherTests: XCTestCase {

    override func setUp() {
        super.setUp()
        StubURLProtocol.reset()
    }

    override func tearDown() {
        StubURLProtocol.reset()
        super.tearDown()
    }

    // MARK: - Authorization header attachment

    func testFetch_AttachesAuthorizationHeader() async throws {
        StubURLProtocol.responder = { request in
            // Capture the header for later assertion; respond with a
            // 1x1 PNG so the .ok branch exercises a decodable image.
            (200, request.value(forHTTPHeaderField: "Authorization") ?? "<missing>",
                Self.tinyPNG)
        }
        let fetcher = URLSessionAuthenticatedFetcher(session: stubSession())

        let outcome = await fetcher.fetch(
            url: URL(string: "https://example.com/thumb.webp")!,
            token: "header.payload.signature"
        )

        guard case .ok = outcome else {
            XCTFail("expected .ok with bearer-attached request, got \(outcome)")
            return
        }
        XCTAssertEqual(StubURLProtocol.lastCapturedNote, "Bearer header.payload.signature",
            "fetcher must attach Authorization: Bearer <token> on every request")
    }

    // MARK: - Status-code branching

    func testFetch_200WithImageBytes_ReturnsOK() async throws {
        StubURLProtocol.responder = { _ in (200, "", Self.tinyPNG) }
        let fetcher = URLSessionAuthenticatedFetcher(session: stubSession())

        let outcome = await fetcher.fetch(
            url: URL(string: "https://example.com/thumb.webp")!,
            token: "any-token"
        )

        guard case .ok(let image) = outcome else {
            XCTFail("expected .ok, got \(outcome)")
            return
        }
        XCTAssertGreaterThan(image.size.width, 0,
            "decoded NSImage must have non-zero size")
    }

    func testFetch_200WithUndecodableBytes_ReturnsFailed() async throws {
        // 200 OK but the body isn't a valid image — a misconfigured
        // upstream serving HTML for a missing object, or corruption.
        StubURLProtocol.responder = { _ in
            (200, "", Data("not an image".utf8))
        }
        let fetcher = URLSessionAuthenticatedFetcher(session: stubSession())

        let outcome = await fetcher.fetch(
            url: URL(string: "https://example.com/thumb.webp")!,
            token: "any-token"
        )

        XCTAssertEqual(outcome, .failed,
            "non-decodable 200 body must surface as .failed, not .ok")
    }

    func testFetch_401_ReturnsUnauthorized() async throws {
        StubURLProtocol.responder = { _ in (401, "", Data("Unauthorized".utf8)) }
        let fetcher = URLSessionAuthenticatedFetcher(session: stubSession())

        let outcome = await fetcher.fetch(
            url: URL(string: "https://example.com/thumb.webp")!,
            token: "expired-or-stale-token"
        )

        XCTAssertEqual(outcome, .unauthorized,
            "401 must surface as .unauthorized so the loader can decide " +
            "whether to invalidate-and-retry")
    }

    func testFetch_NonAuthErrorStatuses_ReturnFailed() async throws {
        for status in [403, 404, 500, 502, 503] {
            StubURLProtocol.responder = { _ in (status, "", Data()) }
            let fetcher = URLSessionAuthenticatedFetcher(session: stubSession())

            let outcome = await fetcher.fetch(
                url: URL(string: "https://example.com/thumb.webp")!,
                token: "any-token"
            )

            XCTAssertEqual(outcome, .failed,
                "non-401 non-2xx status \(status) must surface as .failed " +
                "(retry wouldn't help; the loader's auth-retry path is " +
                "401-specific)")
        }
    }

    // MARK: - Network error

    func testFetch_NetworkError_ReturnsFailed() async throws {
        struct StubError: Error {}
        StubURLProtocol.errorResponder = { _ in StubError() }
        let fetcher = URLSessionAuthenticatedFetcher(session: stubSession())

        let outcome = await fetcher.fetch(
            url: URL(string: "https://example.com/thumb.webp")!,
            token: "any-token"
        )

        XCTAssertEqual(outcome, .failed,
            "network/transport errors must surface as .failed (same UX " +
            "as a non-401 HTTP error: render the iconField fallback)")
    }

    // MARK: - URLSession stub plumbing

    /// Build a URLSession that uses our stub protocol. The
    /// `protocolClasses` MUST be set on the per-test configuration,
    /// not URLSession.shared, otherwise every other test in the
    /// process inherits the stub.
    private func stubSession() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubURLProtocol.self]
        return URLSession(configuration: config)
    }

    /// Smallest valid PNG: 1x1 transparent pixel. Sufficient for
    /// `NSImage(data:)` to decode and produce a non-zero-size image.
    /// Hex-decoded so the test file is self-contained — no fixture
    /// bundle path to maintain.
    private static let tinyPNG: Data = {
        let bytes: [UInt8] = [
            0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
            0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
            0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
            0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
            0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
            0x54, 0x78, 0x9C, 0x62, 0x00, 0x01, 0x00, 0x00,
            0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
            0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
            0x42, 0x60, 0x82,
        ]
        return Data(bytes)
    }()
}

// MARK: - StubURLProtocol

/// Custom URLProtocol that intercepts requests on a per-session
/// stub session. Tests set `responder` (status + capture-note + body
/// triple) for the happy path or `errorResponder` to simulate a
/// transport failure.
final class StubURLProtocol: URLProtocol, @unchecked Sendable {
    typealias Responder = (URLRequest) -> (Int, String, Data)
    typealias ErrorResponder = (URLRequest) -> Error

    static var responder: Responder?
    static var errorResponder: ErrorResponder?
    static var lastCapturedNote: String = ""

    static func reset() {
        responder = nil
        errorResponder = nil
        lastCapturedNote = ""
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        if let errorResponder = Self.errorResponder {
            client?.urlProtocol(self, didFailWithError: errorResponder(request))
            return
        }
        guard let responder = Self.responder else {
            client?.urlProtocol(self, didFailWithError: NSError(domain: "stub", code: -1))
            return
        }
        let (status, note, body) = responder(request)
        Self.lastCapturedNote = note
        let response = HTTPURLResponse(
            url: request.url!,
            statusCode: status,
            httpVersion: "HTTP/1.1",
            headerFields: [:]
        )!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
