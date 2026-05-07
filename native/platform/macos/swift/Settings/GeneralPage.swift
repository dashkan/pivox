import SwiftUI

/// Placeholder General tab — the default landing page for ⌘,.
/// Not empty on purpose: if the user hits Settings without a
/// specific tab in mind, they should see *something*, not a blank
/// surface that reads as broken. Real controls slot in here as the
/// app grows (appearance, notifications, updates, telemetry, etc.).
struct GeneralPage: View {
    @Environment(\.pivoxTheme) private var theme

    var body: some View {
        Form {
            Section {
                LabeledContent("Version") {
                    Text(Bundle.main.appVersionDisplay)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
                LabeledContent("Build") {
                    Text(Bundle.main.appBuildNumber)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
            } header: {
                Text("About")
            }
        }
        .formStyle(.grouped)
        .frame(width: 640)
    }
}

private extension Bundle {
    var appVersionDisplay: String {
        (infoDictionary?["CFBundleShortVersionString"] as? String) ?? "—"
    }
    var appBuildNumber: String {
        (infoDictionary?["CFBundleVersion"] as? String) ?? "—"
    }
}

#if DEBUG

/// In Previews, `Bundle.main` is the previewing process bundle,
/// not Pivox.app — so version + build display the placeholder dash.
/// That's still useful: it confirms the placeholder flows through
/// the LabeledContent + textSelection chain. To see real values,
/// run the app and open Settings > General.

#Preview("Default") {
    GeneralPage()
}

#endif
