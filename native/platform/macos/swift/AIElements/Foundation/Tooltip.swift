import SwiftUI

/// A tooltip modifier that shows content on hover after a short delay.
/// Uses NSPopover for native macOS tooltip behavior.
struct AITooltipModifier<TooltipContent: View>: ViewModifier {
    let content: TooltipContent
    let shortcut: String?
    let side: Edge

    @State private var isShowing = false
    @State private var hoverTask: Task<Void, Never>?

    func body(content: Content) -> some View {
        content
            .onHover { hovering in
                if hovering {
                    hoverTask = Task {
                        try? await Task.sleep(for: .milliseconds(500))
                        guard !Task.isCancelled else { return }
                        isShowing = true
                    }
                } else {
                    hoverTask?.cancel()
                    hoverTask = nil
                    isShowing = false
                }
            }
            .popover(isPresented: $isShowing, arrowEdge: side) {
                HStack(spacing: 6) {
                    self.content
                    if let shortcut {
                        Text(shortcut)
                            .font(.caption2)
                            .padding(.horizontal, 4)
                            .padding(.vertical, 2)
                            .background(.quaternary)
                            .clipShape(RoundedRectangle(cornerRadius: 3))
                    }
                }
                .padding(8)
            }
    }
}

extension View {
    /// Attaches an AIElements tooltip that appears on hover.
    public func aiTooltip<Content: View>(
        @ViewBuilder content: () -> Content,
        shortcut: String? = nil,
        side: Edge = .bottom
    ) -> some View {
        modifier(AITooltipModifier(
            content: content(),
            shortcut: shortcut,
            side: side
        ))
    }

    /// Convenience: text-only tooltip.
    public func aiTooltip(
        _ text: String,
        shortcut: String? = nil,
        side: Edge = .bottom
    ) -> some View {
        modifier(AITooltipModifier(
            content: Text(text).font(.caption),
            shortcut: shortcut,
            side: side
        ))
    }
}
