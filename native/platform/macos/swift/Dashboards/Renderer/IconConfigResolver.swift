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

/// Resolves a row's leading icon according to the
/// `IconConfig` contract. The contract's resolution chain is:
///
///   1. `sourceField` — names a row column carrying a thumbnail URL.
///      A non-empty value is fetched by the renderer's image layer.
///   2. `iconField` — names a row column carrying the numeric `Icon`
///      enum value (server-side `QueryDashboardData` synthesizes
///      this from each Asset's `media_type` / `content_type`).
///   3. `initialsField` — names a row column whose value seeds an
///      initials-derived icon. Reserved for v1; not wired in this
///      renderer (no consumer).
///   4. `fallbackIcon` — static `Icon` enum value used when none of
///      the above produce a value.
///
/// `ResolvedIcon` is the rendered output: a thumbnail URL with a
/// fallback Icon for when the URL fails to load, or a semantic
/// Icon directly.
///
/// Why `.thumbnailURL` carries a fallback: the URL fetch can fail
/// (bad network, gateway requires auth that the renderer doesn't
/// have yet — see Phase 6c's StorageService), and falling back to
/// `IconConfig.fallbackIcon` would mask the per-row `iconField`
/// value the server went to the trouble of synthesizing. The
/// fallback here is the SAME value the resolver would have
/// returned if `sourceField` had been empty.
enum ResolvedIcon: Equatable {
    /// A thumbnail URL the caller should fetch, plus the icon to
    /// render if the fetch fails.
    case thumbnailURL(String, fallback: Pivox_Api_V1_Icon)

    /// A semantic icon (numeric `Icon` enum value resolved into the
    /// macOS SF Symbol via the extension shipped in Phase 6a).
    case icon(Pivox_Api_V1_Icon)
}

/// Pure resolution function. Pure-function shape (no side effects,
/// no I/O) so renderer tests can assert per-row icon resolution
/// against the IconConfig contract directly.
enum IconConfigResolver {
    /// Resolve the icon for `row` according to `config`. Returns
    /// `.thumbnailURL` if `sourceField` produces a non-empty string;
    /// otherwise `.icon` derived from `iconField` (numeric Icon enum
    /// value); otherwise `.icon` from `fallbackIcon`; ultimately
    /// `.icon(.unspecified)` if nothing resolves (the renderer maps
    /// that to `questionmark.circle` per Phase 6a's `Image`
    /// extension).
    static func resolve(
        row: Google_Protobuf_Struct,
        config: Pivox_Api_V1_IconConfig
    ) -> ResolvedIcon {
        if !config.sourceField.isEmpty {
            if case .stringValue(let url) = row.fields[config.sourceField]?.kind, !url.isEmpty {
                return .thumbnailURL(url, fallback: resolveIconChain(row: row, config: config))
            }
        }
        return .icon(resolveIconChain(row: row, config: config))
    }

    /// Resolve the icon-only path (skips `sourceField`). Used for
    /// the fallback that ships with `.thumbnailURL` so a failed
    /// image fetch falls back to the per-row `iconField` value
    /// rather than the static `fallbackIcon`. Also used as the
    /// terminal of the main `resolve` path.
    private static func resolveIconChain(
        row: Google_Protobuf_Struct,
        config: Pivox_Api_V1_IconConfig
    ) -> Pivox_Api_V1_Icon {
        if !config.iconField.isEmpty {
            if let value = row.fields[config.iconField]?.kind {
                if case .numberValue(let n) = value, n != 0 {
                    return iconFromNumber(Int(n))
                }
                // Defensive: server may emit numeric Icon values as
                // string-typed for forward-compat with non-numeric
                // codecs. Honor that path so a future backend can
                // switch encodings without breaking the renderer.
                if case .stringValue(let s) = value, let n = Int(s), n != 0 {
                    return iconFromNumber(n)
                }
            }
        }
        // initialsField intentionally unhandled in v1 — the locked
        // design declared it but no renderer surface needs it yet.
        if config.fallbackIcon != .unspecified {
            return config.fallbackIcon
        }
        return .unspecified
    }

    /// Lift an integer raw value back into the `Icon` enum. Unknown
    /// values map to `.unspecified` so the renderer's fallback path
    /// fires (visible `questionmark.circle` per Phase 6a) rather
    /// than propagating an `.UNRECOGNIZED(N)` that would render
    /// nothing.
    ///
    /// Note: swift-protobuf's `init(rawValue:)` returns
    /// `.UNRECOGNIZED(N)` for unknown raw values rather than nil,
    /// so the `?? .unspecified` shortcut alone wouldn't fire. The
    /// explicit UNRECOGNIZED check is the load-bearing line.
    private static func iconFromNumber(_ n: Int) -> Pivox_Api_V1_Icon {
        let icon = Pivox_Api_V1_Icon(rawValue: n) ?? .unspecified
        if case .UNRECOGNIZED = icon {
            return .unspecified
        }
        return icon
    }
}
