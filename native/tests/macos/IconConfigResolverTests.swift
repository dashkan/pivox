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

/// Tests for the `IconConfigResolver` — the per-row icon resolution
/// chain that turns an `IconConfig` + a `Google_Protobuf_Struct`
/// row into a `ResolvedIcon`. Pure-function shape; no UI; no I/O.
final class IconConfigResolverTests: XCTestCase {
    // MARK: - sourceField wins when present

    func testSourceFieldStringValueWins() {
        var config = Pivox_Api_V1_IconConfig()
        config.sourceField = "thumbnail_url"
        config.iconField = "icon"
        config.fallbackIcon = .document

        let row = makeRow([
            "thumbnail_url": .stringValue("https://gateway.example/thumb.jpg"),
            "icon": .numberValue(Double(Pivox_Api_V1_Icon.photo.rawValue)),
        ])

        let resolved = IconConfigResolver.resolve(row: row, config: config)
        // .thumbnailURL carries the URL plus the iconField-derived
        // fallback so a failed image load uses the right icon, not
        // the static fallbackIcon.
        XCTAssertEqual(
            resolved,
            .thumbnailURL("https://gateway.example/thumb.jpg", fallback: .photo)
        )
    }

    /// Pin the load-bearing contract: when sourceField produces a
    /// URL, the .thumbnailURL fallback is the iconField-derived
    /// value (NOT the static fallbackIcon). Without this, a failed
    /// thumbnail fetch would show `fallbackIcon` for every row,
    /// hiding the per-row icon the server synthesized.
    func testThumbnailURLCarriesIconFieldFallbackNotStatic() {
        var config = Pivox_Api_V1_IconConfig()
        config.sourceField = "thumbnail_url"
        config.iconField = "icon"
        config.fallbackIcon = .document

        let row = makeRow([
            "thumbnail_url": .stringValue("https://gateway.example/thumb.jpg"),
            "icon": .numberValue(Double(Pivox_Api_V1_Icon.video.rawValue)),
        ])

        if case .thumbnailURL(_, let fallback) = IconConfigResolver.resolve(row: row, config: config) {
            XCTAssertEqual(fallback, .video,
                "fallback must be the iconField-derived value, not config.fallbackIcon")
        } else {
            XCTFail("expected .thumbnailURL")
        }
    }

    /// When iconField is empty/missing AND sourceField produces a
    /// URL, the fallback resolves to `config.fallbackIcon` (the
    /// terminal of the icon chain).
    func testThumbnailURLFallbackUsesStaticFallbackWhenIconFieldMissing() {
        var config = Pivox_Api_V1_IconConfig()
        config.sourceField = "thumbnail_url"
        config.iconField = "icon"
        config.fallbackIcon = .document

        let row = makeRow([
            "thumbnail_url": .stringValue("https://gateway.example/thumb.jpg"),
            // no `icon` field
        ])

        if case .thumbnailURL(_, let fallback) = IconConfigResolver.resolve(row: row, config: config) {
            XCTAssertEqual(fallback, .document)
        } else {
            XCTFail("expected .thumbnailURL")
        }
    }

    func testSourceFieldEmptyStringFallsThrough() {
        var config = Pivox_Api_V1_IconConfig()
        config.sourceField = "thumbnail_url"
        config.iconField = "icon"
        config.fallbackIcon = .document

        // Empty thumbnail URL → next link in the chain (icon_field).
        let row = makeRow([
            "thumbnail_url": .stringValue(""),
            "icon": .numberValue(Double(Pivox_Api_V1_Icon.photo.rawValue)),
        ])

        let resolved = IconConfigResolver.resolve(row: row, config: config)
        XCTAssertEqual(resolved, .icon(.photo))
    }

    func testSourceFieldMissingFallsThrough() {
        var config = Pivox_Api_V1_IconConfig()
        config.sourceField = "thumbnail_url"
        config.iconField = "icon"
        config.fallbackIcon = .document

        let row = makeRow([
            "icon": .numberValue(Double(Pivox_Api_V1_Icon.video.rawValue)),
        ])

        let resolved = IconConfigResolver.resolve(row: row, config: config)
        XCTAssertEqual(resolved, .icon(.video))
    }

    // MARK: - iconField numeric vs stringified

    func testIconFieldNumberValue() {
        var config = Pivox_Api_V1_IconConfig()
        config.iconField = "icon"
        let row = makeRow([
            "icon": .numberValue(Double(Pivox_Api_V1_Icon.audio.rawValue)),
        ])
        XCTAssertEqual(
            IconConfigResolver.resolve(row: row, config: config),
            .icon(.audio)
        )
    }

    func testIconFieldStringifiedNumberValue() {
        var config = Pivox_Api_V1_IconConfig()
        config.iconField = "icon"
        let row = makeRow([
            "icon": .stringValue("\(Pivox_Api_V1_Icon.archive.rawValue)"),
        ])
        XCTAssertEqual(
            IconConfigResolver.resolve(row: row, config: config),
            .icon(.archive)
        )
    }

    func testIconFieldUnknownIntFallsBackToFallback() {
        var config = Pivox_Api_V1_IconConfig()
        config.iconField = "icon"
        config.fallbackIcon = .document
        let row = makeRow([
            // Unknown raw value — Pivox_Api_V1_Icon(rawValue:) returns
            // nil; the resolver maps that to .unspecified, which is
            // not the "fall through to fallback" path. The current
            // behavior surfaces .unspecified directly so the renderer
            // shows questionmark.circle (visible) rather than masking
            // the malformed value with a plausible-looking icon.
            "icon": .numberValue(99_999),
        ])
        XCTAssertEqual(
            IconConfigResolver.resolve(row: row, config: config),
            .icon(.unspecified)
        )
    }

    // MARK: - fallbackIcon

    func testFallbackIconWhenNothingElseMatches() {
        var config = Pivox_Api_V1_IconConfig()
        config.fallbackIcon = .folder

        let row = makeRow([:])

        XCTAssertEqual(
            IconConfigResolver.resolve(row: row, config: config),
            .icon(.folder)
        )
    }

    func testUnspecifiedWhenEverythingEmpty() {
        let config = Pivox_Api_V1_IconConfig()
        let row = makeRow([:])
        XCTAssertEqual(
            IconConfigResolver.resolve(row: row, config: config),
            .icon(.unspecified)
        )
    }

    // MARK: - chain ordering

    func testChainPrefersSourceOverIconOverFallback() {
        var config = Pivox_Api_V1_IconConfig()
        config.sourceField = "thumb"
        config.iconField = "icon"
        config.fallbackIcon = .document

        // Every link populated; sourceField must win.
        let everything = makeRow([
            "thumb": .stringValue("u"),
            "icon": .numberValue(Double(Pivox_Api_V1_Icon.video.rawValue)),
        ])
        XCTAssertEqual(
            IconConfigResolver.resolve(row: everything, config: config),
            .thumbnailURL("u", fallback: .video)
        )

        // sourceField empty; iconField wins over fallback.
        let onlyIcon = makeRow([
            "icon": .numberValue(Double(Pivox_Api_V1_Icon.video.rawValue)),
        ])
        XCTAssertEqual(
            IconConfigResolver.resolve(row: onlyIcon, config: config),
            .icon(.video)
        )

        // Both source + icon empty; fallback wins.
        let nothing = makeRow([:])
        XCTAssertEqual(
            IconConfigResolver.resolve(row: nothing, config: config),
            .icon(.document)
        )
    }

    // MARK: - helpers

    private func makeRow(_ fields: [String: Google_Protobuf_Value.OneOf_Kind]) -> Google_Protobuf_Struct {
        var s = Google_Protobuf_Struct()
        for (k, kind) in fields {
            var v = Google_Protobuf_Value()
            v.kind = kind
            s.fields[k] = v
        }
        return s
    }
}
