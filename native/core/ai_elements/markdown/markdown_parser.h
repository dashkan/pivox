#ifndef PIVOX_MARKDOWN_PARSER_H
#define PIVOX_MARKDOWN_PARSER_H

#include <cstdint>
#include <string>
#include <variant>
#include <vector>

namespace pivox::markdown {

// Block types produced by the parser.
struct Paragraph {
  std::string text;  // Inline markdown (bold, italic, code, links).
};

struct Heading {
  int level;  // 1–6
  std::string text;
};

struct CodeBlock {
  std::string language;
  std::string code;
};

struct BlockQuote {
  std::string text;
};

struct ListItem {
  std::string text;
  bool checked;       // For task lists.
  bool has_checkbox;   // True if it's a task list item.
};

struct List {
  bool ordered;
  int start;  // Starting number for ordered lists.
  std::vector<ListItem> items;
};

struct Table {
  std::vector<std::string> headers;
  std::vector<std::vector<std::string>> rows;
};

struct ThematicBreak {};

struct HtmlBlock {
  std::string html;
};

struct Image {
  std::string url;
  std::string alt;
  std::string title;
};

using Block = std::variant<Paragraph, Heading, CodeBlock, BlockQuote, List,
                           Table, ThematicBreak, HtmlBlock, Image>;

// Parse a markdown string into a list of typed blocks.
std::vector<Block> Parse(const std::string& markdown);

// Fix incomplete markdown for streaming (close unclosed fences, etc.).
std::string FixIncomplete(const std::string& markdown);

}  // namespace pivox::markdown

#endif  // PIVOX_MARKDOWN_PARSER_H
