import SwiftUI

/// Onboarding screen shown to a freshly-registered (or memberless)
/// user. Visually mirrors `RegisterView` / `LoginView` — same accent
/// backdrop, same glass card, same typography — so the transition
/// from "create account" to "create your organization" feels like a
/// continuous step in the same flow rather than a separate screen.
struct CreateOrgView: View {
  private let auth = AuthService.shared
  private let orgs = OrgService.shared

  @Environment(\.pivoxTheme) private var theme
  @State private var displayName = ""
  @State private var organizationID = ""
  /// `true` when the user has typed in the slug field manually. As
  /// long as it's `false`, the slug auto-derives from `displayName`
  /// — same UX as Slack workspaces / Notion teams.
  @State private var slugTouched = false
  @State private var isLoading = false
  @State private var errorMessage: String?
  @FocusState private var focusedField: Field?

  private enum Field: Hashable { case shortName, displayName }
  /// Mirrors the auth views' shared toggle so ⌘⇧G flips the glass
  /// treatment everywhere consistently.
  @AppStorage("debug.auth.glass_card") private var useGlassCard: Bool = true

  var body: some View {
    VStack(spacing: 0) {
      Spacer()

      authCard
        .padding(.horizontal, 40)

      Spacer()
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
    .background(authBackdrop.ignoresSafeArea())
    .background(glassToggleShortcut)
    .onAppear { focusedField = .displayName }
  }

  private var authBackdrop: some View {
    ZStack {
      theme.background
      RadialGradient(
        colors: [theme.accent.opacity(0.28), .clear],
        center: .topLeading,
        startRadius: 0,
        endRadius: 520)
      RadialGradient(
        colors: [theme.accent.opacity(0.18), .clear],
        center: .bottomTrailing,
        startRadius: 0,
        endRadius: 620)
    }
  }

  private var glassToggleShortcut: some View {
    Button {
      useGlassCard.toggle()
    } label: { EmptyView() }
      .keyboardShortcut("g", modifiers: [.command, .shift])
      .buttonStyle(.plain)
      .frame(width: 0, height: 0)
      .opacity(0)
      .accessibilityHidden(true)
      .focusable(false)
  }

  private var authCard: some View {
    VStack(spacing: 24) {
      // Mirrors LoginView/RegisterView's fixed-height upper section
      // so the visual rhythm matches when the user moves between
      // these screens during onboarding.
      VStack(spacing: 24) {
        // Header
        VStack(spacing: 8) {
          Text("Pivox")
            .font(theme.brandTitleFont)
          Text("Create your organization")
            .font(theme.bodyFont)
            .foregroundStyle(.secondary)
        }

        // Form
        VStack(spacing: 16) {
          // Custom binding so the setter only fires on user input —
          // programmatic updates from the slugify path don't flip
          // `slugTouched`, which would otherwise lock auto-derivation
          // after the very first character. If the user clears the
          // field we reset to auto so it resumes mirroring the name.
          TextField("Short name", text: Binding(
            get: { organizationID },
            set: { newValue in
              organizationID = newValue
              slugTouched = !newValue.isEmpty
            }
          ))
            .textFieldStyle(.roundedBorder)
            .disabled(isLoading)
            .focused($focusedField, equals: .shortName)
            .accessibilityIdentifier("create-org-id")

          Text(slugHint)
            .font(theme.bodySmallFont)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)

          TextField("Organization name", text: $displayName)
            .textFieldStyle(.roundedBorder)
            .textContentType(.organizationName)
            .disabled(isLoading)
            .focused($focusedField, equals: .displayName)
            .accessibilityIdentifier("create-org-display-name")
            .onChange(of: displayName) { _, newValue in
              if !slugTouched {
                organizationID = Self.slugify(newValue)
              }
            }

          AuthPrimaryButton("Create Organization", isLoading: isLoading) {
            submit()
          }
          .disabled(!isFormValid || isLoading)
          .accessibilityIdentifier("create-org-submit")

          // Error message — pre-allocated space to prevent layout shift.
          Text(errorMessage ?? " ")
            .font(theme.bodyFont)
            .foregroundStyle(theme.destructive)
            .multilineTextAlignment(.center)
            .opacity(errorMessage != nil ? 1 : 0)
            .accessibilityIdentifier("create-org-error")
        }

        Spacer(minLength: 12)
      }
      .frame(height: 320)

      // Footer — escape hatch back to sign-out for users who landed
      // here by accident or want to switch accounts.
      HStack(spacing: 4) {
        Text("Wrong account?")
          .font(theme.bodyFont)
          .foregroundStyle(.secondary)
        Button("Sign out") {
          auth.signOut()
        }
        .buttonStyle(.link)
        .font(theme.bodyFont)
        .accessibilityIdentifier("create-org-sign-out")
      }
    }
    .padding(32)
    .frame(maxWidth: 400)
    .glassCardIfEnabled(useGlassCard)
  }

  private var isFormValid: Bool {
    !displayName.trimmingCharacters(in: .whitespaces).isEmpty
      && Self.isValidSlug(organizationID)
  }

  /// Pattern is enforced server-side by buf-validate
  /// (`^[a-z][a-z0-9-]{3,19}$`). Mirroring it here keeps the submit
  /// button disabled until the slug is at least syntactically valid,
  /// so we don't round-trip a guaranteed `InvalidArgument`.
  private var slugHint: String {
    "Permanent. 4–20 characters · lowercase letters, numbers, hyphens · must start with a letter."
  }

  private func submit() {
    errorMessage = nil
    isLoading = true
    Task {
      do {
        try await orgs.create(
          displayName: displayName.trimmingCharacters(in: .whitespaces),
          organizationID: organizationID
        )
      } catch let err as OrgsClientError {
        errorMessage = err.description
      } catch let err as ChatClientError {
        errorMessage = err.description
      } catch {
        errorMessage = "Couldn't create organization. Try again."
      }
      isLoading = false
    }
  }

  // MARK: - Slug helpers

  private static func slugify(_ s: String) -> String {
    let lowered = s.lowercased()
    var out = ""
    var lastWasHyphen = false
    for ch in lowered {
      if ch.isLetter || ch.isNumber {
        out.append(ch)
        lastWasHyphen = false
      } else if ch.isWhitespace || ch == "-" || ch == "_" {
        if !lastWasHyphen, !out.isEmpty {
          out.append("-")
          lastWasHyphen = true
        }
      }
    }
    while out.hasSuffix("-") { out.removeLast() }
    return String(out.prefix(20))
  }

  private static func isValidSlug(_ s: String) -> Bool {
    guard (4...20).contains(s.count) else { return false }
    guard let first = s.first, first.isLetter, first.isLowercase else { return false }
    return s.allSatisfy { ch in
      (ch.isLetter && ch.isLowercase) || ch.isNumber || ch == "-"
    }
  }
}
