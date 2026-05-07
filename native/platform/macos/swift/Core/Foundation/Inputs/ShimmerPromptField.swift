import AppKit
import SwiftUI

/// Multi-line chat composer wearing the AI-Intelligence shimmer
/// border. Modeled on Claude in VS Code / ChatGPT desktop:
///
///   - Auto-growing text area starts at one line and grows up to
///     ten as the user types (or pastes). Past ten lines the
///     field scrolls internally rather than pushing the rest of
///     the chat UI offscreen.
///   - A tool row below the text area reserves space for inline
///     controls (attachments, slash commands, file context,
///     permission mode, etc.) plus the primary send/stop button
///     on the right. Caller-supplied buttons go in the leading
///     slot via a `@ViewBuilder`; the primary button is owned by
///     the composer so its send-vs-stop state stays in sync with
///     the streaming flag.
///
/// **Submit semantics:** Return submits. Option+Return inserts a
/// newline (the native macOS convention — `insertNewlineIgnoringFieldEditor:`
/// is bound to Option+Return in NSResponder defaults; that's what
/// Mail and TextEdit use). Shift+Return is also accepted as a
/// fallback for users coming from Slack / ChatGPT / Discord which
/// borrowed the Windows convention, but in SwiftUI's
/// `TextField(axis: .vertical)` Shift+Return can be intercepted by
/// the underlying NSText's "extend selection" handler before
/// reaching us, so it's not 100% reliable. Option+Return is the
/// recommended path and what the inline hint shows.
///
/// We intercept Return ourselves via `.onKeyPress` because
/// `axis: .vertical`'s default is to insert a newline on Return
/// and never fire `.onSubmit` — the wrong default for a chat
/// composer where the dominant intent is "send" not "newline."
///
/// Callers inject their own `@FocusState` via `FocusState.Binding`
/// so they can still drive focus programmatically (e.g. the
/// `⌘⇧A` hotkey focusing the chat input when it opens the panel).
struct ShimmerPromptField<ToolItems: View>: View {
    @Binding var text: String
    let placeholder: String
    let isEnabled: Bool
    /// When true, the primary button shows a stop glyph and calls
    /// `onCancel`. When false, it shows the send arrow and calls
    /// `onSubmit` — gated on the field having non-whitespace text.
    let isStreaming: Bool
    let onSubmit: () -> Void
    let onCancel: () -> Void
    var focused: FocusState<Bool>.Binding
    @ViewBuilder var toolItems: () -> ToolItems

    @Environment(\.pivoxTheme) private var theme

    private let cornerRadius: CGFloat = 14

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacingSM) {
            TextField(placeholder, text: $text, axis: .vertical)
                .textFieldStyle(.plain)
                .lineLimit(1...10)
                .focused(focused)
                .disabled(!isEnabled)
                // Match the tool row's intrinsic height (driven by
                // `IconButton`'s 32pt hit target) so the divider
                // sits at the vertical midpoint of the composer
                // instead of clinging to the top. The text still
                // renders at its natural height — `topLeading`
                // alignment pins it to the top of the reserved
                // 32pt area.
                .frame(minHeight: 32, alignment: .topLeading)
                // Intercept Return to implement chat-app submit
                // semantics. Without this, `axis: .vertical` makes
                // Return insert a newline and `.onSubmit` never
                // fires — the wrong default for a chat composer
                // where the dominant intent is "send" not "newline."
                .onKeyPress(.return, phases: .down) { press in
                    // IME safety: while a CJK/etc. input method has
                    // marked (uncommitted) text in its candidate
                    // window, Return must commit the candidate
                    // rather than fire submit. Without this guard a
                    // user composing 你好 with the IME open and
                    // pressing Return to confirm would instead send
                    // a partial pinyin message.
                    if let ctx = NSTextInputContext.current,
                       ctx.client.hasMarkedText() {
                        return .ignored
                    }
                    // Use the modifiers attached to THIS event
                    // (`press.modifiers`) instead of polling
                    // `NSEvent.modifierFlags`. The global state can
                    // misread on rapid modifier transitions —
                    // SwiftUI sometimes coalesces key events across
                    // transitions on Apple Silicon.
                    if press.modifiers.contains(.shift)
                        || press.modifiers.contains(.option) {
                        return .ignored  // let the field insert a newline
                    }
                    if canSend {
                        onSubmit()
                    }
                    return .handled
                }
                // Esc-to-cancel scoped to the focused TextField.
                // This wins the responder race against AppKit's
                // default `cancelOperation:` (which would otherwise
                // clear autocomplete / dismiss IME marked text)
                // ONLY when there's an active stream worth
                // cancelling. When not streaming we return
                // `.ignored` so the AppKit default handles Esc
                // normally — preserves field-editor cancel
                // semantics for users not in mid-stream.
                .onKeyPress(.escape) {
                    if isStreaming {
                        onCancel()
                        return .handled
                    }
                    return .ignored
                }

            // Hairline separator between the message area and the
            // tool row. Spans the full content width so its ends
            // align with the text's leading edge (top row) and the
            // send button's trailing edge (bottom row). The VStack's
            // `spacingSM` provides breathing room above and below.
            Divider()

            HStack(spacing: theme.spacingXS) {
                toolItems()
                Spacer(minLength: 0)
                // Inline keyboard hint. Sits right next to the
                // primary button it explains. Tertiary color +
                // caption size so it reads as ambient
                // documentation, not active UI. Discoverable on
                // first use, ignorable afterwards — matches the
                // "hint instead of setting" decision (vs. a
                // user-configurable shortcut, which fights the
                // chat-app convention).
                Text("↩ Send  ·  ⌥↩ New line")
                    .font(theme.bodySmallFont)
                    .foregroundStyle(theme.textSecondary)
                    .lineLimit(1)
                    .layoutPriority(0)
                primaryButton
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(
            RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                .fill(.thinMaterial)
        )
        .aiShimmer(
            shape: RoundedRectangle(cornerRadius: cornerRadius, style: .continuous),
            // Ambient glow at rest (~35%), stronger when focused.
            // The shift happens via implicit animation on intensity.
            intensity: focused.wrappedValue ? 0.95 : 0.35,
            lineWidth: 1.5
        )
        .animation(.easeInOut(duration: 0.25), value: focused.wrappedValue)
    }

    @ViewBuilder
    private var primaryButton: some View {
        if isStreaming {
            // Stop = destructive role. The IconButton resolves
            // `.destructive` to `theme.destructive` (red), giving
            // the stop glyph an unmistakable "interrupt now" tone
            // without us having to hand-paint colors here.
            IconButton(
                systemName: "stop.circle.fill",
                label: "Stop generating",
                help: "Stop",
                role: .destructive
            ) {
                onCancel()
            }
        } else {
            // `isOn: canSend` flips the IconButton foreground from
            // `theme.textSecondary` (no text or disabled field) to
            // `theme.accent` (ready to send). The color flip
            // doubles as a state indicator and matches `.disabled`
            // — both are driven by `canSend`.
            IconButton(
                systemName: "arrow.up.circle.fill",
                label: "Send message",
                help: "Send",
                isOn: canSend
            ) {
                onSubmit()
            }
            .disabled(!canSend)
        }
    }

    private var canSend: Bool {
        isEnabled && !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

extension ShimmerPromptField where ToolItems == EmptyView {
    /// Convenience init for callers that don't yet have tool
    /// buttons to show. The tool row collapses to just the
    /// send/stop button.
    init(
        text: Binding<String>,
        placeholder: String,
        isEnabled: Bool,
        isStreaming: Bool,
        onSubmit: @escaping () -> Void,
        onCancel: @escaping () -> Void,
        focused: FocusState<Bool>.Binding
    ) {
        self.init(
            text: text,
            placeholder: placeholder,
            isEnabled: isEnabled,
            isStreaming: isStreaming,
            onSubmit: onSubmit,
            onCancel: onCancel,
            focused: focused,
            toolItems: { EmptyView() })
    }
}

#if DEBUG

/// Host wrapper that owns the @FocusState binding and seeds the
/// text @State so each `#Preview` can declare a different starting
/// state. The shimmer animates in Previews same as production
/// (TimelineView-driven, not @State).
private struct PreviewShimmerHost: View {
    @State var text: String
    @FocusState var focused: Bool
    let isStreaming: Bool
    let isEnabled: Bool

    init(text: String = "",
         isStreaming: Bool = false,
         isEnabled: Bool = true) {
        self._text = State(initialValue: text)
        self.isStreaming = isStreaming
        self.isEnabled = isEnabled
    }

    var body: some View {
        ShimmerPromptField(
            text: $text,
            placeholder: "Ask anything…",
            isEnabled: isEnabled,
            isStreaming: isStreaming,
            onSubmit: {},
            onCancel: {},
            focused: $focused
        )
        .padding()
        .frame(width: 480)
    }
}

#Preview("Empty — placeholder visible") {
    PreviewShimmerHost()
}

#Preview("Filled — single line") {
    PreviewShimmerHost(text: "Help me draft a release announcement.")
}

#Preview("Filled — multi-line, near auto-grow ceiling") {
    PreviewShimmerHost(text: """
    Lorem ipsum dolor sit amet, consectetur adipiscing elit. \
    Sed do eiusmod tempor incididunt ut labore et dolore magna \
    aliqua. Ut enim ad minim veniam, quis nostrud exercitation \
    ullamco laboris nisi ut aliquip ex ea commodo consequat.
    """)
}

#Preview("Streaming — stop button") {
    PreviewShimmerHost(
        text: "Generating a long response right now…",
        isStreaming: true)
}

#Preview("Disabled — offline / mid-disconnect") {
    PreviewShimmerHost(
        text: "Type here when you reconnect",
        isEnabled: false)
}

#endif
