import AppKit
import SwiftUI

/// Thin Swift wrapper around the Rust tree-sitter highlighter
/// (`pivox_highlight.h`). Produces an `AttributedString` with per-span
/// foreground colors mapped from the tree-sitter capture names.
///
/// Lifetime: the underlying `PivoxHighlighter*` is expensive to build
/// (loads every grammar) so we hold a single process-global instance.
/// Main-actor isolated because `HighlightTheme` uses SwiftUI `Color`.
@MainActor
final class CodeHighlighter {
    static let shared = CodeHighlighter()

    /// Opaque handle to the Rust highlighter. Swift imports the C
    /// `PivoxHighlighter*` as `OpaquePointer?`. Leaked on process exit —
    /// no deinit because singleton.
    private let handle: OpaquePointer?

    private init() {
        handle = pivox_highlighter_create()
    }

    /// Produce a highlighted AttributedString. If the language isn't
    /// known, falls back to a single plain run.
    func highlight(_ code: String, language: String) -> AttributedString {
        guard !code.isEmpty, let handle else {
            return AttributedString(code)
        }

        let resolved = Self.resolveLanguage(language)
        let bytes = Array(code.utf8)
        let result: PivoxHighlightResult = bytes.withUnsafeBufferPointer { buf in
            pivox_highlight(handle, resolved, buf.baseAddress, UInt32(buf.count))
        }
        defer { pivox_highlight_result_free(result) }

        let spans: [PivoxHighlightSpan] = {
            guard result.count > 0, let ptr = result.spans else { return [] }
            return Array(UnsafeBufferPointer(start: ptr, count: Int(result.count)))
        }()

        if spans.isEmpty {
            return AttributedString(code)
        }

        return buildAttributed(bytes: bytes, spans: spans)
    }

    /// Walk the sorted span list, emitting plain-text runs for gaps and
    /// themed runs for spans. Slicing the raw UTF-8 buffer and building
    /// strings from each slice keeps byte offsets correct even for
    /// multi-byte characters.
    private func buildAttributed(
        bytes: [UInt8], spans: [PivoxHighlightSpan]
    ) -> AttributedString {
        var attr = AttributedString()
        var cursor: UInt32 = 0

        for span in spans where span.end > span.start {
            if span.start > cursor {
                attr.append(decoded(bytes: bytes, start: cursor, end: span.start))
            }
            var run = decoded(bytes: bytes, start: span.start, end: span.end)
            if let color = colorFor(highlightID: span.highlight_id) {
                run.foregroundColor = color
            }
            attr.append(run)
            cursor = span.end
        }

        if Int(cursor) < bytes.count {
            attr.append(decoded(bytes: bytes, start: cursor, end: UInt32(bytes.count)))
        }
        return attr
    }

    private func decoded(bytes: [UInt8], start: UInt32, end: UInt32) -> AttributedString {
        let slice = bytes[Int(start)..<Int(end)]
        let str = String(decoding: slice, as: UTF8.self)
        return AttributedString(str)
    }

    /// Normalize fence-info aliases to the canonical language names
    /// registered in the Rust highlighter. Models emit varied aliases
    /// (yml/yaml, js/javascript, ts/typescript, py/python, sh/bash,
    /// c++/cpp, xml via html fallback). Case-folding included since
    /// fence info isn't case-sensitive in practice.
    private static func resolveLanguage(_ raw: String) -> String {
        switch raw.lowercased() {
        case "yml":                         return "yaml"
        case "js", "jsx":                   return "javascript"
        case "ts", "tsx":                   return "typescript"
        case "py":                          return "python"
        case "sh", "shell", "zsh":          return "bash"
        case "c++", "cc", "cxx", "hpp", "h++": return "cpp"
        case "objective-c", "objc", "m":    return "c"
        case "golang":                      return "go"
        case "rs":                          return "rust"
        // No native XML grammar; HTML's grammar handles angle-bracket
        // markup well enough to be useful. Imperfect but beats plain.
        case "xml", "svg":                  return "html"
        default:                            return raw.lowercased()
        }
    }

    private func colorFor(highlightID: Int32) -> Color? {
        guard highlightID >= 0,
              let cName = pivox_highlight_name(highlightID) else { return nil }
        let name = String(cString: cName)
        return HighlightTheme.shared.color(for: name)
    }
}

/// Appearance-aware color palette keyed by tree-sitter capture name.
/// Colors auto-adapt via NSColor dynamic providers so code blocks look
/// right under both light and dark system appearances without the
/// caller plumbing colorScheme through.
private struct HighlightTheme {
    static let shared = HighlightTheme()

    func color(for name: String) -> Color? {
        switch name {
        case "keyword":                       return Self.keyword
        case "string", "string.special":      return Self.string
        case "comment":                       return .secondary
        case "number":                        return Self.number
        case "type", "type.builtin":          return Self.type
        case "function", "function.builtin",
             "function.macro":                return Self.function
        case "constructor":                   return Self.type
        case "constant", "constant.builtin":  return Self.constant
        case "property":                      return Self.property
        case "attribute":                     return Self.attribute
        case "tag":                           return Self.tag
        default:                              return nil
        }
    }

    // Palette. Each entry declares (dark, light) RGB triplets. Light-
    // mode values are more saturated and darker so they stay legible
    // on white backgrounds — the dark-mode values were chosen for
    // contrast against a dark panel and wash out in light mode.
    private static let keyword   = dyn(dark: (0.78, 0.55, 0.96), light: (0.56, 0.18, 0.75))
    private static let string    = dyn(dark: (1.00, 0.70, 0.50), light: (0.65, 0.32, 0.08))
    private static let number    = dyn(dark: (0.67, 0.83, 0.54), light: (0.23, 0.52, 0.16))
    private static let type      = dyn(dark: (0.52, 0.80, 1.00), light: (0.10, 0.36, 0.72))
    private static let function  = dyn(dark: (0.50, 0.90, 0.85), light: (0.05, 0.48, 0.58))
    private static let constant  = dyn(dark: (0.96, 0.64, 0.73), light: (0.72, 0.20, 0.36))
    private static let property  = dyn(dark: (0.75, 0.88, 1.00), light: (0.18, 0.44, 0.76))
    private static let attribute = dyn(dark: (0.85, 0.70, 1.00), light: (0.54, 0.28, 0.76))
    private static let tag       = dyn(dark: (1.00, 0.70, 0.50), light: (0.65, 0.32, 0.08))

    /// Build a dynamic Color whose resolved RGB depends on the
    /// effective NSAppearance. bestMatch returns nil for the darkAqua
    /// set if the current appearance isn't dark; we treat that as light.
    private static func dyn(dark: (CGFloat, CGFloat, CGFloat),
                            light: (CGFloat, CGFloat, CGFloat)) -> Color {
        let ns = NSColor(name: nil) { appearance in
            let isDark = appearance.bestMatch(from: [
                .darkAqua, .vibrantDark,
                .accessibilityHighContrastDarkAqua,
                .accessibilityHighContrastVibrantDark,
            ]) != nil
            let (r, g, b) = isDark ? dark : light
            return NSColor(red: r, green: g, blue: b, alpha: 1)
        }
        return Color(nsColor: ns)
    }
}
