import AppKit
import SwiftUI

/// Container for a row of `IconButton` buttons beneath (or beside) a
/// message. Kept as a thin decomposition so the row can stay role-
/// specific on the Message component while the icon itself is a
/// shared primitive.
struct MessageActions<Content: View>: View {
    let content: Content

    init(@ViewBuilder _ content: () -> Content) {
        self.content = content()
    }

    var body: some View {
        HStack(spacing: 2) {
            content
        }
    }
}

/// Pasteboard helper — writes a plain-text string to the macOS clipboard.
enum MessagePasteboard {
    static func copy(_ text: String) {
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString(text, forType: .string)
    }
}

#if DEBUG

/// The wrapper itself is trivial; the value is showing the row in
/// realistic context — IconButtons styled for inline-row use
/// (`showsHoverBackground: false`) so the spacing reads correctly.

#Preview("Assistant message row") {
    MessageActions {
        IconButton(systemName: "doc.on.doc",
                   label: "Copy",
                   showsHoverBackground: false,
                   action: {})
        IconButton(systemName: "arrow.clockwise",
                   label: "Regenerate",
                   showsHoverBackground: false,
                   action: {})
        IconButton(systemName: "hand.thumbsup",
                   label: "Helpful",
                   showsHoverBackground: false,
                   action: {})
        IconButton(systemName: "hand.thumbsdown",
                   label: "Not helpful",
                   showsHoverBackground: false,
                   action: {})
    }
    .padding()
}

#Preview("Latched feedback (helpful)") {
    MessageActions {
        IconButton(systemName: "doc.on.doc",
                   label: "Copy",
                   showsHoverBackground: false,
                   action: {})
        IconButton(systemName: "hand.thumbsup.fill",
                   label: "Helpful",
                   isOn: true,
                   showsHoverBackground: false,
                   action: {})
    }
    .padding()
}

#endif
