import AppKit
import FirebaseAuth
import SwiftUI

/// "Who I am" surface: avatar, display name, email, delete-account.
/// Security-specific concerns (password, 2FA) live on `SecurityPage`.
struct AccountPage: View {
    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme
    @State private var errorMessage: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header: title + Sign out (top-right). Sign out is NOT a
            // destructive action — it's just exiting the session.
            HStack(alignment: .center) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Account")
                        .font(theme.pageTitleFont)
                    Text("Manage your account information.")
                        .font(theme.bodyFont)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Button(action: { auth.signOut() }) {
                    Label("Sign out", systemImage: "rectangle.portrait.and.arrow.forward")
                        .labelStyle(.pivoxIcon)
                }
                .controlSize(.regular)
                .accessibilityIdentifier("profile-sign-out")
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 18)
            Divider()

            // No ScrollView — the page fits comfortably at natural
            // height and the Settings window is sized to the
            // content. A ScrollView would fight our
            // `fittingSize`-driven window resize: during a tab
            // switch, ScrollView expands to its container's
            // current (wrong) height, a scrollbar flickers in,
            // then the window catches up and the scrollbar
            // retreats. Add ScrollView back only if a section
            // grows past a screen-height threshold.
            VStack(alignment: .leading, spacing: 0) {
                ProfileSubsection(onError: handleError)
                Divider().padding(.vertical, 4)
                EmailSubsection(onError: handleError)
                Divider().padding(.vertical, 4)
                DangerSubsection(onError: handleError)
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
        }
        .frame(width: 640)
        .alert(
            "Something went wrong",
            isPresented: Binding(
                get: { errorMessage != nil },
                set: { if !$0 { errorMessage = nil } }
            ),
            presenting: errorMessage
        ) { _ in
            Button("OK", role: .cancel) { errorMessage = nil }
        } message: { message in
            Text(message)
        }
    }

    private func handleError(_ error: Error) {
        errorMessage = (error as? ProfileError)?.userMessage ?? error.localizedDescription
    }
}

// MARK: - Profile subsection (display name + photo)

private struct ProfileSubsection: View {
    let onError: (Error) -> Void
    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme

    // Explicit @State mirrors of the Firebase user fields we render.
    // `AuthService.currentUser` is Observable, but Firebase mutates
    // its `User` reference in-place on profile changes (same
    // reference, new field value) and SwiftUI's identity-based
    // tracking doesn't always refire. Keeping local copies and
    // seeding them from the user keeps re-render deterministic.
    @State private var displayName: String = ""
    @State private var photoURL: URL? = nil

    /// The last committed display name. Used as the baseline for
    /// Esc (revert) and for skipping the API call when the draft
    /// hasn't actually changed from what we already saved.
    @State private var committedName: String = ""

    @FocusState private var nameFocused: Bool

    init(onError: @escaping (Error) -> Void) {
        self.onError = onError
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Profile")
                .font(theme.sectionHeadingFont)

            HStack(alignment: .top, spacing: 16) {
                VStack(spacing: 8) {
                    AccountAvatar(photoURL: photoURL, size: 80)
                    PhotoMenuButton(
                        onError: onError,
                        onPhotoChanged: { photoURL = $0 }
                    )
                }

                VStack(alignment: .leading, spacing: 10) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Display name")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        TextField("", text: $displayName)
                            .textFieldStyle(.roundedBorder)
                            .focused($nameFocused)
                            .textContentType(.name)
                            .onSubmit(commitName)
                            .onExitCommand { revertName() }
                            .onChange(of: nameFocused) { _, isFocused in
                                // Blur commits. Commit on the true→false
                                // transition only; ignore the initial
                                // false→false case on first render.
                                if !isFocused { commitName() }
                            }
                            .accessibilityIdentifier("profile-display-name")
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(.vertical, 12)
        .task {
            let current = auth.currentUser?.displayName ?? ""
            displayName = current
            committedName = current
            photoURL = auth.currentUser?.photoURL
        }
    }

    /// Save the draft as the new display name. No-op when the draft
    /// matches the last committed value — blur after a pure
    /// tab-through shouldn't fire an API call.
    private func commitName() {
        let trimmed = displayName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed != committedName else {
            // Normalize whitespace-only edits back to the baseline so
            // the user's next focus is consistent.
            displayName = committedName
            return
        }
        Task {
            do {
                try await auth.updateDisplayName(trimmed)
                let authoritative = auth.currentUser?.displayName ?? trimmed
                committedName = authoritative
                displayName = authoritative
            } catch {
                // Commit failed — revert to the baseline and surface
                // the error. The user sees the field snap back to the
                // old name, which matches how Finder handles rename
                // failures.
                displayName = committedName
                onError(error)
            }
        }
    }

    /// Esc discards the draft back to the last committed value and
    /// drops focus. Once focus is lost, the blur handler runs — but
    /// because we've already reverted `displayName` to the baseline,
    /// the commit guard short-circuits before any API call.
    private func revertName() {
        displayName = committedName
        nameFocused = false
    }
}

// MARK: - Photo menu

/// Explicit "Change photo" button under the avatar. Kept separate from
/// the avatar image: making the avatar itself clickable is a discovery
/// trap — most users don't realize the image is a control.
private struct PhotoMenuButton: View {
    private var auth = AuthService.shared
    let onError: (Error) -> Void
    /// Called with the new photoURL after a successful change so the
    /// parent can update its local mirror state directly — we can't
    /// reliably depend on @Observable propagation through Firebase's
    /// in-place-mutated User.
    let onPhotoChanged: (URL?) -> Void

    init(onError: @escaping (Error) -> Void, onPhotoChanged: @escaping (URL?) -> Void) {
        self.onError = onError
        self.onPhotoChanged = onPhotoChanged
    }

    var body: some View {
        Menu {
            // Firebase Storage isn't wired yet — see docs/native-app.md.
            // Disabled rather than an error-on-click: HIG prefers
            // unavailable affordances be visibly unavailable, not
            // discover-an-error.
            Button {} label: {
                Label("Upload photo…", systemImage: "square.and.arrow.up")
            }
            .disabled(true)

            ForEach(providerPhotos, id: \.providerId) { p in
                Button {
                    applyPhotoURL(p.photoURL)
                } label: {
                    Label("Use \(p.label) photo", systemImage: p.iconSystemName)
                }
            }

            if auth.currentUser?.photoURL != nil {
                Divider()
                Button {
                    applyPhotoURL(nil)
                } label: {
                    Label("Remove photo", systemImage: "trash")
                }
            }
        } label: {
            Text("Change")
        }
        .menuStyle(.button)
        .fixedSize()
    }

    private struct ProviderPhoto {
        let providerId: String
        let label: String
        let iconSystemName: String
        let photoURL: String
    }

    private var providerPhotos: [ProviderPhoto] {
        let providers = auth.currentUser?.providerData ?? []
        return providers.compactMap { info -> ProviderPhoto? in
            guard let url = info.photoURL?.absoluteString, !url.isEmpty else { return nil }
            let (label, icon) = ProfileProviderLabel.lookup(info.providerID)
            return ProviderPhoto(providerId: info.providerID,
                                 label: label,
                                 iconSystemName: icon,
                                 photoURL: url)
        }
    }

    private func applyPhotoURL(_ url: String?) {
        Task {
            do {
                try await auth.setPhotoURL(url)
                onPhotoChanged(auth.currentUser?.photoURL)
            } catch {
                onError(error)
            }
        }
    }
}

private enum ProfileProviderLabel {
    static func lookup(_ providerID: String) -> (label: String, icon: String) {
        switch providerID {
        case "google.com":   return ("Google", "globe")
        case "github.com":   return ("GitHub", "chevron.left.forwardslash.chevron.right")
        case "apple.com":    return ("Apple", "apple.logo")
        case "password":     return ("email", "envelope")
        default:             return (providerID, "person.crop.square")
        }
    }
}

// MARK: - Email subsection

private struct EmailSubsection: View {
    let onError: (Error) -> Void
    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme

    @State private var sending = false

    init(onError: @escaping (Error) -> Void) {
        self.onError = onError
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Email")
                .font(theme.sectionHeadingFont)

            HStack(spacing: 8) {
                Text(auth.currentUser?.email ?? "—")
                    .font(theme.bodyFont)
                if auth.currentUser?.isEmailVerified == true {
                    Label("Verified", systemImage: "checkmark.seal.fill")
                        .labelStyle(.pivoxIcon)
                        .font(theme.statusBadgeFont)
                        .foregroundStyle(theme.success)
                } else if auth.currentUser != nil {
                    Label("Unverified", systemImage: "exclamationmark.triangle.fill")
                        .labelStyle(.pivoxIcon)
                        .font(theme.statusBadgeFont)
                        .foregroundStyle(theme.warning)
                }
                Spacer()
            }

            if auth.currentUser?.isEmailVerified == false {
                Button(action: sendVerification) {
                    if sending {
                        HStack(spacing: 6) {
                            ProgressView().controlSize(.small)
                            Text("Sending…")
                        }
                    } else {
                        Text("Send verification email")
                    }
                }
                .controlSize(.small)
                .disabled(sending)
            }
        }
        .padding(.vertical, 12)
    }

    private func sendVerification() {
        sending = true
        Task {
            defer { sending = false }
            do {
                try await auth.sendVerificationEmail()
            } catch {
                onError(error)
            }
        }
    }
}

// MARK: - Danger subsection (delete account)

private struct DangerSubsection: View {
    let onError: (Error) -> Void
    @Environment(\.pivoxTheme) private var theme
    private var auth = AuthService.shared

    @State private var confirmDelete = false

    init(onError: @escaping (Error) -> Void) {
        self.onError = onError
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Danger zone")
                .font(theme.sectionHeadingFont)
                .foregroundStyle(theme.destructive)

            HStack(alignment: .center) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Delete account")
                        .font(theme.rowTitleFont)
                    Text("Permanently remove your Pivox account. This can't be undone.")
                        .font(theme.bodyFont)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Button(role: .destructive, action: { confirmDelete = true }) {
                    Label("Delete account", systemImage: "trash")
                        .labelStyle(.pivoxIcon)
                        .foregroundStyle(.white)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.regular)
                .tint(theme.destructive)
            }
        }
        .padding(.vertical, 12)
        .confirmationDialog(
            "Delete your account?",
            isPresented: $confirmDelete,
            titleVisibility: .visible
        ) {
            Button("Delete", role: .destructive, action: performDelete)
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This permanently deletes your Pivox account. This can't be undone.")
        }
    }

    private func performDelete() {
        Task {
            do {
                try await auth.deleteAccount()
            } catch {
                onError(error)
            }
        }
    }
}

// MARK: - Avatar

/// Circular avatar — pure display, not a control. Photo management is
/// handled by an adjacent explicit button (see `PhotoMenuButton`).
private struct AccountAvatar: View {
    let photoURL: URL?
    let size: CGFloat

    var body: some View {
        CachedAvatarImage(url: photoURL) {
            Image(systemName: "person.circle.fill")
                .resizable()
                .scaledToFit()
                .foregroundStyle(.secondary)
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .clipped()
        .overlay(Circle().strokeBorder(Color.secondary.opacity(0.3), lineWidth: 0.5))
    }
}
