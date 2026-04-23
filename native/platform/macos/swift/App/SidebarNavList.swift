import SwiftUI

/// Native macOS source-list sidebar. `List(selection:)` with
/// `.listStyle(.sidebar)` bridges to `NSOutlineView` under the hood.
///
/// Each row is an explicit `HStack { Image; Text }` rather than
/// `Label(_, systemImage:)`. On macOS 26 Liquid Glass, `Label` inside
/// a sidebar-style List inherits the row's internal icon/text layout
/// and renders with content clipped from the left at certain widths —
/// visible as "or" showing for "Operator", etc. An explicit HStack
/// gives us predictable, non-clipping layout regardless of what
/// NavigationSplitView is doing with column widths.
struct SidebarNavList: View {
    @Binding var selectedItem: SidebarItem?

    var body: some View {
        List(selection: $selectedItem) {
            ForEach(AppSection.allCases) { section in
                HStack(spacing: 8) {
                    Image(systemName: section.icon)
                        .font(.body)
                        .frame(width: 20)
                    Text(section.rawValue)
                }
                .tag(SidebarItem.section(section))
            }
        }
        .listStyle(.sidebar)
    }
}
