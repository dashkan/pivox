import SwiftUI

/// Fixed-length numeric OTP entry, rendered as a row of individual
/// glyph cells — the familiar one-time-code pattern Apple uses in
/// its own 2FA prompts. Each character gets its own box with the
/// focused cell highlighted; typing auto-advances, backspace steps
/// back, and pasting a 6-digit string fills all cells at once.
///
/// The value is exposed as a single `String` binding (digits only,
/// up to `length`). Callers can observe when it reaches the full
/// length to trigger auto-submit or enable a verify button.
///
/// ## Why not a plain `SecureField` / `TextField`
/// A single text field for a 6-digit code looks low-rent on a
/// security-critical surface and gives no visual affordance for
/// progress. The segmented look signals "fixed-length code" at a
/// glance and matches the platform's own UI for the same task.
struct OTPSegmentedField: View {
    @Binding var value: String
    var length: Int = 6

    @FocusState private var focusedIndex: Int?
    @Environment(\.pivoxTheme) private var theme

    var body: some View {
        HStack(spacing: 8) {
            ForEach(0..<length, id: \.self) { index in
                cell(at: index)
            }
        }
        .onAppear { focusedIndex = 0 }
    }

    @ViewBuilder
    private func cell(at index: Int) -> some View {
        let char = character(at: index)
        let isFocused = focusedIndex == index
        ZStack {
            RoundedRectangle(cornerRadius: 8)
                .strokeBorder(
                    isFocused ? Color.accentColor : Color.secondary.opacity(0.35),
                    lineWidth: isFocused ? 2 : 1)
                .background(
                    RoundedRectangle(cornerRadius: 8)
                        .fill(Color(nsColor: .controlBackgroundColor)))
            // Hidden text field that actually owns the keyboard
            // focus and drives changes. The visible digit sits on
            // top as a display-only `Text`.
            OTPCellField(
                index: index,
                value: $value,
                length: length,
                focusedIndex: $focusedIndex)
                .focused($focusedIndex, equals: index)
            Text(String(char ?? " "))
                .font(.system(size: 24, weight: .medium, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(.primary)
                .allowsHitTesting(false)
        }
        .frame(width: 44, height: 52)
        .onTapGesture { focusedIndex = index }
    }

    private func character(at index: Int) -> Character? {
        guard index < value.count else { return nil }
        return value[value.index(value.startIndex, offsetBy: index)]
    }
}

/// Invisible `TextField` that hosts the real focus + input binding
/// for one OTP cell. Exists as its own view so the `.onChange`
/// observer doesn't need to disambiguate which cell a change
/// belongs to — the index is captured in the closure.
private struct OTPCellField: View {
    let index: Int
    @Binding var value: String
    let length: Int
    var focusedIndex: FocusState<Int?>.Binding

    /// Local buffer that TextField writes into. We sync it with the
    /// parent `value` binding so paste and external updates
    /// (`onChange(of: value)`) are picked up.
    @State private var text: String = ""

    var body: some View {
        TextField("", text: $text)
            .textFieldStyle(.plain)
            .multilineTextAlignment(.center)
            .foregroundStyle(.clear)
            .tint(.clear)
            .frame(width: 44, height: 52)
            .onChange(of: text) { _, newValue in
                handleInput(newValue)
            }
            .onChange(of: value) { _, newValue in
                // Parent updated the shared value (e.g. paste in a
                // sibling cell, or reset). Sync this cell's local
                // buffer to the single glyph at our index.
                let expected = glyph(from: newValue)
                if text != expected { text = expected }
            }
            .onAppear {
                text = glyph(from: value)
            }
    }

    private func glyph(from string: String) -> String {
        guard index < string.count else { return "" }
        return String(string[string.index(string.startIndex, offsetBy: index)])
    }

    private func handleInput(_ raw: String) {
        // Strip non-digits. If the user pasted a multi-digit string
        // into this cell, spread it across subsequent cells starting
        // here. Typing a single digit advances focus.
        let digits = raw.filter(\.isNumber)
        guard !digits.isEmpty || raw.isEmpty else {
            // Pasted something with no digits — clear the buffer.
            text = glyph(from: value)
            return
        }

        if digits.count > 1 {
            // Paste: distribute starting at this cell.
            var chars = Array(value)
            while chars.count < length { chars.append(" ") }
            for (offset, digit) in digits.prefix(length - index).enumerated() {
                chars[index + offset] = digit
            }
            let joined = String(chars.prefix(length)).replacingOccurrences(
                of: " ", with: "")
            value = String(joined.prefix(length))
            text = glyph(from: value)
            focusedIndex.wrappedValue = min(index + digits.count, length - 1)
            return
        }

        if digits.isEmpty {
            // Deletion. Remove glyph at our index and step back.
            var chars = Array(value)
            if index < chars.count {
                chars.remove(at: index)
            }
            value = String(chars.prefix(length))
            text = ""
            if index > 0 {
                focusedIndex.wrappedValue = index - 1
            }
            return
        }

        // Single-digit input. Replace glyph at this index and
        // advance focus to the next cell.
        var chars = Array(value)
        while chars.count < length { chars.append(" ") }
        chars[index] = digits.first!
        let compact = String(chars.prefix(length)).replacingOccurrences(
            of: " ", with: "")
        value = String(compact.prefix(length))
        text = String(digits.first!)
        if index + 1 < length {
            focusedIndex.wrappedValue = index + 1
        }
    }
}
