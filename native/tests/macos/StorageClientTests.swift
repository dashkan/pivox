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

/// Tests for `StorageClient.createSession`. The client is a thin
/// wrapper around the generated `Pivox_Storage_V1_StorageGateways`
/// gRPC client; tests inject a stub mint closure as the test seam
/// (matching the closure-injection pattern from
/// `DashboardViewModelTests`) so we exercise the request-shape and
/// response-mapping logic without binding a real gRPC channel.
///
/// Coverage:
///   - createSession composes a request with `parent =
///     "organizations/{orgID}"` and (when supplied) the right ttl
///     duration. Asserts the wire-message the client would send.
///   - createSession maps the gRPC response (token + expiry timestamp)
///     into the `SessionResult` model — token verbatim, expiry
///     converted from Google_Protobuf_Timestamp to Foundation Date.
///
/// Identity-token attachment lives one layer down in
/// `FirebaseAuthInterceptor`; testing it here would couple the
/// StorageClient test to the interceptor's internals. The dispatch
/// asks for "request shape (org parent, identity token attached)";
/// org parent is StorageClient's job and is asserted here, the
/// identity token is the interceptor's job and is covered by the
/// existing auth-interceptor tests.
@MainActor
final class StorageClientTests: XCTestCase {
    func testCreateSession_RequestShape_ParentOnly() async throws {
        var captured: Pivox_Storage_V1_CreateStorageSessionRequest?
        let stub: StorageClient.MintFn = { req in
            captured = req
            var resp = Pivox_Storage_V1_CreateStorageSessionResponse()
            resp.token = "stub-token"
            resp.expiry.seconds = Int64(Date().addingTimeInterval(3600).timeIntervalSince1970)
            return resp
        }
        let client = StorageClient(mintFn: stub)

        _ = try await client.createSession(forOrg: "meridian-broad")

        let req = try XCTUnwrap(captured)
        XCTAssertEqual(req.parent, "organizations/meridian-broad",
            "createSession must compose the AIP parent shape")
        XCTAssertFalse(req.hasTtl,
            "ttl unset — server should apply default (1h)")
    }

    func testCreateSession_RequestShape_WithExplicitTTL() async throws {
        var captured: Pivox_Storage_V1_CreateStorageSessionRequest?
        let stub: StorageClient.MintFn = { req in
            captured = req
            var resp = Pivox_Storage_V1_CreateStorageSessionResponse()
            resp.token = "stub-token"
            resp.expiry.seconds = Int64(Date().addingTimeInterval(7200).timeIntervalSince1970)
            return resp
        }
        let client = StorageClient(mintFn: stub)

        _ = try await client.createSession(forOrg: "meridian-broad", ttl: 7200)

        let req = try XCTUnwrap(captured)
        XCTAssertEqual(req.parent, "organizations/meridian-broad")
        XCTAssertTrue(req.hasTtl)
        XCTAssertEqual(req.ttl.seconds, 7200)
    }

    func testCreateSession_ResponseMapping() async throws {
        // Pin a specific expiry so the Date conversion is testable.
        let expiryUnix: Int64 = 1_715_212_800 // 2024-05-09T00:00:00Z
        let stub: StorageClient.MintFn = { _ in
            var resp = Pivox_Storage_V1_CreateStorageSessionResponse()
            resp.token = "header.payload.signature"
            resp.expiry.seconds = expiryUnix
            return resp
        }
        let client = StorageClient(mintFn: stub)

        let result = try await client.createSession(forOrg: "any-org")

        XCTAssertEqual(result.token, "header.payload.signature",
            "token field must round-trip verbatim from the response body")
        XCTAssertEqual(result.expiresAt,
            Date(timeIntervalSince1970: TimeInterval(expiryUnix)),
            "expiry Timestamp must convert to Foundation Date in seconds resolution")
    }

    func testCreateSession_PropagatesError() async throws {
        struct StubError: Error, Equatable {}
        let stub: StorageClient.MintFn = { _ in throw StubError() }
        let client = StorageClient(mintFn: stub)

        do {
            _ = try await client.createSession(forOrg: "any-org")
            XCTFail("expected createSession to propagate the underlying gRPC error")
        } catch is StubError {
            // expected — the wrapper doesn't swallow errors from the
            // gRPC layer; the StorageService caller decides what to
            // do with them (e.g., 401 → invalidate cache and re-mint).
        }
    }
}
