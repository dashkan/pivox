import AppKit
import SwiftUI

/// Fixed-length numeric OTP entry rendered as a row of glyph cells —
/// the familiar one-time-code pattern Apple uses in its own 2FA
/// prompts. Typing auto-advances, backspace steps back, pasting a
/// 6-digit string fills all cells at once.
///
/// ## Architecture
/// One hidden `NSTextField` (wrapped as `CaretlessOTPInput` below)
/// owns the entire input and text focus; the visible cells are pure
/// render over `value`. An earlier attempt gave each cell its own
/// `TextField` and synced them, which created a feedback loop —
/// updating one cell's text triggered every cell's `onChange`,
/// which re-triggered the input handler, which moved focus
/// somewhere unexpected. A single source of truth sidesteps that.
///
/// The field has to be AppKit-backed (vs SwiftUI's TextField) so
/// the blinking insertion caret can be cleared. SwiftUI's
/// `.tint(.clear)` does NOT suppress the caret on macOS for plain-
/// styled TextFields — see `CaretlessTextField` below.
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
    @FocusState private var focused: Bool

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

            // Invisible input field — captures typing, paste, and
            // backspace. The cells are the visible state; this
            // field is purely the keyboard / autofill target.
            //
            // Why a custom NSTextField wrap (vs SwiftUI's TextField
            // with `.foregroundStyle(.clear) + .tint(.clear)`):
            // SwiftUI's `.tint(.clear)` does NOT suppress the
            // blinking insertion caret on macOS for `.plain`-styled
            // TextFields — the caret is drawn by NSTextField's
            // field editor in a layer SwiftUI's tint doesn't reach.
            // Setting `insertionPointColor = .clear` directly on
            // the field editor (in `becomeFirstResponder`) is the
            // reliable path. `.oneTimeCode` autofill is preserved
            // via NSTextField's `contentType` property.
            CaretlessOTPInput(
                text: $value,
                isFocused: $focused,
                onCommit: { onComplete?() }
            )
            // NSTextField sizes to its text by default — for an
            // empty string that's a ~10×20 hit target floating
            // centered in the ZStack, leaving most of the cell row
            // unable to receive clicks. Stretch the wrap to fill so
            // taps anywhere across the OTP area land on the field.
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
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
        .onAppear { focused = true }
    }

    @ViewBuilder
    private func cell(at index: Int) -> some View {
        let glyph = character(at: index)
        // Highlight the cell that will receive the next keystroke —
        // the cell immediately after the last entered digit. Once
        // all cells are filled, highlight the last one.
        let cursorAt = min(value.count, length - 1)
        let isFocusedCell = focused && index == cursorAt
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

/// Hidden text input that captures typing + paste + password-manager
/// autofill for `OTPSegmentedField` without rendering visible
/// characters or a blinking insertion caret.
///
/// SwiftUI's `TextField(...).foregroundStyle(.clear).tint(.clear)`
/// almost works — the typed text goes invisible, but the caret
/// keeps blinking because NSTextField's field editor draws the
/// caret in a layer SwiftUI's `.tint` doesn't reach. Patching
/// `insertionPointColor = .clear` on the field editor itself, at
/// the AppKit boundary, is the reliable fix.
private struct CaretlessOTPInput: NSViewRepresentable {
    @Binding var text: String
    var isFocused: FocusState<Bool>.Binding
    let onCommit: () -> Void

    func makeCoordinator() -> Coordinator {
        Coordinator(parent: self)
    }

    func makeNSView(context: Context) -> NSTextField {
        let field = CaretlessTextField(string: "")
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.alignment = .center
        // Type-clear keeps autofilled values invisible until the
        // SwiftUI binding propagates them into the visible cells.
        field.textColor = .clear
        // The autofill hook — without this NSTextField reads as a
        // generic text input and password managers (1Password,
        // iCloud Keychain) don't offer to fill the code.
        field.contentType = .oneTimeCode
        field.delegate = context.coordinator
        // NSTextField's default intrinsic size is text-driven —
        // empty string = small. Lower hugging so SwiftUI's
        // `.frame(maxWidth: .infinity)` actually stretches the
        // hit target across the cell row.
        field.setContentHuggingPriority(.defaultLow, for: .horizontal)
        field.setContentHuggingPriority(.defaultLow, for: .vertical)
        return field
    }

    func updateNSView(_ field: NSTextField, context: Context) {
        // String-binding sync. Compare to avoid clobbering the
        // field's selection / cursor position when the upstream
        // binding emits the same value back to us.
        if field.stringValue != text {
            field.stringValue = text
        }
        // FocusState bridge: when SwiftUI says we should be
        // focused, become first responder. When SwiftUI says
        // we shouldn't, resign — but only if we're currently the
        // first responder, otherwise we'd clobber another view's
        // focus on every update tick.
        DispatchQueue.main.async {
            guard let window = field.window else { return }
            let isFirstResponder = (window.firstResponder == field)
                || (window.firstResponder == field.currentEditor())
            if isFocused.wrappedValue, !isFirstResponder {
                window.makeFirstResponder(field)
            } else if !isFocused.wrappedValue, isFirstResponder {
                window.makeFirstResponder(nil)
            }
        }
    }

    final class Coordinator: NSObject, NSTextFieldDelegate {
        let parent: CaretlessOTPInput

        init(parent: CaretlessOTPInput) {
            self.parent = parent
        }

        override func controlTextDidChange(_ notification: Notification) {
            guard let field = notification.object as? NSTextField else { return }
            parent.text = field.stringValue
        }

        override func controlTextDidBeginEditing(_ notification: Notification) {
            // Mirror responder state into FocusState so consumers
            // observing `@FocusState` see the focus arrive even when
            // it was triggered by a click rather than programmatic
            // makeFirstResponder.
            DispatchQueue.main.async { [weak self] in
                self?.parent.isFocused.wrappedValue = true
            }
        }

        override func controlTextDidEndEditing(_ notification: Notification) {
            DispatchQueue.main.async { [weak self] in
                self?.parent.isFocused.wrappedValue = false
            }
        }

        func control(_ control: NSControl,
                     textView: NSTextView,
                     doCommandBy selector: Selector) -> Bool {
            // Return → submit. AppKit's default for NSTextField
            // Return is `insertNewline:`; we intercept and route to
            // the SwiftUI `onCommit` so the parent can auto-submit
            // (matches Apple's own 2FA prompt behavior).
            if selector == #selector(NSResponder.insertNewline(_:)) {
                parent.onCommit()
                return true
            }
            return false
        }
    }
}

/// `NSTextField` subclass that hides every visual artifact a user
/// could see from the underlying input — caret, selection highlight,
/// selected-text rendering — and pins the cursor to end-of-text so
/// new digits always append rather than inserting mid-string.
///
/// The field editor (an `NSTextView`) only exists while the field is
/// focused, so the visual configuration lives in
/// `becomeFirstResponder` rather than init. `mouseDown` is overridden
/// to snap the cursor back to end-of-text after AppKit's default
/// click handling — without it, a click mid-field places the cursor
/// between digits and a subsequent keystroke would insert there
/// (visually fine since the cells re-derive from the binding, but
/// surprising for a UX where "type appends at end" is the contract).
private final class CaretlessTextField: NSTextField {
    override func becomeFirstResponder() -> Bool {
        let didBecome = super.becomeFirstResponder()
        if didBecome, let editor = currentEditor() as? NSTextView {
            editor.insertionPointColor = .clear
            // `textColor = .clear` on the field handles the
            // unselected-text case; selection draws through a
            // SEPARATE attribute dictionary on the field editor,
            // so we have to clear both keys explicitly. Without
            // this, clicking + selecting all (or triple-click)
            // surfaces a purple highlight + the otherwise-hidden
            // digit string drawn in the selection foreground.
            editor.selectedTextAttributes = [
                .foregroundColor: NSColor.clear,
                .backgroundColor: NSColor.clear,
            ]
        }
        return didBecome
    }

    override func mouseDown(with event: NSEvent) {
        super.mouseDown(with: event)
        // AppKit's default placed the cursor at the click point —
        // override and pin to end-of-text. NSTextField's stringValue
        // is what the parent's `value` binding mirrors, so end-of
        // -text is "after the last entered digit", which is the
        // OTP-correct cursor position.
        guard let editor = currentEditor() else { return }
        let end = NSRange(location: stringValue.utf16.count, length: 0)
        editor.selectedRange = end
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
