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
import SwiftUI

/// Leading-icon view for a single `CollectionWidget` row. Wraps the
/// pure `IconConfigResolver` with the SwiftUI rendering chain:
///
///   - `.thumbnailURL(...)` is shown via `AuthenticatedAsyncImage`,
///     which attaches `Authorization: Bearer <jwt>` to every
///     `URLSession` request through the closure-injected
///     `tokenProvider`. Auth-failed and net-failed both fall through
///     to the iconField fallback the resolver computed.
///   - `.icon(...)` resolves through Phase 6a's
///     `Pivox_Api_V1_Icon.image` extension.
///
/// Size scales with the supplied `IconConfig.size` bucket so a TABLE
/// row's leading-icon column reads tighter than a CARD's hero image.
///
/// Layer-rule note: this view passes `tokenProvider` /
/// `invalidateToken` as opaque closures to `AuthenticatedAsyncImage`
/// (a Core/Foundation primitive). The wiring at the use site
/// (DashboardView → CollectionWidgetView → here) decides which
/// service implements them — production wires
/// `StorageService.shared.token(forOrg:)`. Neither `RowIconView` nor
/// `AuthenticatedAsyncImage` imports `StorageService` directly.
struct RowIconView: View {
    let row: Google_Protobuf_Struct
    let config: Pivox_Api_V1_IconConfig
    let placement: Placement
    let orgID: String
    let tokenProvider: StorageTokenProvider
    let invalidateToken: StorageTokenInvalidator

    /// `Placement` switches the rendered point size between the
    /// table-row "leading icon" form (compact) and the card-face
    /// "thumbnail" form (large). The widget's `IconConfig.size`
    /// bucket is the per-template hint; placement is the per-mode
    /// override.
    enum Placement {
        case tableRow
        case cardThumbnail
    }

    init(
        row: Google_Protobuf_Struct,
        config: Pivox_Api_V1_IconConfig,
        placement: Placement,
        orgID: String,
        tokenProvider: @escaping StorageTokenProvider,
        invalidateToken: @escaping StorageTokenInvalidator = { _ in }
    ) {
        self.row = row
        self.config = config
        self.placement = placement
        self.orgID = orgID
        self.tokenProvider = tokenProvider
        self.invalidateToken = invalidateToken
    }

    var body: some View {
        let resolved = IconConfigResolver.resolve(row: row, config: config)
        switch resolved {
        case .thumbnailURL(let urlString, let fallback):
            if let url = URL(string: urlString) {
                AuthenticatedAsyncImage(
                    url: url,
                    orgID: orgID,
                    tokenProvider: tokenProvider,
                    invalidateToken: invalidateToken,
                    content: { image in
                        image.resizable().aspectRatio(contentMode: .fill)
                    },
                    placeholder: {
                        // Both .loading and the terminal failure
                        // states (.unauthorized / .failed) render
                        // this — for a table row, "loading" briefly
                        // showing the iconField is preferable to a
                        // spinner that flickers in and out per
                        // scroll cell. AuthenticatedAsyncImage's
                        // .task(id: url) re-fires for new URLs so
                        // the placeholder doesn't bleed across rows.
                        iconImage(fallback)
                    }
                )
                .frame(width: pointSize.width, height: pointSize.height)
                .clipShape(RoundedRectangle(cornerRadius: cornerRadius))
            } else {
                iconImage(fallback)
            }
        case .icon(let icon):
            iconImage(icon)
        }
    }

    @ViewBuilder
    private func iconImage(_ icon: Pivox_Api_V1_Icon) -> some View {
        ZStack {
            RoundedRectangle(cornerRadius: cornerRadius)
                .fill(Color.secondary.opacity(0.15))
            icon.image
                .font(.system(size: glyphPointSize, weight: .regular))
                .foregroundStyle(.secondary)
        }
        .frame(width: pointSize.width, height: pointSize.height)
        .accessibilityHidden(true)
    }

    private var pointSize: CGSize {
        switch placement {
        case .tableRow: return CGSize(width: 24, height: 24)
        case .cardThumbnail: return CGSize(width: 160, height: 100)
        }
    }

    private var glyphPointSize: CGFloat {
        switch placement {
        case .tableRow: return 14
        case .cardThumbnail: return 36
        }
    }

    private var cornerRadius: CGFloat {
        switch placement {
        case .tableRow: return 4
        case .cardThumbnail: return 8
        }
    }
}

// MARK: - Previews

#Preview("Table row — fallback icon (no thumbnail URL)") {
    var config = Pivox_Api_V1_IconConfig()
    config.fallbackIcon = .document

    return RowIconView(
        row: Google_Protobuf_Struct(),
        config: config,
        placement: .tableRow,
        orgID: "preview-org",
        tokenProvider: { _ in "preview-token" }
    )
    .padding()
}

#Preview("Card thumbnail — icon resolved (no thumbnail URL)") {
    var config = Pivox_Api_V1_IconConfig()
    config.iconField = "icon"
    config.fallbackIcon = .document

    var row = Google_Protobuf_Struct()
    var v = Google_Protobuf_Value()
    v.numberValue = Double(Pivox_Api_V1_Icon.video.rawValue)
    row.fields["icon"] = v

    return RowIconView(
        row: row,
        config: config,
        placement: .cardThumbnail,
        orgID: "preview-org",
        tokenProvider: { _ in "preview-token" }
    )
    .padding()
}
