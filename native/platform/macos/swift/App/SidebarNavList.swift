import SwiftUI

/// Native macOS source-list sidebar. `List(selection:)` with
/// `.listStyle(.sidebar)` bridges to NSOutlineView under the hood,
/// which gives us for free:
///   - Selection styling that matches the system (gray pill + accent
///     icon/text on macOS 26)
///   - Row sizing that follows System Settings → Appearance →
///     Sidebar icon size (Small / Medium / Large)
///   - Native arrow-key navigation + Tab-in / Tab-out behaviour
///   - Accessibility tree as a List rather than a soup of Buttons
///
/// We tried custom Button-based rows previously to get tighter visual
/// control, but lost system sizing and keyboard nav. The platform
/// already ships what we want — just use it.
struct SidebarNavList: View {
    @Binding var selectedItem: SidebarItem?

    var body: some View {
        List(selection: $selectedItem) {
            ForEach(AppSection.allCases) { section in
                Label(section.rawValue, systemImage: section.icon)
                    // SwiftUI's List(.sidebar) doesn't follow the
                    // system's Sidebar icon size preference on
                    // macOS 26 — icons render too small at Medium.
                    // Bump them manually so the sidebar reads at
                    // parity with Music / Mail. If the user changes
                    // the system setting we won't respond; a fuller
                    // fix requires reading
                    // `NSTableViewDefaultSizeMode` and picking
                    // metrics, which is TODO.
                    .imageScale(.large)
                    .tag(SidebarItem.section(section))
            }
        }
        .listStyle(.sidebar)
    }
}
