#include "markdown_parser.h"

#include <cmark-gfm-core-extensions.h>
#include <cmark-gfm.h>

#include <cstring>

namespace pivox::markdown {
namespace {

// Extract the text content from a node (concatenating all inline children).
std::string NodeText(cmark_node* node) {
  std::string result;
  for (auto* child = cmark_node_first_child(node); child != nullptr;
       child = cmark_node_next(child)) {
    const char* literal = cmark_node_get_literal(child);
    if (literal != nullptr) {
      result += literal;
    }
    // Recurse into inlines (emphasis, strong, link, etc.).
    auto* grandchild = cmark_node_first_child(child);
    while (grandchild != nullptr) {
      const char* gl = cmark_node_get_literal(grandchild);
      if (gl != nullptr) {
        result += gl;
      }
      grandchild = cmark_node_next(grandchild);
    }
  }
  return result;
}

// Extract text for simple leaf nodes.
std::string LeafText(cmark_node* node) {
  const char* literal = cmark_node_get_literal(node);
  return literal != nullptr ? literal : "";
}

void ParseTableNode(cmark_node* table_node, Table& table) {
  for (auto* row = cmark_node_first_child(table_node); row != nullptr;
       row = cmark_node_next(row)) {
    bool is_header = cmark_gfm_extensions_get_table_row_is_header(row) != 0;
    std::vector<std::string> cells;
    for (auto* cell = cmark_node_first_child(row); cell != nullptr;
         cell = cmark_node_next(cell)) {
      cells.push_back(NodeText(cell));
    }
    if (is_header) {
      table.headers = std::move(cells);
    } else {
      table.rows.push_back(std::move(cells));
    }
  }
}

}  // namespace

std::vector<Block> Parse(const std::string& markdown) {
  if (markdown.empty()) {
    return {};
  }

  // Register GFM extensions.
  cmark_gfm_core_extensions_ensure_registered();

  cmark_parser* parser =
      cmark_parser_new(CMARK_OPT_DEFAULT | CMARK_OPT_UNSAFE);

  // Attach table extension.
  auto* table_ext = cmark_find_syntax_extension("table");
  if (table_ext != nullptr) {
    cmark_parser_attach_syntax_extension(parser, table_ext);
  }
  // Attach tasklist extension.
  auto* tasklist_ext = cmark_find_syntax_extension("tasklist");
  if (tasklist_ext != nullptr) {
    cmark_parser_attach_syntax_extension(parser, tasklist_ext);
  }
  // Attach strikethrough extension.
  auto* strikethrough_ext = cmark_find_syntax_extension("strikethrough");
  if (strikethrough_ext != nullptr) {
    cmark_parser_attach_syntax_extension(parser, strikethrough_ext);
  }

  cmark_parser_feed(parser, markdown.data(), markdown.size());
  cmark_node* doc = cmark_parser_finish(parser);

  std::vector<Block> blocks;

  for (auto* node = cmark_node_first_child(doc); node != nullptr;
       node = cmark_node_next(node)) {
    switch (cmark_node_get_type(node)) {
      case CMARK_NODE_PARAGRAPH:
        blocks.emplace_back(Paragraph{NodeText(node)});
        break;

      case CMARK_NODE_HEADING:
        blocks.emplace_back(
            Heading{cmark_node_get_heading_level(node), NodeText(node)});
        break;

      case CMARK_NODE_CODE_BLOCK: {
        const char* info = cmark_node_get_fence_info(node);
        blocks.emplace_back(
            CodeBlock{info != nullptr ? info : "", LeafText(node)});
        break;
      }

      case CMARK_NODE_BLOCK_QUOTE:
        blocks.emplace_back(BlockQuote{NodeText(node)});
        break;

      case CMARK_NODE_LIST: {
        List list;
        list.ordered =
            cmark_node_get_list_type(node) == CMARK_ORDERED_LIST;
        list.start = cmark_node_get_list_start(node);
        for (auto* item = cmark_node_first_child(node); item != nullptr;
             item = cmark_node_next(item)) {
          ListItem li;
          li.text = NodeText(item);
          li.has_checkbox = false;
          li.checked = false;
          // Check for task list checkbox via the extension.
          if (cmark_gfm_extensions_get_tasklist_item_checked(item)) {
            li.has_checkbox = true;
            li.checked = true;
          }
          list.items.push_back(std::move(li));
        }
        blocks.emplace_back(std::move(list));
        break;
      }

      case CMARK_NODE_THEMATIC_BREAK:
        blocks.emplace_back(ThematicBreak{});
        break;

      case CMARK_NODE_HTML_BLOCK:
        blocks.emplace_back(HtmlBlock{LeafText(node)});
        break;

      default:
        // Check for table extension node.
        if (std::strcmp(cmark_node_get_type_string(node), "table") == 0) {
          Table table;
          ParseTableNode(node, table);
          blocks.emplace_back(std::move(table));
        }
        break;
    }
  }

  cmark_node_free(doc);
  cmark_parser_free(parser);

  return blocks;
}

std::string FixIncomplete(const std::string& markdown) {
  if (markdown.empty()) {
    return markdown;
  }

  std::string result = markdown;

  // Count unclosed code fences.
  int fence_count = 0;
  std::string fence_marker;
  size_t pos = 0;
  while (pos < result.size()) {
    size_t line_end = result.find('\n', pos);
    if (line_end == std::string::npos) {
      line_end = result.size();
    }
    std::string line = result.substr(pos, line_end - pos);

    // Check for fence markers (``` or ~~~).
    size_t trimmed_start = line.find_first_not_of(' ');
    if (trimmed_start != std::string::npos) {
      std::string trimmed = line.substr(trimmed_start);
      if (trimmed.substr(0, 3) == "```" || trimmed.substr(0, 3) == "~~~") {
        std::string marker = trimmed.substr(0, 3);
        if (fence_count == 0) {
          fence_marker = marker;
          fence_count++;
        } else if (trimmed == fence_marker || trimmed.find_first_not_of(marker[0]) == std::string::npos) {
          fence_count--;
        }
      }
    }

    pos = line_end + 1;
  }

  // Close unclosed fences.
  if (fence_count > 0) {
    if (result.back() != '\n') {
      result += '\n';
    }
    result += fence_marker + '\n';
  }

  return result;
}

}  // namespace pivox::markdown
