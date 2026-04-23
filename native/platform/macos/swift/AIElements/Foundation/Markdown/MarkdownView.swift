import SwiftUI

/// Renders a markdown string as a vertically-stacked list of block views.
/// Inline formatting (bold, italic, code spans, links) goes through
/// SwiftUI's built-in `AttributedString(markdown:)`; block-level structure
/// (headings, lists, code blocks, tables) is rendered as distinct SwiftUI
/// views styled for in-chat reading.
///
/// Per AIElements plan FN-5: parsing lives in the shared C++ cmark-gfm
/// wrapper, this view only renders. Passing `streaming: true` applies the
/// incomplete-markdown fixer before parsing so mid-stream fragments don't
/// visually break.
struct MarkdownView: View {
    let text: String
    let streaming: Bool

    init(_ text: String, streaming: Bool = false) {
        self.text = text
        self.streaming = streaming
    }

    var body: some View {
        let source = streaming ? MarkdownParser.fixIncomplete(text) : text
        let blocks = MarkdownParser.parse(source)
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                blockView(block)
            }
        }
    }

    @ViewBuilder
    private func blockView(_ block: MarkdownBlock) -> some View {
        switch block {
        case .paragraph(let text):
            inlineText(text)
                .fixedSize(horizontal: false, vertical: true)

        case .heading(let level, let text):
            inlineText(text)
                .font(headingFont(level: level))
                .fontWeight(.bold)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, level <= 2 ? 4 : 2)

        case .codeBlock(let language, let code):
            CodeBlockView(language: language, code: code)

        case .blockQuote(let text):
            // Overlay pattern keeps the leading bar's height pinned to the
            // text's intrinsic height. A sibling Rectangle in an HStack
            // has no intrinsic height and stretches to fill available
            // space — which is infinity inside a scroll view.
            inlineText(text)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.leading, 11)
                .overlay(alignment: .leading) {
                    Rectangle()
                        .fill(.secondary.opacity(0.4))
                        .frame(width: 3)
                }

        case .list(let ordered, let start, let items):
            VStack(alignment: .leading, spacing: 4) {
                ForEach(Array(items.enumerated()), id: \.offset) { idx, item in
                    HStack(alignment: .top, spacing: 8) {
                        Text(bullet(ordered: ordered, index: idx, start: start, item: item))
                            .font(.body)
                            .foregroundStyle(.secondary)
                            .frame(minWidth: 18, alignment: .trailing)
                        inlineText(item.text)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }

        case .table(let headers, let rows):
            TableBlockView(headers: headers, rows: rows)

        case .thematicBreak:
            Divider()
                .padding(.vertical, 4)

        case .htmlBlock(let html):
            // Raw HTML in chat is rare; render as monospace text for now.
            // Full HTML rendering belongs in WKWebView territory (M4).
            Text(html)
                .font(.system(.callout, design: .monospaced))
                .foregroundStyle(.secondary)

        case .image(let url, let alt, _):
            if let u = URL(string: url) {
                AsyncImage(url: u) { phase in
                    if let image = phase.image {
                        image.resizable().scaledToFit()
                    } else if phase.error != nil {
                        Text(alt.isEmpty ? "Image failed to load" : alt)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    } else {
                        ProgressView().controlSize(.small)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    /// Turns a markdown fragment into SwiftUI-rendered inline formatting.
    /// `AttributedString(markdown:)` handles bold, italic, inline code,
    /// links without extra work. Fall back to plain text on parse error.
    private func inlineText(_ source: String) -> Text {
        if let attr = try? AttributedString(
            markdown: source,
            options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)) {
            return Text(attr)
        }
        return Text(source)
    }

    private func headingFont(level: Int) -> Font {
        switch level {
        case 1: return .title2
        case 2: return .title3
        case 3: return .headline
        default: return .subheadline
        }
    }

    private func bullet(ordered: Bool, index: Int, start: Int,
                        item: MarkdownBlock.ListItem) -> String {
        if item.hasCheckbox { return item.checked ? "☑" : "☐" }
        if ordered { return "\(start + index)." }
        return "•"
    }
}

/// Code block with tree-sitter syntax highlighting (via `CodeHighlighter`).
/// Header: language chip on the left, copy button on the right.
/// Content: monospaced attributed string, gray-filled background,
/// horizontally scrollable for long lines.
private struct CodeBlockView: View {
    let language: String
    let code: String

    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !language.isEmpty {
                header
            }
            ScrollView(.horizontal, showsIndicators: false) {
                Text(CodeHighlighter.shared.highlight(code, language: language))
                    .font(.system(.callout, design: .monospaced))
                    .textSelection(.enabled)
                    .padding(.horizontal, 10)
                    .padding(.vertical, language.isEmpty ? 8 : 6)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.secondary.opacity(0.12))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }

    private var header: some View {
        HStack {
            Text(language)
                .font(.caption2)
                .foregroundStyle(.secondary)
            Spacer()
            IconButton(systemName: copied ? "checkmark" : "doc.on.doc",
                       label: copied ? "Copied" : "Copy code",
                       action: copyCode)
        }
        .padding(.leading, 10)
        .padding(.trailing, 4)
        .padding(.top, 2)
    }

    private func copyCode() {
        MessagePasteboard.copy(code)
        copied = true
        // Revert icon after a beat so the affordance stays useful.
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(1200))
            copied = false
        }
    }
}

/// Minimal table rendering — single column's worth of content per cell,
/// header row bold, thin separator between rows. Richer presentation
/// (sort, resize) belongs in AIElements' Table component, not inline
/// chat markdown.
private struct TableBlockView: View {
    let headers: [String]
    let rows: [[String]]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !headers.isEmpty {
                HStack(alignment: .top, spacing: 10) {
                    ForEach(Array(headers.enumerated()), id: \.offset) { _, h in
                        Text(h)
                            .font(.callout)
                            .fontWeight(.semibold)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                .padding(.vertical, 4)
                Divider()
            }
            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                HStack(alignment: .top, spacing: 10) {
                    ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                        Text(cell)
                            .font(.callout)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                .padding(.vertical, 4)
                if row != rows.last {
                    Divider().opacity(0.5)
                }
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 4)
        .background(Color.secondary.opacity(0.05))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }
}
