import AppKit
import CoreImage
import FirebaseAuth
import SwiftUI

/// Security surface: password + two-factor authentication.
///
/// Three display modes share the same tab without ever showing a
/// nested sheet (dialog-in-dialog is an HIG anti-pattern):
///
///   1. Main: password subsection + MFA subsection stacked in a
///      scroll view.
///   2. Enrolling: full-surface TOTP enrollment wizard (QR + OTP).
///   3. Reauth: "confirm it's you" push page when a sensitive op
///      hits Firebase's requires-recent-login gate.
struct SecurityPage: View {
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
        .frame(width: 640)
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

            // No ScrollView — see AccountPage note: ScrollView
            // fights the fittingSize-driven window resize and a
            // scrollbar flickers during tab transitions.
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
                labeledSecureField(
                    "New password", text: $newPassword,
                    contentType: .newPassword, onSubmit: submit)
                labeledSecureField(
                    "Confirm password", text: $confirmPassword,
                    contentType: .newPassword, onSubmit: submit)
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
            labeledSecureField(
                "Current password", text: $currentPassword,
                contentType: .password, onSubmit: submit)
            HStack(spacing: 10) {
                labeledSecureField(
                    "New password", text: $newPassword,
                    contentType: .newPassword, onSubmit: submit)
                labeledSecureField(
                    "Confirm password", text: $confirmPassword,
                    contentType: .newPassword, onSubmit: submit)
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
private func labeledSecureField(
    _ label: String,
    text: Binding<String>,
    contentType: NSTextContentType? = nil,
    onSubmit: @escaping () -> Void = {}
) -> some View {
    VStack(alignment: .leading, spacing: 4) {
        Text(label)
            .font(.caption)
            .foregroundStyle(.secondary)
        SecureField("", text: text)
            .textFieldStyle(.roundedBorder)
            .textContentType(contentType)
            .onSubmit(onSubmit)
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

    /// Two-stage reauth state. The first-factor stage shows
    /// password / OAuth options; if Firebase responds with
    /// `requiresSecondFactor`, we transition to the OTP stage so
    /// the user can complete the MFA leg of reauth. Both stages
    /// have to succeed before we call `onReauthenticated()`.
    private enum Stage { case firstFactor, secondFactor }

    @State private var stage: Stage = .firstFactor
    @State private var password = ""
    @State private var otpCode = ""
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
                Button(action: backOrCancel) {
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
                Text(stage == .firstFactor ? "Confirm it's you" : "Two-factor authentication")
                    .font(theme.pageTitleFont)
                Text(stage == .firstFactor
                    ? reason
                    : "Enter the 6-digit code from your authenticator app to continue.")
                    .font(theme.bodyFont)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 24)
            .padding(.bottom, 16)
            Divider()

            HStack {
                Spacer()
                Group {
                    switch stage {
                    case .firstFactor: firstFactorContent
                    case .secondFactor: secondFactorContent
                    }
                }
                .frame(width: 360)
                Spacer()
            }
            .padding(.vertical, 32)

            Spacer()
        }
    }

    // MARK: - Stage 1: first factor

    private var firstFactorContent: some View {
        VStack(alignment: .leading, spacing: 16) {
            if hasPassword {
                passwordRow
            }
            if hasPassword && (hasGoogle || hasGitHub) {
                HStack(spacing: 10) {
                    Rectangle().frame(height: 1).foregroundStyle(theme.border)
                    Text("or").font(theme.bodySmallFont).foregroundStyle(.secondary)
                    Rectangle().frame(height: 1).foregroundStyle(theme.border)
                }
            }
            if hasGoogle || hasGitHub {
                VStack(spacing: 8) {
                    if hasGoogle {
                        Button(action: { runReauth(.google) }) {
                            HStack {
                                GoogleIcon(size: 16)
                                Text("Continue with Google")
                            }
                            .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.large)
                        .disabled(submitting)
                    }
                    if hasGitHub {
                        Button(action: { runReauth(.github) }) {
                            HStack {
                                Image("GitHubLogo")
                                    .resizable()
                                    .aspectRatio(contentMode: .fit)
                                    .frame(width: 16, height: 16)
                                Text("Continue with GitHub")
                            }
                            .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.large)
                        .disabled(submitting)
                    }
                }
            }
            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
    }

    private var passwordRow: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Password")
                .font(theme.fieldLabelFont)
            SecureField("", text: $password)
                .textFieldStyle(.roundedBorder)
                .textContentType(.password)
                .onSubmit { runReauth(.password) }
            Button("Continue") { runReauth(.password) }
                .keyboardShortcut(.defaultAction)
                .controlSize(.large)
                .buttonStyle(.borderedProminent)
                .frame(maxWidth: .infinity)
                .disabled(password.isEmpty || submitting)
                .padding(.top, 4)
        }
    }

    // MARK: - Stage 2: second factor (TOTP)

    private var secondFactorContent: some View {
        VStack(alignment: .leading, spacing: 16) {
            OTPSegmentedField(
                value: $otpCode, length: 6, onComplete: completeSecondFactor)
            HStack {
                if submitting { ProgressView().controlSize(.small) }
                Spacer()
            }
            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
    }

    // MARK: - Actions

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
            } catch ProfileError.requiresSecondFactor {
                // First-factor accepted, but the account is MFA-
                // enrolled. AuthService has stashed the resolver;
                // hand off to the OTP stage.
                stage = .secondFactor
                errorMessage = nil
                otpCode = ""
            } catch let error as ProfileError {
                errorMessage = error.userMessage
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    private func completeSecondFactor() {
        guard otpCode.count == 6 else { return }
        submitting = true
        errorMessage = nil
        Task {
            defer { submitting = false }
            do {
                try await auth.completeReauthSecondFactor(code: otpCode)
                onReauthenticated()
            } catch let error as ProfileError {
                errorMessage = error.userMessage
                otpCode = ""
            } catch {
                errorMessage = error.localizedDescription
                otpCode = ""
            }
        }
    }

    /// Back button: in the OTP stage, returns to first-factor and
    /// drops the pending resolver. In first-factor stage, fully
    /// cancels the reauth.
    private func backOrCancel() {
        if stage == .secondFactor {
            auth.cancelReauthSecondFactor()
            otpCode = ""
            errorMessage = nil
            stage = .firstFactor
        } else {
            onCancel()
        }
    }
}

// MARK: - TOTP enrollment wizard

/// Full-surface takeover for TOTP enrollment. Left: QR + manual
/// secret for authenticator apps. Right: 6-digit code entry.
/// Cancel / back dismisses back to the main Security view; on
/// successful verification the parent clears the enrollment
/// context, which pops us back to the main view.
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
                OTPSegmentedField(value: $code, length: 6, onComplete: verify)
            }
            HStack {
                Button("Cancel", role: .cancel, action: onCancel)
                    .controlSize(.regular)
                Spacer()
                if submitting {
                    ProgressView().controlSize(.small)
                }
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
                code = ""
            } catch {
                errorMessage = error.localizedDescription
                code = ""
            }
        }
    }
}
