using System.Runtime.InteropServices;

namespace Pivox.Native.Highlight;

/// <summary>
/// P/Invoke surface for the pivox_highlight native library
/// (see dotnet/native/highlight/include/pivox_highlight.h).
///
/// The Rust cdylib owns a <c>PivoxHighlighter</c> instance that
/// caches per-language tree-sitter configurations. Callers create
/// one with <see cref="HighlighterCreate"/>, use it across many
/// highlight calls, and destroy it with
/// <see cref="HighlighterDestroy"/>. <c>CodeHighlighter</c>
/// wraps this lifetime in a managed <see cref="IDisposable"/>.
/// </summary>
internal static partial class HighlightNative
{
    /// <summary>Native library name (see <c>MarkdownNative.Library</c>
    /// for the platform-extension story).</summary>
    internal const string Library = "pivox_highlight";

    /// <summary>One span of highlighted source. <c>HighlightId</c>
    /// indexes into <see cref="HighlightName"/>; -1 means plain
    /// (unhighlighted) source.</summary>
    [StructLayout(LayoutKind.Sequential)]
    internal struct HighlightSpan
    {
        public uint Start;
        public uint End;
        public int HighlightId;
    }

    /// <summary>Native result of a highlight call. <c>Spans</c> is
    /// a malloc'd array of length <c>Count</c>. Must be released via
    /// <see cref="HighlightResultFree"/>.</summary>
    [StructLayout(LayoutKind.Sequential)]
    internal struct HighlightResult
    {
        public IntPtr Spans; // PivoxHighlightSpan*
        public uint Count;
    }

    [LibraryImport(Library, EntryPoint = "pivox_highlighter_create")]
    internal static partial IntPtr HighlighterCreate();

    [LibraryImport(Library, EntryPoint = "pivox_highlighter_destroy")]
    internal static partial void HighlighterDestroy(IntPtr highlighter);

    /// <summary>Run a highlight pass. <paramref name="language"/> is
    /// a NUL-terminated UTF-8 string naming the language
    /// (e.g. "rust", "python"). <paramref name="source"/> points to
    /// <paramref name="sourceLen"/> bytes of source code.</summary>
    [LibraryImport(Library, EntryPoint = "pivox_highlight")]
    internal static partial HighlightResult Highlight(
        IntPtr highlighter, IntPtr language, IntPtr source, uint sourceLen);

    /// <summary>Free a result returned by <see cref="Highlight"/>.</summary>
    [LibraryImport(Library, EntryPoint = "pivox_highlight_result_free")]
    internal static partial void HighlightResultFree(HighlightResult result);

    /// <summary>Look up the name of a highlight ID
    /// (e.g. "keyword", "string"). Returns
    /// <see cref="IntPtr.Zero"/> for out-of-range IDs. The returned
    /// pointer is borrowed — DO NOT free it.</summary>
    [LibraryImport(Library, EntryPoint = "pivox_highlight_name")]
    internal static partial IntPtr HighlightName(int id);

    /// <summary>Total number of recognized highlight names.</summary>
    [LibraryImport(Library, EntryPoint = "pivox_highlight_name_count")]
    internal static partial uint HighlightNameCount();
}
