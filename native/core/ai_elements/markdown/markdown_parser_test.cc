#include <gtest/gtest.h>

#include "markdown_parser.h"

using namespace pivox::markdown;

TEST(MarkdownParser, EmptyInput) {
  auto blocks = Parse("");
  EXPECT_TRUE(blocks.empty());
}

TEST(MarkdownParser, SingleParagraph) {
  auto blocks = Parse("Hello world");
  ASSERT_EQ(blocks.size(), 1u);
  ASSERT_TRUE(std::holds_alternative<Paragraph>(blocks[0]));
  EXPECT_EQ(std::get<Paragraph>(blocks[0]).text, "Hello world");
}

TEST(MarkdownParser, Headings) {
  auto blocks = Parse("# H1\n## H2\n### H3");
  ASSERT_EQ(blocks.size(), 3u);

  auto& h1 = std::get<Heading>(blocks[0]);
  EXPECT_EQ(h1.level, 1);
  EXPECT_EQ(h1.text, "H1");

  auto& h2 = std::get<Heading>(blocks[1]);
  EXPECT_EQ(h2.level, 2);
  EXPECT_EQ(h2.text, "H2");

  auto& h3 = std::get<Heading>(blocks[2]);
  EXPECT_EQ(h3.level, 3);
  EXPECT_EQ(h3.text, "H3");
}

TEST(MarkdownParser, FencedCodeBlock) {
  auto blocks = Parse("```python\nprint('hello')\n```");
  ASSERT_EQ(blocks.size(), 1u);
  ASSERT_TRUE(std::holds_alternative<CodeBlock>(blocks[0]));
  auto& cb = std::get<CodeBlock>(blocks[0]);
  EXPECT_EQ(cb.language, "python");
  EXPECT_EQ(cb.code, "print('hello')\n");
}

TEST(MarkdownParser, BlockQuote) {
  auto blocks = Parse("> This is a quote");
  ASSERT_EQ(blocks.size(), 1u);
  ASSERT_TRUE(std::holds_alternative<BlockQuote>(blocks[0]));
  EXPECT_EQ(std::get<BlockQuote>(blocks[0]).text, "This is a quote");
}

TEST(MarkdownParser, UnorderedList) {
  auto blocks = Parse("- Item 1\n- Item 2\n- Item 3");
  ASSERT_EQ(blocks.size(), 1u);
  ASSERT_TRUE(std::holds_alternative<List>(blocks[0]));
  auto& list = std::get<List>(blocks[0]);
  EXPECT_FALSE(list.ordered);
  ASSERT_EQ(list.items.size(), 3u);
  EXPECT_EQ(list.items[0].text, "Item 1");
  EXPECT_EQ(list.items[1].text, "Item 2");
  EXPECT_EQ(list.items[2].text, "Item 3");
}

TEST(MarkdownParser, OrderedList) {
  auto blocks = Parse("1. First\n2. Second");
  ASSERT_EQ(blocks.size(), 1u);
  ASSERT_TRUE(std::holds_alternative<List>(blocks[0]));
  auto& list = std::get<List>(blocks[0]);
  EXPECT_TRUE(list.ordered);
  ASSERT_EQ(list.items.size(), 2u);
}

TEST(MarkdownParser, ThematicBreak) {
  auto blocks = Parse("---");
  ASSERT_EQ(blocks.size(), 1u);
  EXPECT_TRUE(std::holds_alternative<ThematicBreak>(blocks[0]));
}

TEST(MarkdownParser, GfmTable) {
  auto blocks = Parse("| A | B |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |");
  ASSERT_EQ(blocks.size(), 1u);
  ASSERT_TRUE(std::holds_alternative<Table>(blocks[0]));
  auto& table = std::get<Table>(blocks[0]);
  ASSERT_EQ(table.headers.size(), 2u);
  EXPECT_EQ(table.headers[0], "A");
  EXPECT_EQ(table.headers[1], "B");
  ASSERT_EQ(table.rows.size(), 2u);
  EXPECT_EQ(table.rows[0][0], "1");
  EXPECT_EQ(table.rows[1][1], "4");
}

TEST(MarkdownParser, MultipleMixedBlocks) {
  auto blocks = Parse("# Title\n\nSome text.\n\n```js\nconsole.log('hi')\n```\n\n- a\n- b");
  ASSERT_GE(blocks.size(), 4u);
  EXPECT_TRUE(std::holds_alternative<Heading>(blocks[0]));
  EXPECT_TRUE(std::holds_alternative<Paragraph>(blocks[1]));
  EXPECT_TRUE(std::holds_alternative<CodeBlock>(blocks[2]));
  EXPECT_TRUE(std::holds_alternative<List>(blocks[3]));
}

// Streaming fix tests

TEST(FixIncomplete, ClosesUnclosedFence) {
  auto fixed = FixIncomplete("```python\nprint('hello')");
  // Should close the fence so it parses as a code block.
  auto blocks = Parse(fixed);
  ASSERT_EQ(blocks.size(), 1u);
  EXPECT_TRUE(std::holds_alternative<CodeBlock>(blocks[0]));
}

TEST(FixIncomplete, PassthroughComplete) {
  auto input = "Hello world";
  EXPECT_EQ(FixIncomplete(input), input);
}
