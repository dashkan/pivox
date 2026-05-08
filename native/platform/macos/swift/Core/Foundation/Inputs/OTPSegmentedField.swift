import AppKit
import SwiftUI

/// Fixed-length numeric OTP entry rendered as a row of glyph cells —
/// the familiar one-time-code pattern Apple uses in its own 2FA
/// prompts. Typing auto-advances, backspace steps back, pasting a
/// 6-digit string fills all cells at once.
///
/// ## Architecture
/// One hidden AppKit `NSTextField` (wrapped as `CaretlessOTPInput`
/// below) owns the entire input and text focus; the visible cells
/// are pure render over `value`. An earlier attempt gave each cell
/// its own `TextField` and synced them, which created a feedback
/// loop — updating one cell's text triggered every cell's
/// `onChange`, which re-triggered the input handler, which moved
/// focus somewhere unexpected. A single source of truth sidesteps
/// that.
///
/// The field is AppKit-backed (vs SwiftUI's TextField) so the input
/// can suppress double/triple-click word/all selection — SwiftUI's
/// TextField surfaces a visible selection swatch on those gestures
/// (the hidden text becomes briefly visible against the highlight
/// color). NSTextField subclassing lets us eat multi-click events
/// before AppKit's word-select runs. See `CaretlessTextField`.
///
/// ## Callbacks
/// `onComplete` fires as soon as `value` reaches `length`. The
/// parent typically uses it to auto-submit so the user doesn't have
/// to click Verify after typing the last digit — matches what Apple
/// does in its own prompts. `onSubmit` on the underlying field also
/// routes through `onComplete` so Enter-to-submit works even when
/// the value is mid-entry (verify rejects short codes anyway).
struct OTPSegmentedField: View {
    @Binding var value: String
    var length: Int = 6
    var onComplete: (() -> Void)? = nil

    @Environment(\.pivoxTheme) private var theme
    /// Mirrors NSTextField's first-responder state, written by the
    /// representable's coordinator on begin/end editing. Drives the
    /// active-cell highlight (purple border on the next-to-fill
    /// cell while the field has focus).
    @State private var fieldHasFocus: Bool = false

    var body: some View {
        ZStack {
            // Visible glyph row. Sits behind the transparent input
            // field and ignores hit testing so taps fall through to
            // the input, giving the field focus.
            HStack(spacing: 8) {
                ForEach(0..<length, id: \.self) { index in
                    cell(at: index)
                }
            }
            .allowsHitTesting(false)

            // Invisible AppKit-backed input. Captures typing,
            // paste, password-manager autofill (.oneTimeCode),
            // suppresses caret + selection bleed, eats multi-click.
            CaretlessOTPInput(
                text: $value,
                hasFocus: $fieldHasFocus,
                onCommit: { onComplete?() }
            )
            // NSTextField sizes to its text by default — for an
            // empty string that's a tiny hit target floating in
            // the middle of the cells. Stretch to fill so taps
            // anywhere across the row land on the field.
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .onChange(of: value) { oldValue, newValue in
                // Paste vs. single keystroke. `count` jump > 1
                // means the user pasted (or did a bulk input);
                // we take the *last* `length` digits so a
                // pasted code wins even if the field already
                // had partial input — matches Apple's own 2FA
                // prompt behavior. For single-character changes
                // (typing or deletion) we take the *first*
                // `length` digits so typing past the end is
                // discarded rather than rotating previous
                // digits out of the field.
                let all = newValue.filter(\.isNumber)
                let digits: String
                if newValue.count > oldValue.count + 1 {
                    digits = String(all.suffix(length))
                } else {
                    digits = String(all.prefix(length))
                }
                if digits != newValue {
                    value = digits
                }
                if digits.count == length {
                    onComplete?()
                }
            }
        }
        .frame(height: 52)
    }

    @ViewBuilder
    private func cell(at index: Int) -> some View {
        let glyph = character(at: index)
        // Highlight the cell that will receive the next keystroke —
        // the cell immediately after the last entered digit. Once
        // all cells are filled, highlight the last one.
        let cursorAt = min(value.count, length - 1)
        let isFocusedCell = fieldHasFocus && index == cursorAt
        ZStack {
            RoundedRectangle(cornerRadius: 8)
                .fill(theme.backgroundRaised)
            RoundedRectangle(cornerRadius: 8)
                .strokeBorder(
                    isFocusedCell ? Color.accentColor : Color.secondary.opacity(0.35),
                    lineWidth: isFocusedCell ? 2 : 1)
            Text(String(glyph ?? " "))
                .font(.system(size: 24, weight: .medium, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(.primary)
        }
        .frame(width: 44, height: 52)
    }

    private func character(at index: Int) -> Character? {
        guard index < value.count else { return nil }
        return value[value.index(value.startIndex, offsetBy: index)]
    }
}

// MARK: - AppKit-backed invisible input

/// AppKit-backed hidden text input. Captures typing + paste +
/// `.oneTimeCode` autofill while suppressing the visible artifacts
/// SwiftUI's TextField surfaces on macOS:
///
///  - **Caret** — `insertionPointColor = .clear` on the field
///    editor (`NSTextView`).
///  - **Selection swatch + selected-text rendering** — clear values
///    in `selectedTextAttributes` on the same field editor.
///  - **Double/triple-click word/all selection** — `mouseDown`
///    returns early on `clickCount > 1`, eating the event before
///    AppKit's word-select runs.
///
/// What this DOES NOT change vs. AppKit defaults:
///
///  - Single-click → first responder + place cursor (default
///    `mouseDown` flows through unchanged).
///  - `Tab` → moves to next responder in the form (NSTextField's
///    native key-view-loop behavior; segment-internal Tab is not
///    a thing in the platform convention).
///  - Auto-focus on appear via `viewDidMoveToWindow`.
private struct CaretlessOTPInput: NSViewRepresentable {
    @Binding var text: String
    @Binding var hasFocus: Bool
    let onCommit: () -> Void

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSTextField {
        let field = CaretlessTextField(string: "")
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.alignment = .center
        // Type-clear keeps any visible string invisible until the
        // SwiftUI binding propagates the value into the cells.
        field.textColor = .clear
        // The autofill hook — without this NSTextField reads as a
        // generic text input and password managers (1Password,
        // iCloud Keychain) don't offer to fill the SMS code.
        field.contentType = .oneTimeCode
        field.delegate = context.coordinator
        // NSTextField's intrinsic size is text-driven (empty string
        // = small). Lower hugging so SwiftUI's
        // `.frame(maxWidth: .infinity)` actually stretches the
        // hit target across the cell row.
        field.setContentHuggingPriority(.defaultLow, for: .horizontal)
        field.setContentHuggingPriority(.defaultLow, for: .vertical)
        return field
    }

    func updateNSView(_ field: NSTextField, context: Context) {
        // String binding sync. Compare to avoid re-setting the
        // value on every state mirror tick (would clobber the
        // field's selection / cursor position).
        if field.stringValue != text {
            field.stringValue = text
        }
    }

    final class Coordinator: NSObject, NSTextFieldDelegate {
        let parent: CaretlessOTPInput

        init(_ parent: CaretlessOTPInput) {
            self.parent = parent
        }

        override func controlTextDidChange(_ notification: Notification) {
            guard let field = notification.object as? NSTextField else { return }
            parent.text = field.stringValue
        }

        override func controlTextDidBeginEditing(_ notification: Notification) {
            // Mirror NSTextField focus state into SwiftUI so the
            // active-cell highlight tracks first-responder
            // transitions (click-to-focus, viewDidMoveToWindow
            // auto-focus, Tab-in from another control). Async
            // because we're inside an AppKit notification callback
            // and SwiftUI state writes have to land on a fresh
            // render pass.
            DispatchQueue.main.async { [weak self] in
                self?.parent.hasFocus = true
            }
        }

        override func controlTextDidEndEditing(_ notification: Notification) {
            DispatchQueue.main.async { [weak self] in
                self?.parent.hasFocus = false
            }
        }

        func control(_ control: NSControl,
                     textView: NSTextView,
                     doCommandBy selector: Selector) -> Bool {
            // Return → submit. AppKit's default `insertNewline:`
            // would just insert a newline and ignore the parent's
            // intent to auto-advance.
            if selector == #selector(NSResponder.insertNewline(_:)) {
                parent.onCommit()
                return true
            }
            return false
        }
    }
}

/// `NSTextField` subclass that hides the field editor's caret and
/// selection rendering, suppresses multi-click selection bleed, and
/// auto-focuses when added to a window. The field editor (an
/// `NSTextView`) only exists while focused, so the visual config
/// has to live in `becomeFirstResponder`, not init.
private final class CaretlessTextField: NSTextField {
    override func becomeFirstResponder() -> Bool {
        let didBecome = super.becomeFirstResponder()
        if didBecome, let editor = currentEditor() as? NSTextView {
            editor.insertionPointColor = .clear
            // `textColor = .clear` on the field handles unselected
            // text; selection is rendered via a SEPARATE attribute
            // dictionary on the field editor, so we have to clear
            // both keys explicitly. Without this, cmd-A or any
            // selection extends a visible swatch + foreground.
            editor.selectedTextAttributes = [
                .foregroundColor: NSColor.clear,
                .backgroundColor: NSColor.clear,
            ]
        }
        return didBecome
    }

    override func mouseDown(with event: NSEvent) {
        // Eat double/triple-clicks before AppKit's word-select /
        // select-all handlers run. Single clicks flow through to
        // super so the default click-to-focus + cursor-at-click
        // behavior stays intact (this is the bit that broke last
        // time when we end-snapped on every click).
        if event.clickCount > 1 { return }
        super.mouseDown(with: event)
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        // Auto-focus parity with the previous SwiftUI shape's
        // `.onAppear { focused = true }`. Async because
        // `makeFirstResponder` during the same run-loop turn the
        // view joins the window can no-op silently.
        guard window != nil else { return }
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.window?.makeFirstResponder(self)
        }
    }
}

#if DEBUG

/// Each preview pins `value` to a different fill state so the visual
/// language of empty / partial / full reads at a glance. Focus and
/// auto-advance behavior need a running app to validate (Previews
/// don't simulate keystrokes).

#Preview("Empty") {
    OTPSegmentedField(value: .constant(""))
        .padding()
}

#Preview("Partial — 3 of 6") {
    OTPSegmentedField(value: .constant("123"))
        .padding()
}

#Preview("Full") {
    OTPSegmentedField(value: .constant("123456"))
        .padding()
}

#Preview("Custom length — 4") {
    OTPSegmentedField(value: .constant("42"), length: 4)
        .padding()
}

#endif
