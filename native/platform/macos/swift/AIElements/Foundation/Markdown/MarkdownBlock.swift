import Foundation

/// Typed markdown block produced by the shared C++ cmark-gfm parser.
/// Mirrors `pivox::markdown::Block` (see `native/core/ai_elements/markdown/markdown_parser.h`).
/// The JSON shape is emitted by `pivox_md_parse_json` in the same module.
enum MarkdownBlock: Equatable {
    case paragraph(text: String)
    case heading(level: Int, text: String)
    case codeBlock(language: String, code: String)
    case blockQuote(text: String)
    case list(ordered: Bool, start: Int, items: [ListItem])
    case table(headers: [String], rows: [[String]])
    case thematicBreak
    case htmlBlock(html: String)
    case image(url: String, alt: String, title: String)

    struct ListItem: Equatable {
        let text: String
        let checked: Bool
        let hasCheckbox: Bool
    }
}

/// Parser calling into the shared C++ cmark-gfm wrapper (via `pivox_md_parse_json`).
/// Thread-safe — the underlying C++ parser is stateless per call.
///
/// Results are cached keyed by input string. Committed chat messages
/// are immutable, so after the first parse of a given message's text,
/// every subsequent `parse(...)` for that text is a dictionary
/// lookup. This is what makes `LazyVStack` viable for a transcript of
/// `Message` views: the per-cell rebuild cost drops from "re-run
/// cmark-gfm" to "read an array reference."
enum MarkdownParser {
    private static let cacheLock = NSLock()
    nonisolated(unsafe) private static var cache: [String: [MarkdownBlock]] = [:]

    /// Serial queue for background parsing. The underlying cmark-gfm
    /// core extensions use `cmark_gfm_core_extensions_ensure_registered`
    /// which performs one-time global registration that is NOT
    /// thread-safe — parallel calls racing the registration produced
    /// crashes on startup when we prewarmed 50 messages in parallel.
    /// Serialising solves it without sacrificing off-main-thread work.
    private static let parseQueue = DispatchQueue(
        label: "pivox.markdown.parse", qos: .userInitiated)

    /// Parse a markdown string into a typed block list. Returns an empty
    /// list if parsing fails (e.g. the C bridge returns null). Does NOT
    /// throw — markdown parsing failures are visual, not semantic, and the
    /// caller handles an empty list as "nothing to render."
    static func parse(_ markdown: String) -> [MarkdownBlock] {
        cacheLock.lock()
        if let cached = cache[markdown] {
            cacheLock.unlock()
            return cached
        }
        cacheLock.unlock()

        let blocks = parseUncached(markdown)

        cacheLock.lock()
        cache[markdown] = blocks
        cacheLock.unlock()
        return blocks
    }

    /// Warm the cache for a markdown string off the main thread. Call
    /// when a message is first received (before the user can scroll
    /// to it) so the eventual SwiftUI body evaluation hits a cached
    /// parse result instead of blocking the main thread on cmark-gfm.
    /// Parses run on a single serial queue (not the global concurrent
    /// pool) because cmark-gfm's extension registration is not
    /// thread-safe.
    static func parseAsync(_ markdown: String) {
        parseQueue.async {
            _ = parse(markdown)
        }
    }

    private static func parseUncached(_ markdown: String) -> [MarkdownBlock] {
        guard let cStr = pivox_md_parse_json(markdown) else { return [] }
        defer { pivox_md_free_string(cStr) }
        let data = Data(bytes: cStr, count: strlen(cStr))
        guard let obj = try? JSONSerialization.jsonObject(with: data),
              let raw = obj as? [[String: Any]]
        else { return [] }
        return raw.compactMap(decodeBlock)
    }

    /// Apply streaming-repair to an incomplete markdown fragment (closes
    /// unclosed fences, lists, emphasis). Use during streaming to render
    /// partial content cleanly before the model finishes.
    static func fixIncomplete(_ markdown: String) -> String {
        guard let cStr = pivox_md_fix_incomplete(markdown) else { return markdown }
        defer { pivox_md_free_string(cStr) }
        return String(cString: cStr)
    }

    // MARK: - Decoding

    private static func decodeBlock(_ obj: [String: Any]) -> MarkdownBlock? {
        guard let kind = obj["kind"] as? String else { return nil }
        switch kind {
        case "paragraph":
            return .paragraph(text: obj["text"] as? String ?? "")
        case "heading":
            return .heading(
                level: obj["level"] as? Int ?? 1,
                text: obj["text"] as? String ?? "")
        case "code_block":
            return .codeBlock(
                language: obj["language"] as? String ?? "",
                code: obj["code"] as? String ?? "")
        case "block_quote":
            return .blockQuote(text: obj["text"] as? String ?? "")
        case "list":
            let rawItems = obj["items"] as? [[String: Any]] ?? []
            let items = rawItems.map { item in
                MarkdownBlock.ListItem(
                    text: item["text"] as? String ?? "",
                    checked: item["checked"] as? Bool ?? false,
                    hasCheckbox: item["has_checkbox"] as? Bool ?? false)
            }
            return .list(
                ordered: obj["ordered"] as? Bool ?? false,
                start: obj["start"] as? Int ?? 1,
                items: items)
        case "table":
            let headers = obj["headers"] as? [String] ?? []
            let rows = obj["rows"] as? [[String]] ?? []
            return .table(headers: headers, rows: rows)
        case "thematic_break":
            return .thematicBreak
        case "html_block":
            return .htmlBlock(html: obj["html"] as? String ?? "")
        case "image":
            return .image(
                url: obj["url"] as? String ?? "",
                alt: obj["alt"] as? String ?? "",
                title: obj["title"] as? String ?? "")
        default:
            return nil
        }
    }
}
