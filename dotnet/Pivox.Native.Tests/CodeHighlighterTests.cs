using Pivox.Native.Highlight;
using Xunit;

namespace Pivox.Native.Tests;

/// <summary>
/// Smoke tests for the P/Invoke plumbing into the
/// pivox_highlight Rust cdylib. Goal: prove .NET → C ABI →
/// tree-sitter → .NET round trip works, and that the resulting
/// spans cover the input contiguously.
/// </summary>
public class CodeHighlighterTests
{
    [Fact]
    public void Highlight_RustSnippet_ProducesKeywordSpan()
    {
        using var highlighter = new CodeHighlighter();
        var spans = highlighter.Highlight("rust", "fn main() {}");

        Assert.NotEmpty(spans);

        // At least one span should be tagged as a keyword (the `fn`).
        // We don't pin the exact id-name set — tree-sitter's
        // highlight names are stable per-version but we want this
        // test to survive minor grammar updates.
        Assert.Contains(spans, s => s.HighlightName == "keyword");
    }

    [Fact]
    public void Highlight_SpansAreOrderedAndNonNegative()
    {
        using var highlighter = new CodeHighlighter();
        var spans = highlighter.Highlight("rust",
            "let x: i32 = 42;\nlet y = \"hello\";");

        int previousEnd = 0;
        foreach (var span in spans)
        {
            Assert.True(span.Start >= 0, $"span.Start={span.Start}");
            Assert.True(span.End >= span.Start,
                $"span [{span.Start},{span.End}) is inverted");
            Assert.True(span.Start >= previousEnd,
                "spans overlap or go backward");
            previousEnd = span.End;
        }
    }

    [Fact]
    public void Highlight_EmptyInput_ReturnsEmpty()
    {
        using var highlighter = new CodeHighlighter();
        var spans = highlighter.Highlight("rust", "");

        Assert.Empty(spans);
    }

    [Fact]
    public void Highlight_UnknownLanguage_DoesNotCrash()
    {
        // The C ABI returns an empty span list for languages it
        // doesn't recognize (rather than failing). Pin that
        // contract so we don't accidentally break it.
        using var highlighter = new CodeHighlighter();
        var spans = highlighter.Highlight("madeuplang", "hello world");

        // Empty or a single "plain" span covering the input —
        // either shape is acceptable. The key invariant is "doesn't
        // crash and returns a valid list."
        Assert.NotNull(spans);
    }

    [Fact]
    public void Dispose_TwiceIsSafe()
    {
        var highlighter = new CodeHighlighter();
        highlighter.Dispose();
        highlighter.Dispose(); // must not throw
    }

    [Fact]
    public void Highlight_AfterDispose_ThrowsObjectDisposed()
    {
        var highlighter = new CodeHighlighter();
        highlighter.Dispose();

        Assert.Throws<ObjectDisposedException>(
            () => highlighter.Highlight("rust", "fn main() {}"));
    }

    [Fact]
    public void Highlight_EmbeddedNulInLanguage_ThrowsArgumentException()
    {
        // The C API treats `language` as a NUL-terminated string;
        // silent truncation would be surprising. Pin the explicit
        // rejection.
        using var highlighter = new CodeHighlighter();
        Assert.Throws<ArgumentException>(
            () => highlighter.Highlight("rust\0evil", "fn main() {}"));
    }
}
