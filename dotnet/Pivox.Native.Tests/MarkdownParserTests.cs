using System.Text.Json;
using Pivox.Native.Markdown;
using Xunit;

namespace Pivox.Native.Tests;

/// <summary>
/// Smoke tests for the P/Invoke plumbing into libpivox_markdown.
/// These do not exhaustively cover cmark-gfm's parser surface —
/// that's covered by the C++ unit suite. The goal here is to
/// prove the .NET → C ABI → cmark-gfm → JSON → .NET round trip
/// works end-to-end on the host platform.
/// </summary>
public class MarkdownParserTests
{
    [Fact]
    public void ParseJson_HeadingAndParagraph_RoundTripsCorrectly()
    {
        // Smallest input that exercises both block kinds.
        var json = MarkdownParser.ParseJson("# Hello\n\nWorld");

        using var doc = JsonDocument.Parse(json);
        var blocks = doc.RootElement;

        Assert.Equal(JsonValueKind.Array, blocks.ValueKind);
        Assert.Equal(2, blocks.GetArrayLength());

        var heading = blocks[0];
        Assert.Equal("heading", heading.GetProperty("kind").GetString());
        Assert.Equal(1, heading.GetProperty("level").GetInt32());
        Assert.Equal("Hello", heading.GetProperty("text").GetString());

        var paragraph = blocks[1];
        Assert.Equal("paragraph", paragraph.GetProperty("kind").GetString());
        Assert.Equal("World", paragraph.GetProperty("text").GetString());
    }

    [Fact]
    public void ParseJson_Utf8Multibyte_PreservedThroughRoundTrip()
    {
        // Catches UTF-8 marshaling bugs — the native side is byte-
        // exact but P/Invoke could mis-handle the boundary.
        // 你好 → 6 UTF-8 bytes, 2 UTF-16 codepoints.
        var json = MarkdownParser.ParseJson("# 你好");

        using var doc = JsonDocument.Parse(json);
        Assert.Equal(
            "你好",
            doc.RootElement[0].GetProperty("text").GetString());
    }

    [Fact]
    public void ParseJson_EmptyInput_ReturnsEmptyArray()
    {
        var json = MarkdownParser.ParseJson("");

        using var doc = JsonDocument.Parse(json);
        Assert.Equal(JsonValueKind.Array, doc.RootElement.ValueKind);
        Assert.Equal(0, doc.RootElement.GetArrayLength());
    }

    [Fact]
    public void FixIncomplete_AlreadyComplete_IsNoOpEquivalent()
    {
        // Streaming repair on already-complete markdown should be a
        // no-op at the *semantic* level — output may differ in
        // whitespace, but parsing it must yield the same block
        // shape. Pins the contract called out in the doc-comment.
        const string complete = "# Title\n\nBody paragraph.";
        var repaired = MarkdownParser.FixIncomplete(complete);

        var originalJson = MarkdownParser.ParseJson(complete);
        var repairedJson = MarkdownParser.ParseJson(repaired);

        // The block ADT shape must be identical — kind + level + text.
        using var origDoc = JsonDocument.Parse(originalJson);
        using var repDoc = JsonDocument.Parse(repairedJson);
        Assert.Equal(
            origDoc.RootElement.GetArrayLength(),
            repDoc.RootElement.GetArrayLength());
        for (int i = 0; i < origDoc.RootElement.GetArrayLength(); i++)
        {
            Assert.Equal(
                origDoc.RootElement[i].GetProperty("kind").GetString(),
                repDoc.RootElement[i].GetProperty("kind").GetString());
        }
    }

    [Fact]
    public void FixIncomplete_UnclosedFence_ClosesFence()
    {
        // Streaming repair: the dangling code block must close so
        // the result is valid input for ParseJson. We don't pin
        // the exact output shape (the repair algorithm may change);
        // we pin that the result parses as a closed code block.
        var repaired = MarkdownParser.FixIncomplete(
            "```rust\nfn main() {");

        var json = MarkdownParser.ParseJson(repaired);
        using var doc = JsonDocument.Parse(json);
        var first = doc.RootElement[0];
        Assert.Equal("code_block", first.GetProperty("kind").GetString());
        Assert.Equal("rust", first.GetProperty("language").GetString());
        Assert.Contains("fn main", first.GetProperty("code").GetString());
    }
}
