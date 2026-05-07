import SwiftUI

/// Fixed-length numeric OTP entry rendered as a row of glyph cells —
/// the familiar one-time-code pattern Apple uses in its own 2FA
/// prompts. Typing auto-advances, backspace steps back, pasting a
/// 6-digit string fills all cells at once.
///
/// ## Architecture
/// One hidden `TextField` owns the entire input and text focus; the
/// visible cells are pure render over `value`. An earlier attempt
/// gave each cell its own `TextField` and synced them, which
/// created a feedback loop — updating one cell's text triggered
/// every cell's `onChange`, which re-triggered the input handler,
/// which moved focus somewhere unexpected. A single source of
/// truth sidesteps all of that.
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
            // backspace. Styled transparent so only the glyph row
            // shows. `.textContentType(.oneTimeCode)` is what
            // makes password managers (1Password, iCloud Keychain)
            // offer to fill the code on focus — without it the
            // field reads as a generic text input and no autofill
            // is offered.
            TextField("", text: $value)
                .textFieldStyle(.plain)
                .multilineTextAlignment(.center)
                .foregroundStyle(.clear)
                .tint(.clear)
                .textContentType(.oneTimeCode)
                .focused($focused)
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
                .onSubmit { onComplete?() }
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
