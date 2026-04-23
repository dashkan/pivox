import SwiftUI

/// Bottom-of-sidebar account bar. Mirrors the pattern used by Apple
/// Music and Mail: a translucent strip pinned below the navigation
/// list, showing the user's avatar and display name. Tapping it
/// presents the Profile dialog.
///
/// Accepts the fields it renders as plain value types (URL, String)
/// rather than a Firebase `User` reference — SwiftUI's view-diff
/// doesn't reliably re-render when a same-reference reference type
/// mutates in place, which was the source of the "display name
/// didn't update" bug we hit on the profile page.
///
/// Lives in its own file so the Windows shell (WinUI 3) can provide a
/// parallel implementation without reaching into monolithic SwiftUI.
struct ProfileBar: View {
    let photoURL: URL?
    let displayName: String?
    let email: String?
    let action: () -> Void

    private static let avatarSize: CGFloat = 32

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                ProfileBarAvatar(photoURL: photoURL, size: Self.avatarSize)
                Text(displayLabel)
                    .font(.system(size: 14))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(ProfileBarButtonStyle())
        .background(.ultraThinMaterial)
        .overlay(alignment: .top) {
            Divider()
        }
        .accessibilityIdentifier("sidebar-profile-bar")
    }

    private var displayLabel: String {
        if let name = displayName, !name.isEmpty { return name }
        if let email = email, !email.isEmpty { return email }
        return "Signed in"
    }
}

/// Small circular avatar for the bar. Falls back to an SF Symbol when
/// the user has no photo or the image is still loading.
private struct ProfileBarAvatar: View {
    let photoURL: URL?
    let size: CGFloat

    var body: some View {
        AsyncImage(url: photoURL) { phase in
            if let image = phase.image {
                image.resizable().scaledToFill()
            } else if phase.error != nil || photoURL == nil {
                Image(systemName: "person.crop.circle.fill")
                    .resizable()
                    .scaledToFit()
                    .foregroundStyle(.secondary)
            } else {
                ProgressView().controlSize(.small)
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .overlay(
            Circle().strokeBorder(Color.secondary.opacity(0.25), lineWidth: 0.5)
        )
    }
}

/// Pressed state only (no hover). Full-rect hit area — we can't use
/// `.buttonStyle(.plain)` because it bridges to an NSButton whose
/// hit-test skips transparent padding.
private struct ProfileBarButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed
                ? Color.secondary.opacity(0.15)
                : Color.clear)
    }
}

