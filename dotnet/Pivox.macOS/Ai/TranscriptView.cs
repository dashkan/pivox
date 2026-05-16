using System.Collections.Specialized;
using System.ComponentModel;
using AppKit;
using CoreGraphics;
using Foundation;
using Pivox.Shared.Ai;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox.Ai;

/// <summary>
/// AppKit-backed transcript view — port of the SwiftUI native
/// <c>ConversationTranscriptView.swift</c> AppKit bridge
/// (<c>native/platform/macos/swift/AIChat/Transcript/</c>).
///
/// <para><b>Why AppKit, not SwiftUI / pure stack-view.</b> Two prior
/// dotnet attempts (NSStackView, then NSScrollView+NSTableView)
/// failed and were rolled back. Root cause was procedural — those
/// attempts didn't read the Swift bridge end-to-end first. This
/// port follows the procedural rules in
/// <c>memory/project_transcript_port_redo.md</c>: Swift first,
/// translate including comments, no silent skips, step-gated
/// verification.</para>
///
/// <para><b>Why not <c>usesAutomaticRowHeights</c>.</b> The Swift
/// bridge calls this out explicitly (lines 96–102):
/// auto-row-heights calls <c>NSHostingView.fittingSize</c> which
/// under-measures rich content. We own the height with explicit
/// measurement instead — same rule applies on the dotnet side, and
/// the C# port deliberately does not enable
/// <see cref="NSTableView.UsesAutomaticRowHeights"/>.</para>
///
/// <para><b>Bottom-pin-when-short</b> is owned by
/// <see cref="BottomPinClipView"/> (this file): when the document is
/// shorter than the viewport, <c>ConstrainBoundsRect</c> forces a
/// negative <c>origin.y</c> so the rows land flush against the
/// bottom of the visible area. NSScrollView's normal scroll-clamping
/// resumes the moment content outgrows the viewport. The override
/// is a passive filter — only invoked when AppKit actively proposes
/// a scroll change. The active driver
/// <see cref="TranscriptCoordinator.ApplyBottomPin"/> writes
/// <c>clipView.Bounds</c> directly so initial-layout and data-
/// mutation paths get the pin (Step 4 wires the observer that
/// re-drives this on every frame change).</para>
/// </summary>
internal sealed class TranscriptView : NSScrollView
{
    /// <summary>Identifier for the single message column. Matches
    /// the SwiftUI reference's
    /// <c>NSUserInterfaceItemIdentifier("message")</c> extension at
    /// the bottom of <c>ConversationTranscriptView.swift</c>.</summary>
    internal static readonly NSString MessageColumnIdentifier = new("message");

    private readonly NSTableView _tableView;
    private readonly TranscriptCoordinator _coordinator;

    public TranscriptView(ConversationViewModel vm)
    {
        ArgumentNullException.ThrowIfNull(vm);

        // ───── scroll view itself ─────────────────────────────────
        // Translated verbatim from
        // ConversationTranscriptView.swift:73–82.
        HasVerticalScroller = true;
        BorderType = NSBorderType.NoBorder;
        // Transparent background at BOTH layers: the scroll view
        // itself (which is what `DrawsBackground = false` controls)
        // AND the content view (NSClipView, which independently draws
        // `NSColor.controlBackgroundColor` by default → near-black in
        // dark mode, visible as a dark edge against the parent panel).
        DrawsBackground = false;
        BackgroundColor = NSColor.Clear;
        TranslatesAutoresizingMaskIntoConstraints = false;

        // ───── clip view replacement ─────────────────────────────
        // Swap the default clip view for our bottom-pinning subclass
        // BEFORE attaching the document view, so the documentView
        // setter wires the table to our custom clip view from the
        // start. Inherits transparent drawing.
        // (Swift lines 84–90.)
        var clipView = new BottomPinClipView { DrawsBackground = false };
        ContentView = clipView;

        // ───── table view ────────────────────────────────────────
        // Translated verbatim from
        // ConversationTranscriptView.swift:92–104.
        _tableView = new NSTableView
        {
            HeaderView = null,
            BackgroundColor = NSColor.Clear,
            SelectionHighlightStyle = NSTableViewSelectionHighlightStyle.None,
            GridStyleMask = NSTableViewGridStyle.None,
            // `.custom` row sizing → we own `tableView(_:heightOfRow:)`.
            // We deliberately do NOT use `usesAutomaticRowHeights`; it
            // calls `NSHostingView.fittingSize` which under-measures
            // rich SwiftUI content (markdown, code blocks, lists),
            // producing cells that clip their own content. This was
            // the fatal flaw in the prior AppKit attempt. The same
            // failure mode applies to the dotnet port — leaving row
            // sizing in our hands keeps the measurement honest.
            RowSizeStyle = NSTableViewRowSizeStyle.Custom,
            IntercellSpacing = new CGSize(0, ThemeMetrics.TranscriptIntercellSpacingY),
        };

        // ───── single message column ─────────────────────────────
        // Translated verbatim from
        // ConversationTranscriptView.swift:106–111.
        var column = new NSTableColumn(MessageColumnIdentifier)
        {
            // Auto-resizing so the column width tracks the scroll
            // view's content width. Our measurement functions key off
            // this width, so keeping it in sync with the viewport is
            // critical.
            ResizingMask = NSTableColumnResizing.Autoresizing,
        };
        _tableView.AddColumn(column);

        // Coordinator owns data-source + delegate AND the
        // INotifyCollectionChanged subscription. Step 3 wires data
        // binding + cell vending; Step 4 adds the frame-change
        // observers that re-drive ApplyBottomPin; Step 5 adds
        // streaming-row in-place height invalidation; Step 6 adds
        // width-change height refresh.
        _coordinator = new TranscriptCoordinator(vm, _tableView, this);
        _tableView.DataSource = _coordinator;
        _tableView.Delegate = _coordinator;

        // Setting `documentView` AFTER the clip view swap so AppKit
        // wires the document into our `BottomPinClipView`, not the
        // default NSClipView the scroll view auto-creates.
        // (Swift line 118.)
        DocumentView = _tableView;

        // Sync once with the current snapshot in case messages
        // already exist when this view is constructed (e.g., panel
        // closed and reopened mid-conversation in a future build).
        // Today the VM is brand-new with the VC; this is defensive.
        _coordinator.SyncFromCurrentSnapshot();

        // NOTE: NSNotificationCenter observer registrations
        // (`scrollViewFrameDidChange`, `tableViewFrameDidChange`,
        // `contentViewBoundsDidChange`) are deferred to Step 4+.
        // The active ApplyBottomPin already runs in the first-load
        // and tail-append paths from sync(); the observers add
        // re-application on AppKit-driven frame changes (window
        // resize, intrinsic-size shifts).
    }

    /// <summary>Detach the coordinator's
    /// <see cref="INotifyCollectionChanged"/> subscription on the
    /// VM's <c>Messages</c> collection. Call before the parent VC is
    /// destroyed — leaving the subscription attached holds the
    /// coordinator (and its referenced views) alive as long as the
    /// VM is. Safe to call multiple times. Does NOT call
    /// <see cref="NSObject.Dispose"/> on any view (Rule 13).</summary>
    public void Unsubscribe() => _coordinator.Teardown();

    /// <summary>AppKit's end-of-live-resize hook. The frame-change
    /// observer throttles RefreshVisibleRowHeights to ~10Hz during
    /// active drag (live-resize ticks at 60Hz × all visible rows ×
    /// per-row measurement cost = prohibitively expensive,
    /// especially once markdown rendering lands). This override
    /// triggers one final unthrottled refresh after the user
    /// releases the resize handle, so the final settled width has
    /// fully-correct row heights.</summary>
    public override void ViewDidEndLiveResize()
    {
        base.ViewDidEndLiveResize();
        _coordinator.OnLiveResizeEnded();
    }
}

/// <summary>
/// Clip view that bottom-pins short content.
///
/// <para><c>NSClipView.ConstrainBoundsRect</c> is AppKit's hook for
/// validating any proposed scroll position. The default
/// implementation clamps <c>origin.y</c> to <c>[0, max(0,
/// docHeight - clipHeight)]</c>, which collapses to just <c>0</c>
/// when content fits in the viewport — leaving the document
/// top-aligned with empty space below. We override to force
/// <c>origin.y = docHeight - clipHeight</c> (NEGATIVE) when the
/// document is shorter than the viewport: the clip view then
/// "looks at" a region above the document, and the document
/// renders flush against the bottom of the visible area. Same
/// effect <c>.defaultScrollAnchor(.bottom)</c> gave us in the
/// pre-AppKit SwiftUI version.</para>
///
/// <para><b>Why the coordinator drives application</b>:
/// <c>ConstrainBoundsRect</c> is a passive filter — only called
/// when a scroll change is actively proposed. On initial layout
/// and during data mutations, the clip view's origin stays at
/// <c>(0,0)</c>, AppKit never asks the override to weigh in, and
/// the bottom-pin never applies. Forcing it requires an explicit
/// <c>clipView.Bounds = clipView.ConstrainBoundsRect(clipView.Bounds)</c>
/// after the layout settles. The coordinator's
/// <see cref="TranscriptCoordinator.ApplyBottomPin"/> drives this
/// directly today (called on first-load and tail-append); Step 4
/// adds frame-change observers that re-drive it on AppKit's own
/// timing — overriding the clip view's <c>Frame</c> /
/// <c>DocumentView</c> properties to self-observe doesn't reliably
/// fire when AppKit sets those via Obj-C internals.</para>
///
/// <para>The override is a no-op once content outgrows the
/// viewport (<c>docHeight &gt;= clipHeight</c>); the base class's
/// default clamping resumes and normal scrolling works.</para>
///
/// <para><b>Dotnet-side delta from the Swift port</b>
/// (deliberate). The Swift <c>BottomPinClipView</c> does NOT
/// override <c>isFlipped</c> — it inherits the flipped coordinate
/// system from the document view (<see cref="NSTableView"/> is
/// flipped by default in AppKit, and an unflipped clip view
/// embedded in a flipped document context still yields the right
/// hit-test math via Swift's view-hierarchy implicit
/// inheritance). The prior dotnet session found the negative-bounds
/// formula's visual effect didn't take hold without an explicit
/// <see cref="IsFlipped"/> override — see
/// <c>memory/project_transcript_port_redo.md</c>. Adding it here
/// is a procedurally-mandated dotnet adaptation, narrow in scope
/// (one property), documented at the source.</para>
/// </summary>
internal sealed class BottomPinClipView : NSClipView
{
    public override bool IsFlipped => true;

    public override CGRect ConstrainBoundsRect(CGRect proposedBounds)
    {
        // Translated verbatim from
        // ConversationTranscriptView.swift:1428–1444.
        var rect = base.ConstrainBoundsRect(proposedBounds);
        if (DocumentView is not NSTableView table) return rect;

        // True content extent — `frame.height` lies (NSTableView
        // pads its frame to clip height when content is short).
        var rowCount = table.RowCount;
        if (rowCount <= 0) return rect;
        var docHeight = table.RectForRow(rowCount - 1).GetMaxY();
        var clipHeight = proposedBounds.Height;

        if (docHeight < clipHeight)
        {
            rect.Y = docHeight - clipHeight;
        }
        return rect;
    }
}

/// <summary>
/// Data-source + delegate for <see cref="TranscriptView"/>'s
/// <see cref="NSTableView"/>. Mirrors the Swift bridge's
/// <c>Coordinator</c>.
///
/// <para><b>Step 3 surface</b>: data binding (subscribe to
/// <c>_vm.Messages.CollectionChanged</c>), cell vending (wraps
/// <see cref="MessageRowView"/> in <see cref="NSTableCellView"/>),
/// height measurement with cache, plus first-load
/// <see cref="ScrollToBottom"/> + <see cref="ApplyBottomPin"/>
/// (early arrivals from Step 4 — needed for the first-load
/// bottom-anchoring case which the passive ConstrainBoundsRect
/// override doesn't cover).</para>
///
/// <para><b>Deferred to later steps</b>: frame-change observers
/// that re-drive <see cref="ApplyBottomPin"/> on AppKit timing
/// (Step 4); per-row PropertyChanged handling for streaming text
/// + sticky-bottom (Step 5); width-change height refresh
/// (Step 6).</para>
/// </summary>
internal sealed class TranscriptCoordinator : NSObject,
    INSTableViewDataSource, INSTableViewDelegate
{
    // Stored references. Strong, not weak — the coordinator is
    // owned by TranscriptView, which is the parent scroll view; the
    // table is `documentView` of that scroll view; both share a
    // lifetime. Swift uses `weak` on these because its Coordinator
    // is a SwiftUI representable's coordinator, owned independently
    // from the view it manages — different ownership graph; weak is
    // a safety-net there. Strong here is simpler and correct for
    // our shape.
    private readonly ConversationViewModel _vm;
    private readonly NSTableView _tableView;
    private readonly TranscriptView _scrollView;

    /// <summary>Render-order list of messages. In Step 3 this is just
    /// <c>_messages.ToList()</c>; the Swift bridge's <c>Row</c> sum
    /// type interleaves DEBUG-only page-boundary markers, both of
    /// which depend on pagination (deferred to Phase C/D).</summary>
    private readonly List<Message> _rows = new();

    /// <summary>Last-known snapshot of <c>_vm.Messages</c> for
    /// classification. Compared against incoming snapshots to
    /// distinguish first-load / tail-append / no-op.</summary>
    private readonly List<Message> _messages = new();

    /// <summary>Height cache keyed by <see cref="Message"/> object
    /// reference. Persists across reloads because <see cref="Message"/>
    /// instances are owned by <c>_vm.Messages</c> and not
    /// reconstructed across renders — a prepend (Phase C/D) would
    /// add new instances above without touching existing ones'
    /// identity, so cached heights stay valid.
    ///
    /// <para><b>Why object-identity, not <c>MessageId</c>.</b> Swift
    /// keys by <c>m.name</c> because <c>Pivox_Ai_V1_Message</c> is
    /// a value type with no useful object identity. The C#
    /// <see cref="Message"/> is a reference type, AND
    /// <see cref="Message.MessageId"/> is empty for locally-composed
    /// user turns (the server only assigns ids to assistant
    /// responses) — keying by MessageId would collide every user
    /// message into the same cache slot. Object identity is the
    /// stable, collision-free analog of <c>m.name</c> for our
    /// type model.</para></summary>
    private readonly Dictionary<Message, nfloat> _heightCache = new();

    /// <summary>Per-cache-entry width validity. Companion to
    /// <see cref="_heightCache"/>; entries whose noted width
    /// differs from the current effective width are stale and need
    /// re-measurement (Step 6 drives this on resize).</summary>
    private readonly Dictionary<Message, nfloat> _lastNotedWidth = new();

    /// <summary>Blocks pagination triggers until first scroll-to-
    /// bottom completes — Phase C/D territory. Pre-allocated; today
    /// it's set true in the first-load handler and read by no one.</summary>
    private bool _hasScrolledToBottomOnce;

    /// <summary>Last column width seen by the scroll-view frame
    /// observer. Width changes (panel resize, split-view drag)
    /// invalidate visible row heights via
    /// <see cref="RefreshVisibleRowHeights"/>; a tolerance gate
    /// (<c>|new - cached| &gt; 0.5</c>) suppresses noise from
    /// sub-pixel rounding. (Swift line 892.)</summary>
    private nfloat _cachedWidth;

    /// <summary>Last time <see cref="RefreshVisibleRowHeights"/>
    /// was called during a live resize. AppKit fires the
    /// frame-change observer at ~60Hz during drag; throttling to
    /// ~10Hz keeps per-tick measurement cost bounded (Swift
    /// doesn't need this because <c>NSHostingController.sizeThatFits</c>
    /// has internal layout caching). The final settled width is
    /// caught by <see cref="TranscriptView.ViewDidEndLiveResize"/>
    /// → <see cref="OnLiveResizeEnded"/>, which does one
    /// unthrottled refresh after the user releases the
    /// handle.</summary>
    private DateTime _lastResizeRefresh = DateTime.MinValue;
    private static readonly TimeSpan ResizeRefreshInterval =
        TimeSpan.FromMilliseconds(100);

    /// <summary>Latch — set the first time the document is observed
    /// to be at least as tall as the clip view. Once set,
    /// <see cref="ApplyBottomPin"/> short-circuits forever
    /// (defends against scroll-stutter regressions; see Swift
    /// lines 252–266).</summary>
    private bool _docHasEverOverflowed;

    /// <summary>Messages we've attached a <c>PropertyChanged</c>
    /// subscription to. Used to handle streaming text deltas: when
    /// a placeholder assistant message's <c>Text</c> mutates, we
    /// need to evict its cached height and re-ask NSTableView for a
    /// fresh measurement. Tracked separately from <c>_messages</c>
    /// so subscribe/unsubscribe is exactly-once even if
    /// <see cref="ApplyMessages"/> fires repeatedly with the same
    /// snapshot.
    ///
    /// <para><b>Step 5 work brought forward into Step 3.</b> The
    /// procedure split per-row subscription into Step 5, but Step
    /// 3's verification can't pass without it: the assistant
    /// placeholder enters the collection with empty Text (one-line
    /// height); deltas mutate Text in place without a
    /// CollectionChanged event; without per-row subscription, the
    /// initial single-line height is cached forever and rows clip.
    /// The Swift bridge handles this in <c>sync()</c>'s streaming-
    /// text-delta branch (lines 524–580), invoked from
    /// <c>updateNSView</c> on any observed-property change. Our
    /// trigger model is per-row PropertyChanged instead.</para></summary>
    private readonly HashSet<Message> _subscribedMessages = new();

    /// <summary>Coalesces multiple scroll-to-bottom requests within
    /// a single runloop tick into one. Per-token streaming would
    /// otherwise queue 50–100
    /// <see cref="NSObject.BeginInvokeOnMainThread"/> blocks per
    /// second, each forcing a synchronous layout pass — the source
    /// of the streaming-flicker and end-of-stream beach-ball
    /// observed in Step 3 verification.
    ///
    /// <para>The Swift bridge has the same accumulation hazard
    /// (each delta calls <c>DispatchQueue.main.async { scrollToBottom() }</c>)
    /// but lacks an explicit coalesce; it relies on
    /// <c>viewModel.stickToBottom</c> being a cheap-gated boolean
    /// for the auto-scroll branch and on SwiftUI's render diffing
    /// to absorb the redundant work. Our trigger model fires
    /// per-property-change on the Message, and our scroll path
    /// forces <c>LayoutSubtreeIfNeeded</c> +
    /// <see cref="NSTableView.Tile"/> — both heavier than the
    /// SwiftUI equivalent. Coalescing is the dotnet-side
    /// adaptation that keeps the same per-delta correctness
    /// without the accumulated cost.</para></summary>
    private bool _scrollScheduled;

    /// <summary>Rows whose cached height has been evicted and need
    /// to be re-measured. Drained in the coalesced flush — one
    /// <see cref="NSTableView.NoteHeightOfRowsWithIndexesChanged"/>
    /// call per runloop tick with the accumulated index set,
    /// instead of one call per delta. Per-delta NoteHeight is
    /// cheap on its own but each call schedules an AppKit layout
    /// pass; batching makes the layout cost track tick rate, not
    /// delta rate.</summary>
    private readonly HashSet<int> _dirtyRows = new();

    private bool _torndown;

    public TranscriptCoordinator(
        ConversationViewModel vm,
        NSTableView tableView,
        TranscriptView scrollView)
    {
        _vm = vm;
        _tableView = tableView;
        _scrollView = scrollView;

        // Subscribe to the VM's Messages collection. The handler
        // takes a snapshot and routes via ApplyMessages — same shape
        // as Swift's `sync(viewModel:)` which is invoked from
        // `updateNSView` on any observed-property change.
        _vm.Messages.CollectionChanged += OnMessagesCollectionChanged;

        // ───── frame / bounds observers ─────────────────────────
        // Translated verbatim from
        // ConversationTranscriptView.swift:126–155, using
        // (observer, selector, name, object) form per Rule 12 +
        // the prior-session writeup's explicit warning that closure
        // form silently GCs.

        // Panel resize → column width changes → row heights must be
        // recomputed against the new width for visible content.
        _scrollView.PostsFrameChangedNotifications = true;
        NSNotificationCenter.DefaultCenter.AddObserver(
            this,
            new ObjCRuntime.Selector("scrollViewFrameDidChange:"),
            NSView.FrameChangedNotification,
            _scrollView);

        // Document view frame changes → docHeight changed → bottom-
        // pin needs re-application. ConstrainBoundsRect is a passive
        // filter; we have to actively poke the clip view by writing
        // its bounds via ApplyBottomPin.
        _tableView.PostsFrameChangedNotifications = true;
        NSNotificationCenter.DefaultCenter.AddObserver(
            this,
            new ObjCRuntime.Selector("tableViewFrameDidChange:"),
            NSView.FrameChangedNotification,
            _tableView);

        // Content view bounds change on every user scroll. We use
        // this to (a) refresh heights of any rows that scrolled into
        // view at a new width since last measurement (lazy post-
        // resize fixup), and (b) update StickToBottom — the user-
        // intent flag that gates streaming auto-scroll.
        _scrollView.ContentView.PostsBoundsChangedNotifications = true;
        NSNotificationCenter.DefaultCenter.AddObserver(
            this,
            new ObjCRuntime.Selector("contentViewBoundsDidChange:"),
            NSView.BoundsChangedNotification,
            _scrollView.ContentView);
    }

    /// <summary>One-shot sync against the VM's current
    /// <c>Messages</c> snapshot. Called from the
    /// <see cref="TranscriptView"/> ctor so an existing transcript
    /// (e.g., panel reopened mid-conversation in a future build)
    /// renders without waiting for the next mutation event.</summary>
    public void SyncFromCurrentSnapshot()
    {
        ApplyMessages(_vm.Messages.ToList());
    }

    public void Teardown()
    {
        if (_torndown) return;
        _torndown = true;
        _vm.Messages.CollectionChanged -= OnMessagesCollectionChanged;
        foreach (var msg in _subscribedMessages)
        {
            msg.PropertyChanged -= OnMessagePropertyChanged;
        }
        _subscribedMessages.Clear();
        // Remove every notification observer registered with this
        // coordinator as the observer (covers scrollView frame,
        // tableView frame, contentView bounds).
        NSNotificationCenter.DefaultCenter.RemoveObserver(this);
    }

    // ────────────────────────────────────────────────────────────
    // Frame / bounds observer handlers.
    // ────────────────────────────────────────────────────────────

    /// <summary>Panel resize handler. Detects width changes and
    /// invalidates visible row heights so they re-wrap to the new
    /// width on the next layout pass. Also re-applies the bottom-
    /// pin since vertical resize changes clipHeight.
    ///
    /// <para><b>Live-resize throttle</b> — when the scroll view is
    /// inside an active drag (<see cref="NSView.InLiveResize"/>),
    /// throttle the refresh to ~10Hz. AppKit fires this observer
    /// at ~60Hz during drag, and with markdown rendering (Phase
    /// C) per-row measurement could grow to several ms — at 60Hz
    /// × N visible rows that's prohibitive. The throttled cadence
    /// keeps the UI responsive during drag, and
    /// <see cref="TranscriptView.ViewDidEndLiveResize"/> →
    /// <see cref="OnLiveResizeEnded"/> catches the final settled
    /// width with an unthrottled refresh after release.</para>
    ///
    /// <para>(Swift lines 886–898 — Swift doesn't need the
    /// throttle because <c>NSHostingController.sizeThatFits</c>
    /// has internal layout caching that amortizes per-tick cost;
    /// our NSTextField-based measurement has less caching even
    /// with the static-reuse optimization in
    /// <see cref="MessageRowView.MeasureRowHeight"/>.)</para></summary>
    [Export("scrollViewFrameDidChange:")]
    public void ScrollViewFrameDidChange(NSNotification notification)
    {
        if (_torndown) return;
        var newWidth = _scrollView.ContentSize.Width;
        // Width changes trigger height invalidation; pure height
        // changes (window vertical resize) don't need remeasurement
        // because row heights depend only on width.
        if (Math.Abs((double)(newWidth - _cachedWidth)) > 0.5)
        {
            _cachedWidth = newWidth;
            if (_scrollView.InLiveResize)
            {
                var now = DateTime.UtcNow;
                if (now - _lastResizeRefresh >= ResizeRefreshInterval)
                {
                    _lastResizeRefresh = now;
                    RefreshVisibleRowHeights();
                }
                // Else: drop this tick. OnLiveResizeEnded picks up
                // the final width.
            }
            else
            {
                RefreshVisibleRowHeights();
            }
        }
        // Vertical resize changes clipHeight → re-apply bottom-pin.
        ApplyBottomPin();
    }

    /// <summary>End-of-live-resize cleanup, invoked by
    /// <see cref="TranscriptView.ViewDidEndLiveResize"/>. Forces
    /// one final <see cref="RefreshVisibleRowHeights"/> +
    /// <see cref="ApplyBottomPin"/> against the settled width so
    /// row heights match the final geometry — catches the final
    /// resize tick that the throttle in
    /// <see cref="ScrollViewFrameDidChange"/> skipped.</summary>
    public void OnLiveResizeEnded()
    {
        if (_torndown) return;
        _cachedWidth = _scrollView.ContentSize.Width;
        RefreshVisibleRowHeights();
        ApplyBottomPin();
    }

    /// <summary>Document view frame changes (rows added, removed,
    /// height invalidated). docHeight just changed; force-apply the
    /// bottom-pin so the clip view's bounds re-evaluate against the
    /// new height. (Swift lines 904–906.)</summary>
    [Export("tableViewFrameDidChange:")]
    public void TableViewFrameDidChange(NSNotification notification)
    {
        if (_torndown) return;
        ApplyBottomPin();
    }

    /// <summary>Scroll tick handler. Two concerns: (a) refresh
    /// heights of rows newly scrolled into view at a width different
    /// from when they were last measured (companion to the resize
    /// handler's "only refresh visible" — offscreen rows are lazily
    /// refreshed here as they scroll into view); (b) update
    /// <see cref="ConversationViewModel.StickToBottom"/> based on
    /// the user's current scroll position. (Swift lines 1054–1068;
    /// the pagination-trigger branch at 1071+ is deferred.)</summary>
    [Export("contentViewBoundsDidChange:")]
    public void ContentViewBoundsDidChange(NSNotification notification)
    {
        if (_torndown) return;
        RefreshVisibleRowHeights();
        UpdateStickToBottom();
    }

    /// <summary>For each currently-visible row whose cached height
    /// was measured at a width different from the current effective
    /// width, invalidate the cache entry and note it so NSTableView
    /// re-queries at the new width. Called during live resize (to
    /// update the visible portion) and during scroll (to catch
    /// newly-visible rows after a prior resize).
    ///
    /// <para>This is THE optimization that makes resize interactive
    /// on large conversations. The naive "note all rows" approach
    /// does N synchronous measurements (≥5ms each for rich content),
    /// producing a beachball on long conversations. This version
    /// only measures the ~10 rows on screen, spreading the rest
    /// across scroll-time events. (Swift lines 1108–1127.)</para></summary>
    private void RefreshVisibleRowHeights()
    {
        var visible = _tableView.RowsInRect(_tableView.VisibleRect());
        if (visible.Length == 0) return;
        var width = EffectiveWidth(_tableView);
        NSMutableIndexSet? toNote = null;
        for (long offset = 0; offset < visible.Length; offset++)
        {
            var row = (int)(visible.Location + offset);
            if (row < 0 || row >= _rows.Count) continue;
            var msg = _rows[row];
            if (!_lastNotedWidth.TryGetValue(msg, out var notedWidth)
                || notedWidth != width)
            {
                _lastNotedWidth[msg] = width;
                _heightCache.Remove(msg);
                (toNote ??= new NSMutableIndexSet()).Add((nuint)row);
            }
        }
        if (toNote is not null)
        {
            _tableView.NoteHeightOfRowsWithIndexesChanged(toNote);
        }
    }

    /// <summary>Recomputes
    /// <see cref="ConversationViewModel.StickToBottom"/> from the
    /// current scroll position. Threshold is one viewport height
    /// (floor 120pt): the flag only flips false once the user has
    /// scrolled up far enough that the previously-visible page of
    /// content is fully off-screen. Smaller scrolls — incidental
    /// upward nudges, downward scrolls at the bottom, momentum
    /// tails — never cross the threshold, so streaming auto-scroll
    /// doesn't fight casual interactions. By the time the flag
    /// flips false, the user has unambiguously navigated away.
    /// (Swift lines 1042–1052 — verbatim semantics.)</summary>
    private void UpdateStickToBottom()
    {
        var visible = _scrollView.ContentView.DocumentVisibleRect();
        var docHeight = _tableView.Frame.Height;
        var viewportHeight = _scrollView.ContentView.Bounds.Height;
        var threshold = (nfloat)Math.Max(120.0, (double)viewportHeight);
        var nearBottom = visible.GetMaxY() >= docHeight - threshold;
        if (_vm.StickToBottom != nearBottom)
        {
            _vm.StickToBottom = nearBottom;
        }
    }

    /// <summary>Diffs the current snapshot against
    /// <see cref="_subscribedMessages"/> and attaches /
    /// detaches <c>PropertyChanged</c> handlers as needed. Subscribe
    /// is idempotent — HashSet.Add returns false for already-present
    /// members. Unsubscribe removes anything that's left the
    /// collection (Reset on org switch, future edit/delete).</summary>
    private void RefreshMessageSubscriptions(IList<Message> snapshot)
    {
        // Subscribe to new arrivals.
        foreach (var msg in snapshot)
        {
            if (_subscribedMessages.Add(msg))
            {
                msg.PropertyChanged += OnMessagePropertyChanged;
            }
        }
        // Unsubscribe from departures. Walk the subscribed set
        // looking for anything not in the snapshot.
        var snapshotSet = new HashSet<Message>(snapshot);
        List<Message>? toRemove = null;
        foreach (var msg in _subscribedMessages)
        {
            if (!snapshotSet.Contains(msg))
            {
                (toRemove ??= new()).Add(msg);
            }
        }
        if (toRemove is not null)
        {
            foreach (var msg in toRemove)
            {
                msg.PropertyChanged -= OnMessagePropertyChanged;
                _subscribedMessages.Remove(msg);
            }
        }
    }

    /// <summary>Per-row streaming handler. Fires when a Message's
    /// <c>Text</c> mutates (TextDelta path on the assistant
    /// placeholder). Evicts the cached height, asks NSTableView to
    /// re-measure the row, and schedules a scroll-to-bottom +
    /// bottom-pin re-application on the next runloop tick.
    ///
    /// <para><b>Note on the scroll gate.</b> The Swift bridge gates
    /// this scroll on <c>viewModel.stickToBottom</c> (an intent
    /// flag set true on send, flipped false only when the user
    /// scrolls past a one-viewport threshold) precisely because
    /// position-based gates fail across the cross-delta layout
    /// flicker the Swift comment at lines 528–547 describes. The
    /// C# VM has no <c>StickToBottom</c> property yet; Step 5
    /// adds it. Until then, we always scroll on text change — same
    /// "temporary expedient" the tail-append branch uses. UX
    /// limitation: scrolling up to read history mid-stream is
    /// fought by each delta. Acceptable for Step 3 verification;
    /// Step 5 fixes properly.</para>
    ///
    /// <para><b>Note on per-cell text update.</b> The cell
    /// (<see cref="MessageRowView"/>) has its OWN
    /// <c>PropertyChanged</c> subscription that updates
    /// <c>_body.StringValue</c> synchronously — that's the visual
    /// text update. The coordinator's subscription here is purely
    /// for layout (cache evict + NoteHeightOfRows + scroll). Both
    /// subscriptions fire per delta; the cell handler updates the
    /// text content, the coordinator handler updates the
    /// layout-rect that contains it. Order between them doesn't
    /// matter because NSTableView's height refresh and AppKit's
    /// text-intrinsic refresh both land in the next layout
    /// pass.</para></summary>
    private void OnMessagePropertyChanged(
        object? sender, PropertyChangedEventArgs e)
    {
        if (_torndown) return;
        if (e.PropertyName != nameof(Message.Text)) return;
        if (sender is not Message msg) return;

        var rowIdx = _rows.IndexOf(msg);
        if (rowIdx < 0) return;

        // Per-delta work: O(1) cache evict + tracking the dirty
        // row in the set. The NoteHeight + ScrollToBottom +
        // ApplyBottomPin work happens once per runloop tick via
        // ScheduleScrollToBottom — see that helper's doc for the
        // streaming-cost model.
        _heightCache.Remove(msg);
        _lastNotedWidth.Remove(msg);
        _dirtyRows.Add(rowIdx);

        ScheduleScrollToBottom();
    }

    /// <summary>Coalesces streaming-flush work via
    /// <see cref="_scrollScheduled"/>: at most one queued block
    /// per runloop tick drains all accumulated dirty rows
    /// (single batched <see cref="NSTableView.NoteHeightOfRowsWithIndexesChanged"/>
    /// call), then scrolls to bottom and re-applies the bottom-
    /// pin. With per-delta scheduling the streaming path would
    /// queue N blocks per tick (each forcing a full layout pass)
    /// and the post-stream flush would beach-ball; this throttle
    /// keeps total UI work bounded to ~tick-rate regardless of
    /// delta rate.</summary>
    private void ScheduleScrollToBottom()
    {
        if (_scrollScheduled) return;
        _scrollScheduled = true;
        BeginInvokeOnMainThread(() =>
        {
            _scrollScheduled = false;
            if (_torndown) return;

            // Drain accumulated dirty rows into a single
            // NoteHeight call. Empty when callers (first-load,
            // tail-append) don't need height invalidation —
            // ApplyBottomPin still runs.
            if (_dirtyRows.Count > 0)
            {
                var indexSet = new NSMutableIndexSet();
                foreach (var idx in _dirtyRows)
                {
                    indexSet.Add((nuint)idx);
                }
                _dirtyRows.Clear();
                _tableView.NoteHeightOfRowsWithIndexesChanged(indexSet);
            }

            // Gate the scroll on StickToBottom — the user-intent
            // flag set true on Send and flipped false by
            // UpdateStickToBottom when the user scrolls past the
            // one-viewport-up threshold. This is what lets the user
            // scroll up and read history mid-stream without being
            // yanked back by every delta. (Swift's streaming-delta
            // branch at lines 528–547 documents why this MUST be an
            // intent flag, not a position check.) ApplyBottomPin
            // always runs — it's a no-op once content has ever
            // overflowed, and the bottom-pin behavior for short
            // content is unrelated to streaming-intent gating.
            if (_vm.StickToBottom)
            {
                ScrollToBottom();
            }
            ApplyBottomPin();
        });
    }

    private void OnMessagesCollectionChanged(
        object? sender, NotifyCollectionChangedEventArgs e)
    {
        // Snapshot-and-classify — same shape as Swift's
        // `sync(viewModel:)` dispatcher. The action argument is
        // ignored: ApplyMessages distinguishes first-load vs
        // tail-append vs no-op against `_messages`. This is simpler
        // than per-NotifyCollectionChangedAction handling and
        // correct for the cases our build supports.
        ApplyMessages(_vm.Messages.ToList());
    }

    // ────────────────────────────────────────────────────────────
    // Mutation dispatcher — first-load + tail-append + no-op only.
    // Prepend (loadOlder) and other-size-change (edit, regenerate)
    // are deferred per the Step 1 mapping table.
    // ────────────────────────────────────────────────────────────

    private void ApplyMessages(IList<Message> snapshot)
    {
        // Cheap identity check — events fire for reasons unrelated
        // to our data (state-change ripples that touch the Messages
        // collection's notifier hooks). Bail fast when unchanged.
        if (MessagesUnchanged(snapshot)) return;

        // Refresh per-row PropertyChanged subscriptions before the
        // structural work below — so streaming text mutations on
        // the newly-added placeholder reach our height-invalidation
        // handler immediately, not after the next CollectionChanged
        // tick. Subscribe/unsubscribe is idempotent inside
        // RefreshMessageSubscriptions.
        RefreshMessageSubscriptions(snapshot);

        var oldCount = _messages.Count;
        var newCount = snapshot.Count;

        if (oldCount == 0 && newCount > 0)
        {
            // First page landed.
            _messages.Clear();
            _messages.AddRange(snapshot);
            RebuildRows();
            _tableView.ReloadData();
            if (!_hasScrolledToBottomOnce)
            {
                _hasScrolledToBottomOnce = true;
                // Defer to next runloop tick (coalesced via
                // ScheduleScrollToBottom): after ReloadData,
                // NSTableView needs a layout pass before row rects
                // are valid. Calling scroll synchronously lands on
                // stale (pre-layout) coordinates — you end up
                // somewhere mid-content instead of the bottom.
                // (Swift lines 624–650.) The scheduled block also
                // runs ApplyBottomPin so short conversations pin to
                // the bottom (ScrollToBottom early-returns when
                // docHeight ≤ clipHeight; ApplyBottomPin handles
                // the rest).
                ScheduleScrollToBottom();
            }
        }
        else if (newCount > oldCount)
        {
            // Tail append: user just sent a message, or a streamed
            // assistant response just committed. The user's mental
            // model is "the new message I asked for is at the
            // bottom" — keep the viewport pinned there. (Swift
            // lines 656–679.)
            //
            // The Swift bridge gates this scroll on
            // `viewModel.stickToBottom`, an intent flag set true on
            // send. The C# VM does not yet expose a StickToBottom
            // property; the gate lands in Step 5 along with the
            // streaming auto-scroll. For Step 3, we always scroll
            // to bottom on tail-append. The Swift comment at lines
            // 656–667 is explicit that intent-based gating is what
            // SHOULD drive this; treating "always" as the
            // first-cut behavior is a temporary expedient until
            // Step 5 wires StickToBottom through the VM.
            _messages.Clear();
            _messages.AddRange(snapshot);
            RebuildRows();
            _tableView.ReloadData();
            // Coalesced scroll — ScheduleScrollToBottom runs both
            // ScrollToBottom and ApplyBottomPin. Tail-append doesn't
            // make content shorter, but if we're still inside the
            // bottom-pin regime (docHasEverOverflowed not yet set),
            // the pin needs re-application against the new
            // docHeight. The latch inside ApplyBottomPin makes this
            // cheap once overflow has been observed.
            ScheduleScrollToBottom();
        }
        else if (newCount != oldCount)
        {
            // Other size changes (Reset on org switch; future
            // edit/regenerate) — structural reload, no scroll
            // change.
            _messages.Clear();
            _messages.AddRange(snapshot);
            RebuildRows();
            _tableView.ReloadData();
        }
    }

    private bool MessagesUnchanged(IList<Message> snapshot)
    {
        if (snapshot.Count != _messages.Count) return false;
        // Compare by stable identity (object reference), not by
        // full content equality — message content can be large
        // (markdown strings) and unchanging across most ticks;
        // reference comparison is O(n) cheap. Streaming-text deltas
        // mutate the same Message instance in place (same reference),
        // so this would falsely report "unchanged" — but the C# port
        // routes streaming via per-row PropertyChanged in Step 5
        // (the Swift bridge's separate textOf() check before
        // applyMessages is replaced by direct PropertyChanged
        // dispatch in our trigger model). Reference-equality is the
        // C# analog of Swift's `m.name == m.name` identity test.
        // (Swift lines 698–707.)
        for (var i = 0; i < snapshot.Count; i++)
        {
            if (!ReferenceEquals(snapshot[i], _messages[i])) return false;
        }
        return true;
    }

    /// <summary>Flattens the message snapshot into the linear
    /// <c>_rows</c> sequence NSTableView expects. Swift's version
    /// interleaves DEBUG-only page-boundary markers between
    /// <c>pageSizes</c> entries; with pagination deferred (Phase
    /// C/D), the C# version reduces to a straight copy.</summary>
    private void RebuildRows()
    {
        _rows.Clear();
        _rows.AddRange(_messages);
    }

    // ────────────────────────────────────────────────────────────
    // Scroll + bottom-pin. Both functions are early arrivals from
    // Step 4 — needed for first-load and tail-append anchoring per
    // Swift's sync()/applyMessages branches. Step 4 adds the frame-
    // change observers that re-drive ApplyBottomPin on AppKit's own
    // timing.
    // ────────────────────────────────────────────────────────────

    /// <summary>Scrolls the clip view origin to the bottom of the
    /// document.
    ///
    /// <para>Two forces have to align for this to land at the
    /// visible bottom every time:</para>
    /// <list type="number">
    /// <item><b>Measurement honesty</b> — <c>heightOfRow</c> must
    /// return the same height the cell actually renders at,
    /// including pin-actions overhead for the last assistant.
    /// Handled today by <see cref="MakeMessageCell"/> +
    /// <see cref="MeasureMessageHeight"/> being the SAME
    /// <see cref="MessageRowView"/> class — measurement and render
    /// are structurally identical (the C# port's equivalent of
    /// Swift's <c>messageBody()</c> single-source-of-truth helper).</item>
    /// <item><b>Tile freshness</b> — NSTableView's tile (the pass
    /// that turns a sequence of <c>heightOfRow</c> answers into
    /// the document-view frame) is scheduled lazily by
    /// <c>noteHeightOfRows(withIndexesChanged:)</c>. Step 5's
    /// streaming branch calls noteHeightOfRows then dispatches
    /// scrollToBottom async; <c>LayoutSubtreeIfNeeded</c> runs
    /// autolayout but does NOT drive the tile; if a delta arrives
    /// while tile is still pending, <c>frame.Height</c> reads the
    /// previous delta's value and the scroll lands one delta's
    /// growth short. Across many fast deltas the viewport drifts
    /// behind the streaming cursor — the "last response cut off
    /// behind the input box" bug.</item>
    /// </list>
    /// <para><c>tableView.Tile()</c> forces the pending tile to
    /// commit synchronously. Apple's docs discourage direct
    /// invocation ("you shouldn't have to call this") but that
    /// guidance assumes lazy commit is acceptable for the caller —
    /// for synchronous scroll-after-note it's exactly the right
    /// primitive.</para>
    ///
    /// <para>(Swift lines 377–387.)</para>
    /// </summary>
    private void ScrollToBottom()
    {
        if (_rows.Count == 0) return;
        _tableView.LayoutSubtreeIfNeeded();
        _tableView.Tile();
        var docHeight = _tableView.Frame.Height;
        var clipHeight = _scrollView.ContentView.Bounds.Height;
        if (docHeight <= clipHeight) return;
        var origin = new CGPoint(0, docHeight - clipHeight);
        _scrollView.ContentView.ScrollToPoint(origin);
        _scrollView.ReflectScrolledClipView(_scrollView.ContentView);
    }

    /// <summary>Active bottom-pin: when the document is shorter
    /// than the viewport, write the clip view's bounds origin to a
    /// NEGATIVE Y value so the viewport "looks at" coordinates
    /// above the document. The document — anchored at y=0 in
    /// flipped coordinates — then renders flush against the bottom
    /// of the visible area, with empty space above.
    ///
    /// <para>Why we don't rely on
    /// <c>BottomPinClipView.ConstrainBoundsRect</c> alone: the
    /// override is a passive filter; AppKit only invokes it when
    /// an active scroll proposal needs validation. Initial layout
    /// and ReloadData-driven docHeight changes don't trigger that
    /// path, so we have to write the bounds directly. We use
    /// <c>Bounds = ...</c> (not <c>ScrollToPoint(...)</c>) to skip
    /// AppKit's animation interpolation during streaming row
    /// growth and live-resize.</para>
    ///
    /// <para>When content overflows the viewport
    /// (<c>docHeight &gt;= clipHeight</c>), restore origin.y to 0
    /// if it's currently negative — otherwise the negative origin
    /// from a previous short-content state would persist past the
    /// threshold.</para>
    ///
    /// <para>(Swift lines 928–980 — translated verbatim, every
    /// guard, every comment carried over.)</para>
    /// </summary>
    private void ApplyBottomPin()
    {
        // Once the document has ever exceeded the clip view's
        // height, the pin is permanently disengaged for this
        // coordinator's lifetime. See `_docHasEverOverflowed`
        // for why this latch exists.
        if (_docHasEverOverflowed) return;

        // Layout-in-progress guard. `tableView.RectForRow(...)`
        // returns CGRect.Empty for rows that haven't been laid out
        // yet, which happens transiently during the noteHeightOfRows
        // / reloadData passes that scroll-time width-aware height
        // refresh triggers (Step 6). Without this guard, the table-
        // frame notification that fires mid-relayout would read
        // docHeight as 0, decide content is "short", and snap
        // origin.y to `-clipHeight`. A follow-up frame-change
        // notification fires after layout settles and we apply the
        // pin then.
        var rowCount = _tableView.RowCount;
        if (rowCount <= 0) return;
        var lastRect = _tableView.RectForRow(rowCount - 1);
        if (lastRect.GetMaxY() <= 0) return;

        var clipView = _scrollView.ContentView;
        // True content extent (bottom of last row in document
        // coords). We deliberately don't use
        // `documentView.Frame.Height` — NSTableView pads its own
        // frame to the clip height when content is short, so
        // frame.Height always reads >= clipHeight and the
        // bottom-pin check would never trigger.
        var docHeight = lastRect.GetMaxY();
        var clipHeight = clipView.Bounds.Height;

        // Latch first. Even on a "no-op" overflow path (origin
        // already 0, content fills viewport), set the flag so
        // future calls during scroll-driven height refresh skip
        // the function entirely. Swift's docHasEverOverflowed
        // setter has an additional `clipHeight > 0` guard
        // implicit in the `lastRect.maxY > 0` check above —
        // both conditions guard the transient zero during pre-
        // layout.
        if (docHeight >= clipHeight)
        {
            _docHasEverOverflowed = true;
        }

        var newBounds = clipView.Bounds;
        if (docHeight < clipHeight)
        {
            newBounds.Y = docHeight - clipHeight;
        }
        else if (clipView.Bounds.Y < 0)
        {
            newBounds.Y = 0;
        }
        else
        {
            return;
        }
        if (!newBounds.Equals(clipView.Bounds))
        {
            clipView.Bounds = newBounds;
            _scrollView.ReflectScrolledClipView(clipView);
        }
    }

    // ────────────────────────────────────────────────────────────
    // Cell construction.
    // ────────────────────────────────────────────────────────────

    /// <summary>Constructs a fresh <see cref="MessageRowView"/> per
    /// cell. NSTableView reuses cell views via
    /// <c>MakeView(WithIdentifier:owner:)</c>, but we don't use that
    /// API here because each message has unique content (different
    /// text, future markdown layout) and reuse would mean swapping
    /// the underlying message — which requires a new ctor path on
    /// MessageRowView and complicates the
    /// <see cref="Message.PropertyChanged"/> subscription lifecycle.
    /// NSTableView's windowing still gives us the memory win: only
    /// visible cells exist; offscreen cells are released when they
    /// scroll out of view, and MessageRowView's
    /// <c>WillMoveToSuperview(null)</c> override unhooks its
    /// PropertyChanged subscription at that point.
    ///
    /// <para>The Swift port has an <c>actionsVisibility</c>
    /// parameter here as a single source of truth shared with
    /// <c>measureMessageHeight</c>. Action row is deferred in this
    /// build (no thumbs / copy / redo yet) — the parameter has no
    /// consumer; same drift hazard the Swift comment warned about
    /// (lines 1215–1235) doesn't apply because there's nothing to
    /// drift on. When actions land, this path takes the parameter
    /// AND <see cref="MeasureMessageHeight"/> takes it from the
    /// same source.</para>
    /// </summary>
    private NSView MakeMessageCell(Message msg)
        => WrapInCell(new MessageRowView(msg));

    /// <summary>Wraps a content view in an
    /// <see cref="NSTableCellView"/> with four-edge auto-layout
    /// pinning. NSTableView expects its cells to be
    /// <see cref="NSTableCellView"/> (or a subclass); hosting the
    /// content view directly works in some macOS versions but
    /// breaks cell reuse / recycling in others.
    ///
    /// <para>(Swift lines 1247–1258.)</para></summary>
    private static NSView WrapInCell(NSView content)
    {
        content.TranslatesAutoresizingMaskIntoConstraints = false;
        var cell = new NSTableCellView();
        cell.AddSubview(content);
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            content.LeadingAnchor.ConstraintEqualTo(cell.LeadingAnchor),
            content.TrailingAnchor.ConstraintEqualTo(cell.TrailingAnchor),
            content.TopAnchor.ConstraintEqualTo(cell.TopAnchor),
            content.BottomAnchor.ConstraintEqualTo(cell.BottomAnchor),
        });
        return cell;
    }

    // ────────────────────────────────────────────────────────────
    // Measurement.
    // ────────────────────────────────────────────────────────────

    // Cache key for a message row is just the Message object
    // reference itself (see _heightCache doc for rationale). Swift's
    // `cacheKey(for:actionsInLayout:)` additionally buckets by
    // action-row presence because the action row's presence affects
    // measured height; with the action row deferred (no thumbs /
    // copy / redo in this build), there's nothing to distinguish.
    // When actions land in Phase C, switch _heightCache /
    // _lastNotedWidth to keyed-by-(Message, bool) tuples and update
    // the cache hit-check below in the same commit.
    // (Swift lines 1276–1281.)

    /// <summary>Authoritative column width, with a best-effort
    /// fallback. The column width is authoritative because rows
    /// are laid out at that width; the scroll view's
    /// <c>ContentSize.Width</c> is only used as a fallback during
    /// early initialization before the column has picked up its
    /// autoresized width.
    ///
    /// <para>(Swift lines 1332–1337 — verbatim.)</para></summary>
    private nfloat EffectiveWidth(NSTableView? tableView = null)
    {
        var table = tableView ?? _tableView;
        var colWidth = table.TableColumns().FirstOrDefault()?.Width ?? 0;
        if (colWidth > 1) return colWidth;
        return (nfloat)Math.Max((double)_scrollView.ContentSize.Width, 1.0);
    }

    /// <summary>Delegates to <see cref="MessageRowView.MeasureRowHeight"/>
    /// — measurement lives next to the render path in
    /// <see cref="MessageRowView"/> so the role-aware width math,
    /// font role, and padding constants have a single source of
    /// truth. See that method's doc for the bounding-rect rationale
    /// and the Phase C revisit point (markdown rendering).
    ///
    /// <para>An earlier cut of this method allocated a fresh
    /// <see cref="MessageRowView"/> per measurement and read
    /// <see cref="NSView.FittingSize"/> after seeding the frame and
    /// running two layout passes — the intent was structural
    /// identity-with-render via shared class. Empirically the
    /// detached view's Auto Layout solver doesn't fully resolve
    /// without window context, so
    /// <see cref="NSTextField.PreferredMaxLayoutWidth"/> invalidation
    /// never propagates and FittingSize returns the single-line
    /// intrinsic. The current shape preserves identity-with-render
    /// at the constants + font + wrap-mode level (which is the
    /// drift surface that mattered) without needing the layout-
    /// engine roundtrip.</para></summary>
    private static nfloat MeasureMessageHeight(Message msg, nfloat width)
        => MessageRowView.MeasureRowHeight(msg, width);

    // ────────────────────────────────────────────────────────────
    // INSTableViewDataSource + INSTableViewDelegate.
    // ────────────────────────────────────────────────────────────

    [Export("numberOfRowsInTableView:")]
    public nint GetRowCount(NSTableView tableView) => _rows.Count;

    /// <summary>NSTableView calls this repeatedly during layout
    /// (every row, not just visible ones, to compute total content
    /// height for the scrollbar). Cache lookups must be fast;
    /// re-measurement happens only on miss (cache keyed by message
    /// name and re-checked against current width via
    /// <see cref="_lastNotedWidth"/>).
    ///
    /// <para>(Swift lines 1141–1158.)</para></summary>
    [Export("tableView:heightOfRow:")]
    public nfloat GetRowHeight(NSTableView tableView, nint row)
    {
        var idx = (int)row;
        if (idx < 0 || idx >= _rows.Count) return 1;
        var msg = _rows[idx];
        var width = EffectiveWidth(tableView);
        if (_lastNotedWidth.TryGetValue(msg, out var notedWidth)
            && notedWidth == width
            && _heightCache.TryGetValue(msg, out var cached))
        {
            return cached;
        }
        var measured = MeasureMessageHeight(msg, width);
        _heightCache[msg] = measured;
        _lastNotedWidth[msg] = width;
        return measured;
    }

    [Export("tableView:viewForTableColumn:row:")]
    public NSView GetViewForItem(
        NSTableView tableView, NSTableColumn tableColumn, nint row)
    {
        var idx = (int)row;
        if (idx < 0 || idx >= _rows.Count) return null!;
        return MakeMessageCell(_rows[idx]);
    }
}
