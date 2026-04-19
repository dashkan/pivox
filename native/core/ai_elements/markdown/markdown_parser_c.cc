#include "markdown_parser_c.h"

#include <cstdlib>
#include <cstring>
#include <sstream>
#include <string>
#include <variant>
#include <vector>

#include "markdown_parser.h"

namespace pivox::markdown {
namespace {

// JSON string-escape, enough for markdown payloads (control chars, quote,
// backslash, backspace, tab, newline, carriage return, form feed). Hand-
// rolled to avoid pulling in a JSON library.
void AppendJsonEscaped(std::ostringstream& out, const std::string& s) {
  out << '"';
  for (unsigned char c : s) {
    switch (c) {
      case '"': out << "\\\""; break;
      case '\\': out << "\\\\"; break;
      case '\b': out << "\\b"; break;
      case '\f': out << "\\f"; break;
      case '\n': out << "\\n"; break;
      case '\r': out << "\\r"; break;
      case '\t': out << "\\t"; break;
      default:
        if (c < 0x20) {
          char buf[8];
          std::snprintf(buf, sizeof(buf), "\\u%04x", c);
          out << buf;
        } else {
          out << static_cast<char>(c);
        }
    }
  }
  out << '"';
}

void AppendField(std::ostringstream& out, const char* key,
                 const std::string& value, bool trailing_comma = true) {
  out << '"' << key << "\":";
  AppendJsonEscaped(out, value);
  if (trailing_comma) out << ',';
}

void WriteBlock(std::ostringstream& out, const Block& block) {
  out << '{';
  std::visit([&out](const auto& b) {
    using T = std::decay_t<decltype(b)>;
    if constexpr (std::is_same_v<T, Paragraph>) {
      out << "\"kind\":\"paragraph\",";
      AppendField(out, "text", b.text, /*trailing_comma=*/false);
    } else if constexpr (std::is_same_v<T, Heading>) {
      out << "\"kind\":\"heading\",\"level\":" << b.level << ',';
      AppendField(out, "text", b.text, /*trailing_comma=*/false);
    } else if constexpr (std::is_same_v<T, CodeBlock>) {
      out << "\"kind\":\"code_block\",";
      AppendField(out, "language", b.language);
      AppendField(out, "code", b.code, /*trailing_comma=*/false);
    } else if constexpr (std::is_same_v<T, BlockQuote>) {
      out << "\"kind\":\"block_quote\",";
      AppendField(out, "text", b.text, /*trailing_comma=*/false);
    } else if constexpr (std::is_same_v<T, List>) {
      out << "\"kind\":\"list\",\"ordered\":"
          << (b.ordered ? "true" : "false")
          << ",\"start\":" << b.start << ",\"items\":[";
      for (size_t i = 0; i < b.items.size(); ++i) {
        if (i > 0) out << ',';
        out << '{';
        AppendField(out, "text", b.items[i].text);
        out << "\"checked\":" << (b.items[i].checked ? "true" : "false")
            << ",\"has_checkbox\":"
            << (b.items[i].has_checkbox ? "true" : "false")
            << '}';
      }
      out << ']';
    } else if constexpr (std::is_same_v<T, Table>) {
      out << "\"kind\":\"table\",\"headers\":[";
      for (size_t i = 0; i < b.headers.size(); ++i) {
        if (i > 0) out << ',';
        AppendJsonEscaped(out, b.headers[i]);
      }
      out << "],\"rows\":[";
      for (size_t i = 0; i < b.rows.size(); ++i) {
        if (i > 0) out << ',';
        out << '[';
        for (size_t j = 0; j < b.rows[i].size(); ++j) {
          if (j > 0) out << ',';
          AppendJsonEscaped(out, b.rows[i][j]);
        }
        out << ']';
      }
      out << ']';
    } else if constexpr (std::is_same_v<T, ThematicBreak>) {
      out << "\"kind\":\"thematic_break\"";
    } else if constexpr (std::is_same_v<T, HtmlBlock>) {
      out << "\"kind\":\"html_block\",";
      AppendField(out, "html", b.html, /*trailing_comma=*/false);
    } else if constexpr (std::is_same_v<T, Image>) {
      out << "\"kind\":\"image\",";
      AppendField(out, "url", b.url);
      AppendField(out, "alt", b.alt);
      AppendField(out, "title", b.title, /*trailing_comma=*/false);
    }
  }, block);
  out << '}';
}

char* DupString(const std::string& s) {
  char* out = static_cast<char*>(std::malloc(s.size() + 1));
  if (out == nullptr) return nullptr;
  std::memcpy(out, s.data(), s.size());
  out[s.size()] = '\0';
  return out;
}

}  // namespace
}  // namespace pivox::markdown

extern "C" char* pivox_md_parse_json(const char* markdown) {
  if (markdown == nullptr) markdown = "";
  auto blocks = pivox::markdown::Parse(markdown);
  std::ostringstream out;
  out << '[';
  for (size_t i = 0; i < blocks.size(); ++i) {
    if (i > 0) out << ',';
    pivox::markdown::WriteBlock(out, blocks[i]);
  }
  out << ']';
  return pivox::markdown::DupString(out.str());
}

extern "C" char* pivox_md_fix_incomplete(const char* markdown) {
  if (markdown == nullptr) markdown = "";
  auto fixed = pivox::markdown::FixIncomplete(markdown);
  return pivox::markdown::DupString(fixed);
}

extern "C" void pivox_md_free_string(char* s) {
  if (s != nullptr) std::free(s);
}
