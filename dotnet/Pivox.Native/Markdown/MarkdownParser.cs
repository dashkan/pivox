using System.Runtime.InteropServices;
using System.Text;

namespace Pivox.Native.Markdown;

/// <summary>
/// Managed wrapper around <see cref="MarkdownNative"/>. Handles UTF-8
/// marshaling and native-string lifetime — callers see plain
/// <see cref="string"/> in / out.
///
/// Returns JSON shapes per markdown_parser_c.h:
/// <code>
///   {"kind":"paragraph","text":"..."}
///   {"kind":"heading","level":1-6,"text":"..."}
///   {"kind":"code_block","language":"...","code":"..."}
///   {"kind":"block_quote","text":"..."}
///   {"kind":"list","ordered":true|false,"start":N,"items":[...]}
///   {"kind":"table","headers":[...],"rows":[...]}
///   {"kind":"thematic_break"}
///   {"kind":"html_block","html":"..."}
///   {"kind":"image","url":"...","alt":"...","title":"..."}
/// </code>
///
/// The strongly-typed block ADT lives at a higher layer (Pivox.Shared
/// or per-platform UI); this class deliberately stays at the JSON
/// boundary so consumers can pick their own deserialization
/// strategy (System.Text.Json source generator, hand-rolled,
/// etc.).
/// </summary>
public static class MarkdownParser
{
    /// <summary>Parse markdown into a JSON-encoded block list.
    /// Throws <see cref="InvalidOperationException"/> on native
    /// allocation failure (rare — only seen under genuine OOM).</summary>
    public static string ParseJson(string markdown)
    {
        ArgumentNullException.ThrowIfNull(markdown);
        return CallReturningString(markdown, MarkdownNative.ParseJson);
    }

    /// <summary>Apply streaming-repair to a partial markdown string.
    /// Safe to call on already-complete markdown — repair is a
    /// no-op when nothing needs closing. Output is always valid
    /// input for <see cref="ParseJson"/>.</summary>
    public static string FixIncomplete(string markdown)
    {
        ArgumentNullException.ThrowIfNull(markdown);
        return CallReturningString(markdown, MarkdownNative.FixIncomplete);
    }

    /// <summary>Marshal a managed string → UTF-8 → native pointer →
    /// invoke → read back UTF-8 → free. Centralized so both
    /// entry points share the lifetime ceremony.</summary>
    private static string CallReturningString(
        string input, Func<IntPtr, IntPtr> nativeCall)
    {
        // Marshal.StringToCoTaskMemUTF8 allocates an unmanaged UTF-8
        // buffer the native side reads as `const char*`. Must be
        // freed regardless of the call's outcome.
        IntPtr inputPtr = Marshal.StringToCoTaskMemUTF8(input);
        try
        {
            IntPtr resultPtr = nativeCall(inputPtr);
            if (resultPtr == IntPtr.Zero)
            {
                throw new InvalidOperationException(
                    "pivox_markdown native call returned NULL " +
                    "(native allocation failure).");
            }
            try
            {
                // Marshal.PtrToStringUTF8 copies the bytes into a
                // managed string. After this returns, the native
                // pointer is no longer referenced and can be freed.
                return Marshal.PtrToStringUTF8(resultPtr)
                    ?? throw new InvalidOperationException(
                        "pivox_markdown returned non-NULL pointer " +
                        "but failed to materialize as UTF-8 string.");
            }
            finally
            {
                MarkdownNative.FreeString(resultPtr);
            }
        }
        finally
        {
            Marshal.FreeCoTaskMem(inputPtr);
        }
    }
}
