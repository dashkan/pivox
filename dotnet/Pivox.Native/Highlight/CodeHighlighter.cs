using System.Runtime.InteropServices;
using System.Text;

namespace Pivox.Native.Highlight;

/// <summary>One run of highlighted source — half-open byte range
/// <c>[Start, End)</c> within the input, tagged with a
/// <c>HighlightName</c> like <c>"keyword"</c> or <c>"string"</c>.
/// <c>HighlightName</c> is <see cref="string.Empty"/> for plain
/// (unhighlighted) source — the native side surfaces those spans so
/// the consumer can render the unhighlighted gaps without doing range
/// arithmetic itself.</summary>
public readonly record struct HighlightSpan(
    int Start, int End, string HighlightName);

/// <summary>
/// Managed wrapper around the pivox_highlight tree-sitter cdylib.
/// Owns a single <c>PivoxHighlighter</c> handle for the wrapper's
/// lifetime — instantiation cost is non-trivial (tree-sitter
/// language configs warmed once), so re-use one instance across
/// many <see cref="Highlight"/> calls.
///
/// Thread-safety: the underlying tree-sitter handle is NOT
/// thread-safe for concurrent <c>Highlight</c> calls — callers
/// either serialize access or use one wrapper per worker thread.
/// What IS safe is concurrent disposal: <see cref="Dispose"/> uses
/// <see cref="Interlocked.Exchange(ref IntPtr, IntPtr)"/> on the
/// handle so the native destroy runs exactly once even if Dispose
/// races with the finalizer (or with itself across threads). The
/// "concurrent call vs Dispose" interleaving is still undefined
/// behavior — pinning that down would require a reader/writer lock
/// the caller does not want to pay for.
/// </summary>
public sealed class CodeHighlighter : IDisposable
{
    // _handle == IntPtr.Zero IS the disposed state. No separate bool —
    // a single source of truth keeps Dispose/finalizer reasoning
    // straightforward and matches what Interlocked.Exchange returns.
    private IntPtr _handle;

    /// <summary>Cache of name-id → managed string, populated lazily
    /// from <c>pivox_highlight_name</c>. Names are stable for the
    /// lifetime of the cdylib so caching is safe. Sized to the count
    /// of highlight names declared in the Rust side (~24) to avoid
    /// a dictionary growth on first use.</summary>
    private readonly Dictionary<int, string> _nameCache = new(32);

    public CodeHighlighter()
    {
        _handle = HighlightNative.HighlighterCreate();
        if (_handle == IntPtr.Zero)
        {
            throw new InvalidOperationException(
                "pivox_highlighter_create returned NULL.");
        }
    }

    /// <summary>Highlight <paramref name="source"/> as
    /// <paramref name="language"/> (e.g. "rust", "python", "swift").
    /// Returns spans in source order; consecutive spans cover the
    /// input contiguously. Returns an empty array for empty
    /// input.</summary>
    public IReadOnlyList<HighlightSpan> Highlight(
        string language, string source)
    {
        ArgumentNullException.ThrowIfNull(language);
        ArgumentNullException.ThrowIfNull(source);
        // The C API treats `language` as a NUL-terminated C string, so
        // an embedded NUL would silently truncate. Reject explicitly
        // rather than producing surprising tree-sitter mismatches
        // ("python\0evil" would match "python").
        if (language.AsSpan().IndexOf('\0') >= 0)
        {
            throw new ArgumentException(
                "language must not contain embedded NUL characters.",
                nameof(language));
        }

        // Capture the handle once — any disposer racing this method
        // after the capture targets a handle that may already have
        // been destroyed. That's the documented "Highlight vs Dispose
        // is undefined" boundary; checking-then-using inside the
        // same method does not buy real safety, so we just guard the
        // single read with ObjectDisposedException.
        var handle = _handle;
        ObjectDisposedException.ThrowIf(handle == IntPtr.Zero, this);

        if (source.Length == 0) return Array.Empty<HighlightSpan>();

        // Marshal language as NUL-terminated UTF-8.
        IntPtr languagePtr = Marshal.StringToCoTaskMemUTF8(language);
        // Source bytes carry their own length — we pin a UTF-8 byte
        // buffer rather than using StringToCoTaskMemUTF8 so we know
        // the exact byte count for the C API's uint32 source_len
        // parameter (StringToCoTaskMemUTF8 includes the NUL
        // terminator in the allocation but doesn't surface the
        // length).
        byte[] sourceBytes = Encoding.UTF8.GetBytes(source);
        // The C ABI's source_len is uint32. A silent wrap on a >4 GiB
        // source would produce undefined tree-sitter behavior reading
        // past the buffer end. Fail fast — and realistically this
        // never trips because .NET arrays cap at ~2 GiB by default,
        // but the explicit guard documents the contract.
        if ((uint)sourceBytes.Length != (long)sourceBytes.Length)
        {
            throw new ArgumentException(
                $"source UTF-8 byte length ({sourceBytes.Length:N0}) " +
                "exceeds the C ABI's uint32 source_len.",
                nameof(source));
        }

        try
        {
            unsafe
            {
                fixed (byte* sourcePtr = sourceBytes)
                {
                    var result = HighlightNative.Highlight(
                        handle,
                        languagePtr,
                        (IntPtr)sourcePtr,
                        (uint)sourceBytes.Length);
                    try
                    {
                        return MaterializeSpans(result);
                    }
                    finally
                    {
                        HighlightNative.HighlightResultFree(result);
                    }
                }
            }
        }
        finally
        {
            Marshal.FreeCoTaskMem(languagePtr);
        }
    }

    /// <summary>Convert byte-range spans into managed records.
    /// Native ranges are byte offsets into the UTF-8 source; we
    /// expose them as-is rather than translating to UTF-16 indices
    /// because callers typically draw glyph runs against the same
    /// byte stream they passed in.</summary>
    private List<HighlightSpan> MaterializeSpans(
        HighlightNative.HighlightResult result)
    {
        // result.Count is uint32. The List<>.ctor capacity is int —
        // a silent narrowing cast on >2B spans would wrap to a
        // negative number and throw OOM-shaped exceptions deep in
        // the loop. Tree-sitter doesn't emit 2B spans, but the
        // explicit guard makes the contract visible.
        if (result.Count > int.MaxValue)
        {
            throw new OverflowException(
                $"pivox_highlight returned {result.Count:N0} spans, " +
                "exceeding the managed wrapper's int32 limit.");
        }

        var spans = new List<HighlightSpan>((int)result.Count);
        if (result.Count == 0 || result.Spans == IntPtr.Zero)
        {
            return spans;
        }

        unsafe
        {
            var native =
                (HighlightNative.HighlightSpan*)result.Spans.ToPointer();
            for (uint i = 0; i < result.Count; i++)
            {
                var s = native[i];
                spans.Add(new HighlightSpan(
                    (int)s.Start,
                    (int)s.End,
                    ResolveName(s.HighlightId)));
            }
        }
        return spans;
    }

    /// <summary>Resolve a highlight ID to its name, caching on
    /// first access. -1 → empty string ("plain source"); out-of-range
    /// → empty string with a defensive fallback.</summary>
    private string ResolveName(int id)
    {
        if (id < 0) return string.Empty;
        if (_nameCache.TryGetValue(id, out var cached)) return cached;

        IntPtr namePtr = HighlightNative.HighlightName(id);
        string resolved = namePtr == IntPtr.Zero
            ? string.Empty
            : Marshal.PtrToStringUTF8(namePtr) ?? string.Empty;
        _nameCache[id] = resolved;
        return resolved;
    }

    public void Dispose()
    {
        // Atomic claim of the handle: whichever caller (explicit
        // Dispose, finalizer, or a second Dispose racing the first)
        // observes the non-zero handle is the one that calls destroy.
        // Everyone else gets zero and no-ops. This makes
        // double-Dispose, Dispose-races-finalizer, and concurrent-
        // Dispose all safe by construction — no _disposed bool, no
        // lock, no memory-barrier reasoning.
        var handle = Interlocked.Exchange(ref _handle, IntPtr.Zero);
        if (handle != IntPtr.Zero)
        {
            HighlightNative.HighlighterDestroy(handle);
        }
        GC.SuppressFinalize(this);
    }

    ~CodeHighlighter()
    {
        // Same Interlocked.Exchange pattern as Dispose — if Dispose
        // already ran, this exchange yields zero and we no-op. If
        // Dispose was forgotten (resource leak — should not happen in
        // well-formed code), we still release the native handle.
        //
        // Calling the native destroy from a finalizer is safe: the
        // pivox_highlighter_destroy function only touches Rust-side
        // memory it owns (no managed callback, no synchronization
        // with managed state).
        var handle = Interlocked.Exchange(ref _handle, IntPtr.Zero);
        if (handle != IntPtr.Zero)
        {
            HighlightNative.HighlighterDestroy(handle);
        }
    }
}
