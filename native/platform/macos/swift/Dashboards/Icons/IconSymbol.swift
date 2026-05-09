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
import SwiftUI

/// Maps the cross-platform `Icon` proto enum to the SF Symbol the
/// macOS renderer draws. Server-side widget templates emit numeric
/// `Icon` values via `IconConfig.fallback_icon`, `RowAction.icon`,
/// and the per-row `icon` synthesized by `QueryDashboardData`; the
/// renderer resolves each value through this extension.
///
/// The switch is intentionally exhaustive — Swift's compiler refuses
/// to build the app if a new proto `Icon` value isn't covered here,
/// so every cross-platform icon contract has a hard local checkpoint
/// in addition to the `lint-icon-maps` Go program that runs in CI.
extension Pivox_Api_V1_Icon {
    /// SF Symbol name for this icon. Returns the empty string for
    /// `unspecified` and the wire-unknown `UNRECOGNIZED`; callers
    /// fall back to a context-appropriate default (typically
    /// `Image(systemName: "questionmark.circle")` or hide the icon
    /// slot).
    public var sfSymbol: String {
        switch self {
        case .unspecified: return ""

        // File / content types (1xxx).
        case .document: return "doc"
        case .folder: return "folder"
        case .photo: return "photo"
        case .video: return "video"
        case .audio: return "music.note"
        case .code: return "chevron.left.forwardslash.chevron.right"
        case .archive: return "archivebox"

        // Identity (2xxx).
        case .person: return "person"
        case .people: return "person.2"
        case .organization: return "building.2"

        // Actions (3xxx).
        case .share: return "square.and.arrow.up"
        case .trash: return "trash"
        case .tag: return "tag"
        case .pin: return "pin"
        case .star: return "star"
        case .download: return "arrow.down.circle"
        case .upload: return "arrow.up.circle"
        case .copy: return "doc.on.doc"
        case .edit: return "pencil"

        // Status (4xxx).
        case .check: return "checkmark"
        case .xMark: return "xmark"
        case .warning: return "exclamationmark.triangle"
        case .info: return "info.circle"
        case .lock: return "lock"
        case .unlocked: return "lock.open"

        // Navigation (5xxx).
        case .ellipsis: return "ellipsis"
        case .plus: return "plus"
        case .minus: return "minus"
        case .settings: return "gearshape"
        case .search: return "magnifyingglass"

        case .UNRECOGNIZED: return ""
        }
    }

    /// SwiftUI `Image` for this icon. Convenience over `sfSymbol`
    /// for direct use inside view bodies. Falls back to a
    /// `questionmark.circle` placeholder for `unspecified` /
    /// `UNRECOGNIZED` so a missing icon shows up visibly in
    /// development rather than vanishing silently.
    public var image: Image {
        let name = sfSymbol
        if name.isEmpty {
            return Image(systemName: "questionmark.circle")
        }
        return Image(systemName: name)
    }
}
