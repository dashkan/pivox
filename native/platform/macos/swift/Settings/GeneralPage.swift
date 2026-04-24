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
