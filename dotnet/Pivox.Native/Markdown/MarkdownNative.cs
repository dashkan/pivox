using System.Runtime.InteropServices;

namespace Pivox.Native.Markdown;

/// <summary>
/// P/Invoke surface for the pivox_markdown native library
/// (see dotnet/native/markdown/markdown_parser_c.h).
///
/// All entry points speak <see cref="byte"/> pointers + UTF-8 — the
/// native side treats input as opaque bytes and emits NUL-terminated
/// UTF-8 strings. We do the marshaling here so callers stay in
/// managed land.
///
/// Returned pointers are owned by the caller and MUST be freed via
/// <see cref="FreeString"/>. The convenience methods in
/// <c>MarkdownParser</c> do this automatically; raw P/Invoke
/// callers must remember.
/// </summary>
internal static partial class MarkdownNative
{
    /// <summary>Native library name, resolved against the host
    /// platform's lib search path (no extension, no "lib" prefix —
    /// the runtime applies platform conventions:
    /// <c>libpivox_markdown.dylib</c> on macOS,
    /// <c>pivox_markdown.dll</c> on Windows,
    /// <c>libpivox_markdown.so</c> on Linux).</summary>
    internal const string Library = "pivox_markdown";

    /// <summary>Parse markdown into a JSON-encoded block list.
    /// Returns a malloc'd NUL-terminated UTF-8 string the caller
    /// must free with <see cref="FreeString"/>. Returns
    /// <see cref="IntPtr.Zero"/> on allocation failure.</summary>
    [LibraryImport(Library, EntryPoint = "pivox_md_parse_json")]
    internal static partial IntPtr ParseJson(IntPtr utf8Markdown);

    /// <summary>Apply streaming-repair to a partial markdown string
    /// (close unclosed fences, lists, emphasis). Returns a malloc'd
    /// NUL-terminated UTF-8 string the caller must free with
    /// <see cref="FreeString"/>. Safe to pass the result to
    /// <see cref="ParseJson"/>.</summary>
    [LibraryImport(Library, EntryPoint = "pivox_md_fix_incomplete")]
    internal static partial IntPtr FixIncomplete(IntPtr utf8Markdown);

    /// <summary>Free a string returned by <see cref="ParseJson"/> or
    /// <see cref="FixIncomplete"/>. Passing
    /// <see cref="IntPtr.Zero"/> is a no-op on the native side.</summary>
    [LibraryImport(Library, EntryPoint = "pivox_md_free_string")]
    internal static partial void FreeString(IntPtr s);
}
