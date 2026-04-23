import FirebaseAuth
import SwiftUI

/// Profile settings surface. Mirrors the web `user-profile` feature's
/// two-page (Account / Security) layout. Pass 1 ships the Account
/// page. Passes 2 and 3 add password + MFA + connected accounts.
///
/// Uses a segmented picker rather than a left sidebar: with only two
/// pages a sidebar wastes vertical space, and the segmented pill
/// matches the Apple Music / 3rd-party-app convention for a handful
/// of sibling modes. If we grow past ~4 pages, revisit.
struct ProfileView: View {
    @State private var page: Page = .account
    @State private var errorMessage: String?
    @Environment(\.dismiss) private var dismiss
    private var auth = AuthService.shared

    enum Page: String, Hashable, CaseIterable, Identifiable {
        case account = "Account"
        case security = "Security"

        var id: String { rawValue }
    }

    var body: some View {
        VStack(spacing: 0) {
            Picker("", selection: $page) {
                ForEach(Page.allCases) { page in
                    Text(page.rawValue).tag(page)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(maxWidth: 320)
            .padding(.horizontal, 24)
            .padding(.top, 20)
            .padding(.bottom, 12)

            Divider()

            content
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)

            Divider()

            HStack {
                Spacer()
                Button("Done") { dismiss() }
                    .keyboardShortcut(.defaultAction)
                    .controlSize(.large)
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 12)
        }
        .task { await auth.refreshUser() }
        // HIG: success is the expected outcome and is communicated by the
        // UI reflecting the new value. Only surface failures, and do it
        // in a proper alert rather than inline status rows.
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

    @ViewBuilder
    private var content: some View {
        switch page {
        case .account: AccountPage(onError: handleError)
        case .security: SecurityPage()
        }
    }

    private func handleError(_ error: Error) {
        errorMessage = (error as? ProfileError)?.userMessage ?? error.localizedDescription
    }
}

// MARK: - Account Page

private struct AccountPage: View {
    let onError: (Error) -> Void
    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme

    init(onError: @escaping (Error) -> Void) {
        self.onError = onError
    }

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

            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    ProfileSubsection(onError: onError)
                    Divider().padding(.vertical, 4)
                    EmailSubsection(onError: onError)
                    Divider().padding(.vertical, 4)
                    DangerSubsection(onError: onError)
                }
                .padding(.horizontal, 24)
                .padding(.vertical, 16)
            }
        }
    }
}

// MARK: - Profile subsection (display name + photo)

private struct ProfileSubsection: View {
    let onError: (Error) -> Void
    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme

    @State private var editingName = false

    // Mirror the observable fields we render into explicit @State.
    // Swift's Observation tracking on `AuthService.currentUser`
    // intermittently fails to re-register a body tracker after a
    // save when the Firebase User instance is mutated in-place
    // (same reference, different field value). Keeping our own
    // copies and updating them from the action handlers makes
    // re-render deterministic.
    @State private var displayName: String = ""
    @State private var photoURL: URL? = nil

    init(onError: @escaping (Error) -> Void) {
        self.onError = onError
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Profile")
                .font(theme.sectionHeadingFont)

            HStack(alignment: .top, spacing: 16) {
                VStack(spacing: 8) {
                    AvatarView(photoURL: photoURL, size: 80)
                    PhotoMenuButton(
                        onError: onError,
                        onPhotoChanged: { photoURL = $0 }
                    )
                }

                VStack(alignment: .leading, spacing: 10) {
                    ProfileFieldRow(
                        label: "Display name",
                        value: displayName,
                        editing: $editingName,
                        fetchCurrent: { auth.currentUser?.displayName ?? "" },
                        onSave: saveName)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(.vertical, 12)
        .task {
            displayName = auth.currentUser?.displayName ?? ""
            photoURL = auth.currentUser?.photoURL
        }
    }

    private func saveName(_ newName: String) {
        Task {
            do {
                let trimmed = newName.trimmingCharacters(in: .whitespacesAndNewlines)
                try await auth.updateDisplayName(trimmed)
                // Force the local mirror to pick up the new value.
                displayName = auth.currentUser?.displayName ?? trimmed
                editingName = false
            } catch {
                onError(error)
            }
        }
    }
}

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
        // Default `menuStyle(.button)` with system-default control
        // size — matches the scale of other neutral bordered buttons
        // in this dialog (Sign Out). `.fixedSize()` so the button
        // hugs its label width instead of stretching with the
        // enclosing VStack.
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

// MARK: - Security page (placeholder for Pass 2)

private struct SecurityPage: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Security")
                    .font(.title2.weight(.semibold))
                Text("Password and two-factor authentication.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            HStack {
                Spacer()
                VStack(spacing: 8) {
                    Image(systemName: "hammer")
                        .font(.largeTitle)
                        .foregroundStyle(.tertiary)
                    Text("Coming in the next pass.")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            Spacer()
        }
        .padding(24)
    }
}

// MARK: - Shared field components

/// Inline-editable field with matched heights between read and edit
/// modes — no vertical jump when swapping the Text for a TextField.
/// Pencil uses the shared IconButton so sizing and hover affordances
/// stay consistent with the rest of the app.
///
/// Owns its own draft state and reseeds it from `value` every time the
/// user enters edit mode. Parent-owned drafts with a `onBeginEdit`
/// callback proved unreliable across repeated edits because closure
/// captures and State propagation timing can leave the draft bound to
/// a stale read.
private struct ProfileFieldRow: View {
    let label: String
    let value: String
    @Binding var editing: Bool
    /// Reads the live value at click time. Prefer this over `value`
    /// when seeding the draft, because `value` is captured at the last
    /// view-body evaluation and can be stale after a Firebase reload
    /// that didn't propagate through @Observable tracking as expected.
    let fetchCurrent: () -> String
    let onSave: (String) -> Void

    @State private var draft: String = ""
    @Environment(\.pivoxTheme) private var theme

    /// Outer row height. Sized to match the tallest child across
    /// both modes (read-mode is dominated by the 32pt IconButton hit
    /// target) so switching edit mode on/off doesn't shift the
    /// surrounding layout. Inner controls (TextField, buttons) stay
    /// at their natural macOS sizes and center inside this frame.
    private let rowHeight: CGFloat = 32

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label)
                .font(theme.fieldLabelFont)
                .foregroundStyle(.secondary)

            Group {
                if editing {
                    HStack(spacing: 6) {
                        TextField("", text: $draft)
                            .textFieldStyle(.roundedBorder)
                            .onSubmit { onSave(draft) }
                        Button("Save") { onSave(draft) }
                            .buttonStyle(.borderedProminent)
                            .keyboardShortcut(.defaultAction)
                            .disabled(draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                        Button("Cancel") { editing = false }
                            .buttonStyle(.bordered)
                            .keyboardShortcut(.cancelAction)
                    }
                } else {
                    HStack(spacing: 8) {
                        Text(value.isEmpty ? "—" : value)
                            .font(theme.bodyFont)
                            .foregroundStyle(value.isEmpty ? .tertiary : .primary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        IconButton(
                            systemName: "pencil",
                            label: "Edit \(label.lowercased())"
                        ) {
                            draft = fetchCurrent()
                            editing = true
                        }
                        Spacer(minLength: 0)
                    }
                }
            }
            .frame(height: rowHeight)
        }
    }
}

/// Circular avatar — pure display, not a control. Photo management is
/// handled by an adjacent explicit button (see `PhotoMenuButton`).
private struct AvatarView: View {
    let photoURL: URL?
    let size: CGFloat

    var body: some View {
        AsyncImage(url: photoURL) { phase in
            if let image = phase.image {
                image.resizable().scaledToFill()
            } else if phase.error != nil || photoURL == nil {
                Image(systemName: "person.circle.fill")
                    .resizable()
                    .scaledToFit()
                    .foregroundStyle(.secondary)
            } else {
                ProgressView().controlSize(.small)
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .clipped()
        .overlay(Circle().strokeBorder(Color.secondary.opacity(0.3), lineWidth: 0.5))
    }
}

