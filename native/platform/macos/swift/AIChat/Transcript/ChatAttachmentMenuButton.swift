import SwiftUI

/// `+` button on the leading edge of the chat composer's tool row
/// that opens a small menu of attachment options. Mirrors Claude
/// in VS Code / ChatGPT desktop's leading affordance — the
/// catch-all surface for "add something to this message that isn't
/// typed text."
///
/// The popup positions itself naturally — because the composer
/// lives at the bottom of the chat panel, AppKit's menu manager
/// flips the popup above the button when there's no room below,
/// which is exactly the behavior we want here.
///
/// Both menu items are no-ops for now; wiring real handlers is
/// scoped to follow-up work.
struct ChatAttachmentMenuButton: View {
    @Environment(\.pivoxTheme) private var theme

    var body: some View {
        Menu {
            Button {
                // TODO: file picker → upload local file as message
                // attachment. Implementation will route through the
                // assets ingestion pipeline.
            } label: {
                Label("Upload from computer", systemImage: "square.and.arrow.up")
            }
            Button {
                // TODO: open the asset picker UI scoped to the
                // current org and let the user pick an existing
                // asset to attach.
            } label: {
                Label("Select asset", systemImage: "photo.on.rectangle")
            }
        } label: {
            // Same visual sizing as IconButton — 17pt glyph centered
            // in a 32pt hit target, secondary foreground. Keeps the
            // tool row cohesive across all leading-side controls.
            Image(systemName: "plus")
                .font(theme.iconToolbar)
                .foregroundStyle(theme.textSecondary)
                .frame(width: 32, height: 32)
                .contentShape(Rectangle())
        }
        // `.borderlessButton` strips the default Menu chrome
        // (rounded chip + chevron) so the trigger reads as a plain
        // icon button matching its IconButton siblings.
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .accessibilityLabel("Add attachment")
        .help("Add attachment")
    }
}
