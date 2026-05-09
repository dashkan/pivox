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

/// Empty-state surface for a `CollectionWidget` whose data source
/// returned no rows. Shape mirrors the proto's `EmptyState`:
/// optional title + subtitle + icon + primary call-to-action.
///
/// The action handler is a closure injected by the renderer's
/// containing view — it's how the customer's "Upload" / "Invite"
/// click flows back into the app's action dispatcher. Empty action
/// keys produce a hidden button (the proto allows omitting
/// `primary_action`).
struct EmptyStateView: View {
    let state: Pivox_Api_V1_EmptyState
    let onAction: (Pivox_Api_V1_RowAction) -> Void

    var body: some View {
        VStack(spacing: 12) {
            if state.icon != .unspecified {
                state.icon.image
                    .font(.system(size: 48, weight: .light))
                    .foregroundStyle(.secondary)
                    .padding(.bottom, 4)
                    .accessibilityHidden(true)
            }
            if !state.title.isEmpty {
                Text(state.title)
                    .font(.title3.weight(.semibold))
            }
            if !state.subtitle.isEmpty {
                Text(state.subtitle)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            if state.hasPrimaryAction, !state.primaryAction.key.isEmpty {
                Button {
                    onAction(state.primaryAction)
                } label: {
                    Label(
                        state.primaryAction.label,
                        systemImage: state.primaryAction.icon.sfSymbol.isEmpty
                            ? "plus" : state.primaryAction.icon.sfSymbol
                    )
                }
                .buttonStyle(.borderedProminent)
                .padding(.top, 8)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }
}

#Preview("Asset library — empty") {
    var state = Pivox_Api_V1_EmptyState()
    state.title = "No assets yet"
    state.subtitle = "Upload or import files to see them here."
    state.icon = .photo
    var action = Pivox_Api_V1_RowAction()
    action.key = "upload_assets"
    action.label = "Upload"
    action.icon = .upload
    state.primaryAction = action

    return EmptyStateView(state: state) { action in
        print("preview action: \(action.key)")
    }
    .frame(width: 480, height: 320)
}

#Preview("Title only") {
    var state = Pivox_Api_V1_EmptyState()
    state.title = "All clear"
    return EmptyStateView(state: state) { _ in }
        .frame(width: 480, height: 320)
}
