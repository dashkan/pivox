import AppKit
import CoreImage
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

// MARK: - Security page

/// Security surface: password + two-factor authentication. Two
/// display modes share the same tab without resizing the dialog:
///
///   1. Main: password subsection + MFA subsection stacked in a
///      scroll view.
///   2. Enrolling: full-surface TOTP enrollment wizard (QR + OTP)
///      that takes over the tab content until the user verifies or
///      cancels.
///
/// Mode 2 is a push-page-within-the-tab, not a nested sheet, so we
/// avoid dialog-in-dialog (an explicit HIG anti-pattern) while
/// still giving the wizard enough room for the QR code and OTP
/// input.
private struct SecurityPage: View {
    @Environment(\.pivoxTheme) private var theme
    private var auth = AuthService.shared

    @State private var errorMessage: String?
    @State private var enrollment: AuthService.TOTPEnrollmentContext?
    @State private var pendingReauth: PendingReauth?

    /// Operations that may hit Firebase's "requires-recent-login"
    /// gate. When that happens we push the reauth view and, after
    /// a successful reauth, retry the original operation.
    enum PendingReauth: Identifiable {
        case enableTotp
        case disableTotp
        var id: String {
            switch self {
            case .enableTotp: return "enableTotp"
            case .disableTotp: return "disableTotp"
            }
        }
    }

    var body: some View {
        Group {
            if let enrollment {
                TOTPEnrollmentView(
                    context: enrollment,
                    onCancel: cancelEnrollment,
                    onVerified: { self.enrollment = nil })
            } else if let pendingReauth {
                ReauthenticateView(
                    reason: reauthReason(for: pendingReauth),
                    onCancel: { self.pendingReauth = nil },
                    onReauthenticated: { runPending(pendingReauth) })
            } else {
                mainView
            }
        }
        .alert(
            "Something went wrong",
            isPresented: Binding(
                get: { errorMessage != nil },
                set: { if !$0 { errorMessage = nil } }),
            presenting: errorMessage
        ) { _ in
            Button("OK", role: .cancel) { errorMessage = nil }
        } message: { message in
            Text(message)
        }
    }

    private var mainView: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Security")
                    .font(theme.pageTitleFont)
                Text("Manage your password and two-factor authentication.")
                    .font(theme.bodyFont)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 24)
            .padding(.vertical, 18)
            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    PasswordSubsection(onError: handleError)
                    Divider().padding(.vertical, 4)
                    MFASubsection(
                        onError: handleError,
                        onEnrollmentStarted: { enrollment = $0 },
                        onReauthRequired: { pendingReauth = $0 })
                }
                .padding(.horizontal, 24)
                .padding(.vertical, 16)
            }
        }
    }

    private func reauthReason(for op: PendingReauth) -> String {
        switch op {
        case .enableTotp:
            return "To enable two-factor authentication, please confirm it's you."
        case .disableTotp:
            return "To disable two-factor authentication, please confirm it's you."
        }
    }

    /// Clear the reauth marker and re-run the operation. The
    /// Firebase session is fresh now so the same method that threw
    /// `requiresRecentLogin` will succeed this time.
    private func runPending(_ op: PendingReauth) {
        pendingReauth = nil
        switch op {
        case .enableTotp:
            Task {
                do {
                    let ctx = try await auth.startTotpEnrollment()
                    enrollment = ctx
                } catch {
                    handleError(error)
                }
            }
        case .disableTotp:
            Task {
                do {
                    try await auth.unenrollTotp()
                } catch {
                    handleError(error)
                }
            }
        }
    }

    private func cancelEnrollment() {
        auth.cancelTotpEnrollment()
        enrollment = nil
    }

    private func handleError(_ error: Error) {
        errorMessage = (error as? ProfileError)?.userMessage ?? error.localizedDescription
    }
}

// MARK: - Password subsection

/// Renders "Set password" for OAuth-only users (no password linked
/// yet) and "Change password" for users who have one. The forms are
/// similar enough to share the subsection wrapper but different
/// enough in fields and action text to keep as two separate views.
private struct PasswordSubsection: View {
    let onError: (Error) -> Void
    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme

    init(onError: @escaping (Error) -> Void) {
        self.onError = onError
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Password")
                .font(theme.sectionHeadingFont)
            if auth.hasPasswordProvider {
                ChangePasswordForm(onError: onError)
            } else {
                SetPasswordForm(onError: onError)
            }
        }
        .padding(.vertical, 12)
    }
}

private struct SetPasswordForm: View {
    let onError: (Error) -> Void
    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme

    @State private var newPassword = ""
    @State private var confirmPassword = ""
    @State private var submitting = false

    init(onError: @escaping (Error) -> Void) {
        self.onError = onError
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(
                "You signed in with a provider. Set a password to enable email sign-in as a backup."
            )
            .font(theme.bodyFont)
            .foregroundStyle(.secondary)
            HStack(spacing: 10) {
                labeledSecureField("New password", text: $newPassword)
                labeledSecureField("Confirm password", text: $confirmPassword)
            }
            HStack {
                Spacer()
                Button("Set password", action: submit)
                    .keyboardShortcut(.defaultAction)
                    .controlSize(.regular)
                    .disabled(!canSubmit || submitting)
            }
        }
    }

    private var canSubmit: Bool {
        newPassword.count >= 6 && newPassword == confirmPassword
    }

    private func submit() {
        guard canSubmit else { return }
        submitting = true
        Task {
            defer { submitting = false }
            do {
                try await auth.setPassword(newPassword)
                newPassword = ""
                confirmPassword = ""
            } catch {
                onError(error)
            }
        }
    }
}

private struct ChangePasswordForm: View {
    let onError: (Error) -> Void
    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme

    @State private var currentPassword = ""
    @State private var newPassword = ""
    @State private var confirmPassword = ""
    @State private var submitting = false

    init(onError: @escaping (Error) -> Void) {
        self.onError = onError
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            labeledSecureField("Current password", text: $currentPassword)
            HStack(spacing: 10) {
                labeledSecureField("New password", text: $newPassword)
                labeledSecureField("Confirm password", text: $confirmPassword)
            }
            HStack {
                Spacer()
                Button("Change password", action: submit)
                    .keyboardShortcut(.defaultAction)
                    .controlSize(.regular)
                    .disabled(!canSubmit || submitting)
            }
        }
    }

    private var canSubmit: Bool {
        !currentPassword.isEmpty
            && newPassword.count >= 6
            && newPassword == confirmPassword
    }

    private func submit() {
        guard canSubmit else { return }
        submitting = true
        Task {
            defer { submitting = false }
            do {
                try await auth.changePassword(
                    currentPassword: currentPassword,
                    newPassword: newPassword)
                currentPassword = ""
                newPassword = ""
                confirmPassword = ""
            } catch {
                onError(error)
            }
        }
    }
}

@ViewBuilder
private func labeledSecureField(_ label: String, text: Binding<String>) -> some View {
    VStack(alignment: .leading, spacing: 4) {
        Text(label)
            .font(.caption)
            .foregroundStyle(.secondary)
        SecureField("", text: text)
            .textFieldStyle(.roundedBorder)
    }
}

// MARK: - MFA subsection

private struct MFASubsection: View {
    let onError: (Error) -> Void
    let onEnrollmentStarted: (AuthService.TOTPEnrollmentContext) -> Void
    let onReauthRequired: (SecurityPage.PendingReauth) -> Void
    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme

    @State private var submitting = false

    init(
        onError: @escaping (Error) -> Void,
        onEnrollmentStarted: @escaping (AuthService.TOTPEnrollmentContext) -> Void,
        onReauthRequired: @escaping (SecurityPage.PendingReauth) -> Void
    ) {
        self.onError = onError
        self.onEnrollmentStarted = onEnrollmentStarted
        self.onReauthRequired = onReauthRequired
    }

    var body: some View {
        // Establish a read dependency on profileRevision so in-place
        // mutations to the user's enrolled factors re-render this
        // subsection (see AuthService.profileRevision doc comment).
        let _ = auth.profileRevision
        return VStack(alignment: .leading, spacing: 14) {
            Text("Two-factor authentication")
                .font(theme.sectionHeadingFont)

            if auth.isMfaEnrolled {
                HStack(alignment: .firstTextBaseline) {
                    VStack(alignment: .leading, spacing: 4) {
                        Label("Enabled", systemImage: "checkmark.shield.fill")
                            .foregroundStyle(Color.accentColor)
                            .font(theme.bodyFont)
                        Text(
                            "You'll be asked for a code from your authenticator app when signing in."
                        )
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("Disable", role: .destructive, action: disable)
                        .controlSize(.regular)
                        .disabled(submitting)
                }
            } else {
                HStack(alignment: .firstTextBaseline) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Disabled")
                            .font(theme.bodyFont)
                            .foregroundStyle(.secondary)
                        Text(
                            "Add an authenticator app to require a second step when signing in."
                        )
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("Enable", action: startEnrollment)
                        .controlSize(.regular)
                        .disabled(submitting)
                }
            }
        }
        .padding(.vertical, 12)
    }

    private func startEnrollment() {
        submitting = true
        Task {
            defer { submitting = false }
            do {
                let ctx = try await auth.startTotpEnrollment()
                onEnrollmentStarted(ctx)
            } catch ProfileError.requiresRecentLogin {
                onReauthRequired(.enableTotp)
            } catch {
                onError(error)
            }
        }
    }

    private func disable() {
        submitting = true
        Task {
            defer { submitting = false }
            do {
                try await auth.unenrollTotp()
            } catch ProfileError.requiresRecentLogin {
                onReauthRequired(.disableTotp)
            } catch {
                onError(error)
            }
        }
    }
}

// MARK: - Reauthentication push page

/// "Confirm it's you" surface shown before privileged ops (MFA
/// enroll/unenroll, account delete if we wire it later). Shows
/// exactly the reauth methods the user actually has linked — a
/// password field if they have email/password, and buttons for any
/// OAuth providers in `providerData`. Picks the first completed
/// reauth path as success; does not require the user to re-auth
/// with every linked method.
private struct ReauthenticateView: View {
    let reason: String
    let onCancel: () -> Void
    let onReauthenticated: () -> Void

    private var auth = AuthService.shared
    @Environment(\.pivoxTheme) private var theme

    @State private var password = ""
    @State private var submitting = false
    @State private var errorMessage: String?

    init(
        reason: String,
        onCancel: @escaping () -> Void,
        onReauthenticated: @escaping () -> Void
    ) {
        self.reason = reason
        self.onCancel = onCancel
        self.onReauthenticated = onReauthenticated
    }

    private var providerIDs: [String] {
        (auth.currentUser?.providerData ?? []).map(\.providerID)
    }
    private var hasPassword: Bool { providerIDs.contains("password") }
    private var hasGoogle: Bool { providerIDs.contains("google.com") }
    private var hasGitHub: Bool { providerIDs.contains("github.com") }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                Button(action: onCancel) {
                    Label("Back", systemImage: "chevron.left")
                        .labelStyle(.titleAndIcon)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                Spacer()
            }
            .padding(.horizontal, 20)
            .padding(.top, 14)
            .padding(.bottom, 8)

            VStack(alignment: .leading, spacing: 4) {
                Text("Confirm it's you")
                    .font(theme.pageTitleFont)
                Text(reason)
                    .font(theme.bodyFont)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 24)
            .padding(.bottom, 16)
            Divider()

            VStack(alignment: .leading, spacing: 16) {
                if hasPassword {
                    passwordRow
                }
                if hasPassword && (hasGoogle || hasGitHub) {
                    HStack(spacing: 10) {
                        Divider()
                        Text("or")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Divider()
                    }
                    .frame(maxWidth: 360)
                }
                if hasGoogle {
                    Button(action: { runReauth(.google) }) {
                        Label("Continue with Google", systemImage: "g.circle")
                    }
                    .controlSize(.large)
                    .disabled(submitting)
                }
                if hasGitHub {
                    Button(action: { runReauth(.github) }) {
                        Label("Continue with GitHub", systemImage: "chevron.left.forwardslash.chevron.right")
                    }
                    .controlSize(.large)
                    .disabled(submitting)
                }
                if let errorMessage {
                    Text(errorMessage)
                        .font(.caption)
                        .foregroundStyle(.red)
                }
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 20)

            Spacer()
        }
    }

    private var passwordRow: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Enter your password")
                .font(theme.sectionHeadingFont)
            HStack(spacing: 8) {
                SecureField("", text: $password)
                    .textFieldStyle(.roundedBorder)
                    .frame(maxWidth: 280)
                    .onSubmit { runReauth(.password) }
                Button("Continue") { runReauth(.password) }
                    .keyboardShortcut(.defaultAction)
                    .controlSize(.regular)
                    .disabled(password.isEmpty || submitting)
            }
        }
    }

    private enum Method { case password, google, github }

    private func runReauth(_ method: Method) {
        submitting = true
        errorMessage = nil
        Task {
            defer { submitting = false }
            do {
                switch method {
                case .password: try await auth.reauthenticateWithPassword(password)
                case .google: try await auth.reauthenticateWithGoogle()
                case .github: try await auth.reauthenticateWithGitHub()
                }
                onReauthenticated()
            } catch let error as ProfileError {
                errorMessage = error.userMessage
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }
}

// MARK: - TOTP enrollment wizard

/// Full-surface takeover for TOTP enrollment. Left: QR + manual
/// secret for authenticator apps. Right: 6-digit code entry +
/// verify button. Cancel / back dismisses back to the main Security
/// view; on successful verification the parent clears the
/// enrollment context, which pops us back to the main view.
private struct TOTPEnrollmentView: View {
    let context: AuthService.TOTPEnrollmentContext
    let onCancel: () -> Void
    let onVerified: () -> Void

    @Environment(\.pivoxTheme) private var theme
    private var auth = AuthService.shared

    @State private var code = ""
    @State private var submitting = false
    @State private var errorMessage: String?

    init(
        context: AuthService.TOTPEnrollmentContext,
        onCancel: @escaping () -> Void,
        onVerified: @escaping () -> Void
    ) {
        self.context = context
        self.onCancel = onCancel
        self.onVerified = onVerified
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                Button(action: onCancel) {
                    Label("Back", systemImage: "chevron.left")
                        .labelStyle(.titleAndIcon)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                Spacer()
            }
            .padding(.horizontal, 20)
            .padding(.top, 14)
            .padding(.bottom, 8)

            VStack(alignment: .leading, spacing: 4) {
                Text("Set up two-factor authentication")
                    .font(theme.pageTitleFont)
                Text(
                    "Scan the QR code with your authenticator app, then enter the 6-digit code it shows."
                )
                .font(theme.bodyFont)
                .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 24)
            .padding(.bottom, 16)
            Divider()

            HStack(alignment: .top, spacing: 28) {
                qrColumn
                verifyColumn
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 20)

            Spacer()

            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .padding(.horizontal, 24)
                    .padding(.bottom, 8)
            }
        }
    }

    private var qrColumn: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let qr = qrImage {
                Image(nsImage: qr)
                    .interpolation(.none)
                    .resizable()
                    .scaledToFit()
                    .frame(width: 180, height: 180)
                    .padding(6)
                    .background(
                        RoundedRectangle(cornerRadius: 8).fill(Color.white))
            }
            VStack(alignment: .leading, spacing: 4) {
                Text("Or enter this key manually")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                HStack(spacing: 6) {
                    Text(context.sharedSecret)
                        .font(.system(.callout, design: .monospaced))
                        .textSelection(.enabled)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Button {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(
                            context.sharedSecret, forType: .string)
                    } label: {
                        Image(systemName: "doc.on.doc")
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                    .help("Copy")
                }
                .frame(maxWidth: 220, alignment: .leading)
            }
        }
    }

    private var verifyColumn: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Enter the 6-digit code")
                    .font(theme.sectionHeadingFont)
                OTPSegmentedField(value: $code, length: 6)
            }
            HStack {
                Button("Cancel", role: .cancel, action: onCancel)
                    .controlSize(.regular)
                Spacer()
                Button("Verify", action: verify)
                    .keyboardShortcut(.defaultAction)
                    .controlSize(.regular)
                    .disabled(code.count < 6 || submitting)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var qrImage: NSImage? {
        guard
            let data = context.qrCodeURL.data(using: .utf8),
            let filter = CIFilter(name: "CIQRCodeGenerator")
        else { return nil }
        filter.setValue(data, forKey: "inputMessage")
        filter.setValue("H", forKey: "inputCorrectionLevel")
        guard let output = filter.outputImage else { return nil }
        let rep = NSCIImageRep(ciImage: output)
        let image = NSImage(size: rep.size)
        image.addRepresentation(rep)
        return image
    }

    private func verify() {
        submitting = true
        errorMessage = nil
        Task {
            defer { submitting = false }
            do {
                try await auth.verifyTotpEnrollment(code: code)
                onVerified()
            } catch let error as ProfileError {
                errorMessage = error.userMessage
            } catch {
                errorMessage = error.localizedDescription
            }
        }
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

