using System.ComponentModel;
using AppKit;
using Foundation;
using Pivox.Shared.Ai;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox.Ai;

// NOTE: This class is intentionally retained in the codebase despite
// having no consumers in the current build. The transcript renderer
// was rolled back to a placeholder state at the end of the last
// session (two porting attempts — NSStackView and NSScrollView+NSTableView
// — both produced broken output). The visual shape captured here
// (role-distinct rows, user-bubble at accent-tint with 380pt cap,
// assistant full-width plain text) is correct against the SwiftUI
// source at native/.../AIElements/Components/Message/Message.swift
// and is the right cell-content shape for whatever transcript
// primitive the next session picks. Keeping the file saves the
// next port from re-deriving the bubble layout. See the project
// memory note for the broader rationale.

/// <summary>
/// One row in the chat transcript. Two visual shapes by role, mirroring
/// the SwiftUI native chat (<c>native/.../AIElements/Components/Message/Message.swift</c>):
///
/// <list type="bullet">
/// <item><b>User</b>: right-aligned pill bubble, capped at <see cref="UserBubbleMaxWidth"/>pt
///   wide, secondary-tinted fill, rounded corners (14pt — matches the
///   SwiftUI reference). Padding 14h × 10v. The bubble sizes to
///   content up to the cap; a flex spacer on the leading edge
///   pushes the bubble against the trailing edge of the row.</item>
/// <item><b>Assistant</b>: left-aligned, full-width plain text. No
///   bubble chrome. Same background as the panel (assistant turns
///   feel like the AI is "speaking into" the panel, not putting up
///   chrome of its own — the SwiftUI reference is explicit about
///   this).</item>
/// </list>
///
/// <para><b>Width determination.</b> Each row is sized by the
/// transcript's <see cref="ChatPanelViewController.AppendRow"/> via
/// <c>row.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor, 1,
/// -2*inset)</c> (Rule 15 in <c>dotnet/CLAUDE.md</c>). The body's
/// height is intrinsic — driven by a width constraint on the
/// wrapping <c>NSTextField</c> rather than the racier
/// <c>PreferredMaxLayoutWidth</c> property. Explicit width
/// constraints break the chicken-and-egg between "row width depends
/// on body height" and "body height depends on body width."</para>
///
/// <para>Streaming-assistant rows mutate their label in place by
/// subscribing to <see cref="Message.PropertyChanged"/>;
/// <see cref="Unsubscribe"/> detaches before the row leaves the
/// transcript. Do NOT call <c>Dispose</c> on the <see cref="NSView"/>
/// peer — Rule 13 (AppKit owns native lifetime; GC reaps the managed
/// peer after AppKit's release path).</para>
///
/// <para>Markdown rendering, hover-reveal action icons, and the
/// thinking-indicator placeholder are Phase C surface — not present
/// here. Phase B step 2b ships plain text on both sides.</para>
/// </summary>
internal sealed class MessageRowView : NSView
{
    /// <summary>Maximum width of the user bubble. The SwiftUI ref
    /// uses 520pt; we cap tighter (380pt) for the trailing inspector
    /// which sits at 280–520pt overall. A bubble that fills the
    /// inspector edge-to-edge reads as "system message" rather than
    /// "user turn"; capping below the panel width preserves the
    /// right-rail visual.
    ///
    /// <para><b>Internal visibility</b>: the transcript coordinator
    /// reads this for measurement so render and measure use the same
    /// cap. Adding a second source of truth (a parallel constant in
    /// the coordinator) would let the two values drift — exactly the
    /// drift class Swift's <c>messageBody()</c> helper exists to
    /// prevent. Keeping the constant here makes MessageRowView the
    /// single source for user-bubble geometry.</para></summary>
    internal const float UserBubbleMaxWidth = 380;

    /// <summary>Horizontal padding inside the user bubble (matches
    /// SwiftUI's <c>.padding(.horizontal, 14)</c>). Internal so the
    /// transcript coordinator's measurement helper reads the same
    /// value the render path uses.</summary>
    internal const float BubbleHorizontalPadding = 14;

    /// <summary>Vertical padding inside the user bubble (matches
    /// SwiftUI's <c>.padding(.vertical, 10)</c>). Internal so the
    /// transcript coordinator's measurement helper reads the same
    /// value the render path uses.</summary>
    internal const float BubbleVerticalPadding = 10;

    private readonly Message _message;
    private readonly NSTextField _body;
    private bool _subscribed;

    /// <summary>Coalesces streaming-text PropertyChanged events to
    /// at most one <see cref="NSTextField"/> update per runloop
    /// tick. Without this, fast deltas (50–100/sec) each trigger a
    /// synchronous NSTextField intrinsic recompute against the
    /// growing text, which is O(text length) per delta — cumulative
    /// O(N²) over the stream. With coalesce, the body picks up the
    /// latest text once per runloop tick (≤60Hz), bounding UI cost
    /// regardless of delta rate. SwiftUI's <c>Text</c> view has the
    /// same bounding via the render-pass batching SwiftUI does
    /// implicitly per frame; AppKit needs the throttle to be
    /// explicit.</summary>
    private bool _textUpdateScheduled;

    public MessageRowView(Message message)
    {
        _message = message;
        TranslatesAutoresizingMaskIntoConstraints = false;

        _body = NSTextField.CreateWrappingLabel(message.Text);
        _body.Font = ThemeFonts.NS(ThemeFont.Body);
        _body.TextColor = ThemeColors.NS(ThemeColor.Foreground);
        _body.TranslatesAutoresizingMaskIntoConstraints = false;
        _body.LineBreakMode = NSLineBreakMode.ByWordWrapping;
        _body.UsesSingleLineMode = false;

        if (message.Role == MessageRole.User)
        {
            BuildUserBubble();
        }
        else
        {
            BuildAssistantRow();
        }

        // Subscribe AFTER initial Text is set so we don't get a
        // redundant notification for the initial value.
        _message.PropertyChanged += OnMessagePropertyChanged;
        _subscribed = true;
    }

    /// <summary>Right-aligned pill bubble. The bubble hugs its
    /// content; a flex spacer on the leading side fills the
    /// remaining row width, pushing the bubble against the trailing
    /// edge. Body has an explicit max-width constraint so wrapping
    /// kicks in cleanly.</summary>
    private void BuildUserBubble()
    {
        var bubble = new NSView { TranslatesAutoresizingMaskIntoConstraints = false };
        bubble.WantsLayer = true;
        // Accent-tinted fill at 12% — matches the SwiftUI theme's
        // `userBubble` token (Color.accentColor.opacity(0.12)).
        // The previous secondary-label tint was wrong; user turns
        // should read with the accent flavor.
        bubble.Layer!.BackgroundColor = ThemeColors.NS(ThemeColor.UserBubble).CGColor;
        bubble.Layer.CornerRadius = ThemeMetrics.ChatBubbleCornerRadius;
        bubble.AddSubview(_body);

        // Spacer: zero-intrinsic NSView that absorbs the leading
        // free space. Low horizontal content-hugging priority so
        // the layout solver picks "spacer grows, bubble shrinks to
        // content" over "bubble grows, spacer shrinks to zero."
        var spacer = new NSView { TranslatesAutoresizingMaskIntoConstraints = false };
        spacer.SetContentHuggingPriorityForOrientation(
            1, NSLayoutConstraintOrientation.Horizontal);

        AddSubview(spacer);
        AddSubview(bubble);

        // Maximum width the body can occupy inside the bubble.
        const float bodyMaxWidth = UserBubbleMaxWidth - 2 * BubbleHorizontalPadding;

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            // Spacer fills the leading edge.
            spacer.LeadingAnchor.ConstraintEqualTo(LeadingAnchor),
            spacer.TopAnchor.ConstraintEqualTo(TopAnchor),
            spacer.BottomAnchor.ConstraintEqualTo(BottomAnchor),
            spacer.TrailingAnchor.ConstraintEqualTo(bubble.LeadingAnchor),

            // Bubble pinned to trailing edge, content-sized.
            bubble.TrailingAnchor.ConstraintEqualTo(TrailingAnchor),
            bubble.TopAnchor.ConstraintEqualTo(TopAnchor),
            bubble.BottomAnchor.ConstraintEqualTo(BottomAnchor),

            // Body inside the bubble with explicit padding.
            _body.LeadingAnchor.ConstraintEqualTo(
                bubble.LeadingAnchor, BubbleHorizontalPadding),
            _body.TrailingAnchor.ConstraintEqualTo(
                bubble.TrailingAnchor, -BubbleHorizontalPadding),
            _body.TopAnchor.ConstraintEqualTo(
                bubble.TopAnchor, BubbleVerticalPadding),
            _body.BottomAnchor.ConstraintEqualTo(
                bubble.BottomAnchor, -BubbleVerticalPadding),

            // Body wraps at the cap width. NSTextField with this
            // constraint computes intrinsic height for the wrapped
            // text, so the bubble's height (driven by body) settles
            // naturally. Without the cap, short strings produce
            // tiny bubbles ("hello" wraps to 1 character wide
            // without it — exactly the bug visible in the prior
            // screenshot).
            _body.WidthAnchor.ConstraintLessThanOrEqualTo(bodyMaxWidth),
        });
    }

    /// <summary>Left-aligned, full-width plain text. No bubble chrome
    /// — the SwiftUI reference explicitly skips bubble styling for
    /// assistant turns so markdown content (Phase C) renders against
    /// the panel surface without extra visual containment.</summary>
    private void BuildAssistantRow()
    {
        AddSubview(_body);

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            _body.LeadingAnchor.ConstraintEqualTo(LeadingAnchor),
            // Body fills the row width exactly. NSTextField wrapping
            // uses this width to compute intrinsic height.
            _body.TrailingAnchor.ConstraintEqualTo(TrailingAnchor),
            _body.TopAnchor.ConstraintEqualTo(TopAnchor),
            _body.BottomAnchor.ConstraintEqualTo(BottomAnchor),
        });
    }

    /// <summary>NSTextField wrapping requires
    /// <c>PreferredMaxLayoutWidth</c> to compute the intrinsic height
    /// for wrapped content. The width-constraint alone is necessary
    /// but not sufficient — AppKit caches the single-line intrinsic
    /// content size until <c>PreferredMaxLayoutWidth</c> is set
    /// (then re-measures against that). Recompute on every layout
    /// pass so width changes (split-view drag, window resize)
    /// re-wrap correctly.
    ///
    /// <para><b>Order matters.</b> Read the row's settled
    /// <see cref="NSView.Bounds"/> BEFORE invalidating intrinsic
    /// size — the row's width is set by Auto Layout from the
    /// transcript stack's width constraint, independent of the
    /// body's intrinsic. The body's <see cref="NSView.Frame"/> may
    /// still reflect the previous pass's value (especially the
    /// stale single-line intrinsic) at the time
    /// <see cref="Layout"/> is invoked. Use the row's Bounds (the
    /// authoritative width) and compute the body's available area
    /// from it, accounting for the bubble's padding for the user
    /// case.</para></summary>
    public override void Layout()
    {
        var rowWidth = (float)Bounds.Width;
        if (rowWidth > 0)
        {
            float available;
            if (_message.Role == MessageRole.User)
            {
                // User bubble caps at UserBubbleMaxWidth, then the
                // body inside accounts for horizontal padding.
                available = Math.Min(rowWidth, UserBubbleMaxWidth)
                    - 2 * BubbleHorizontalPadding;
            }
            else
            {
                // Assistant fills the row.
                available = rowWidth;
            }

            if (available > 0
                && Math.Abs((float)_body.PreferredMaxLayoutWidth - available) > 0.5)
            {
                _body.PreferredMaxLayoutWidth = available;
                // Setting PreferredMaxLayoutWidth invalidates the
                // cached intrinsic size; AppKit re-lays-out on the
                // next pass with the wrapped height.
                _body.InvalidateIntrinsicContentSize();
            }
        }
        base.Layout();
    }

    /// <summary>AppKit's natural detach hook. Called when the cell
    /// is about to be removed from its parent view (which NSTableView
    /// does both when scrolling rows out of the visible window and
    /// when a structural change like <c>ReloadData</c> rebuilds the
    /// cell pool). Unsubscribing here prevents
    /// <see cref="Message.PropertyChanged"/> from rooting orphaned
    /// cells past their visible lifetime — the SwiftUI reference
    /// uses <c>NSHostingView&lt;Message&gt;</c> whose SwiftUI render
    /// tree is torn down by SwiftUI itself on removal, so the issue
    /// doesn't surface there. Our plain AppKit cell needs the
    /// explicit hook. Newly-visible cells are constructed fresh by
    /// <c>tableView(_:viewForTableColumn:row:)</c> with a current
    /// <c>Message.Text</c> snapshot, so no missed-update window:
    /// scroll-back picks up the latest text in the new cell's ctor.</summary>
    public override void ViewWillMoveToSuperview(NSView? newSuperview)
    {
        base.ViewWillMoveToSuperview(newSuperview);
        if (newSuperview is null) Unsubscribe();
    }

    private void OnMessagePropertyChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (!_subscribed) return;
        if (e.PropertyName != nameof(Message.Text)) return;

        // Coalesce per-runloop-tick — multiple deltas within one
        // tick share a single body update. The block reads
        // `_message.Text` at flush time, so it always picks up the
        // latest streamed value regardless of how many deltas
        // landed between schedule and flush. See _textUpdateScheduled
        // doc for the cost model that motivates the throttle.
        if (_textUpdateScheduled) return;
        _textUpdateScheduled = true;
        BeginInvokeOnMainThread(() =>
        {
            _textUpdateScheduled = false;
            if (!_subscribed) return;
            // Same-thread by Rule 12 (the VM raises events on the UI
            // thread; BeginInvokeOnMainThread also runs on the UI
            // thread). Safe to mutate AppKit directly.
            _body.StringValue = _message.Text;
            // NSTextField re-computes intrinsic content size on
            // StringValue change; Auto Layout picks up new height
            // automatically as long as the width constraint is set
            // (it is, in both bubble and assistant builds).
        });
    }

    /// <summary>Detach this row's <see cref="Message.PropertyChanged"/>
    /// subscription. Call BEFORE <see cref="NSView.RemoveFromSuperview"/>
    /// when the row is being torn out of the transcript. Do NOT call
    /// <see cref="NSObject.Dispose"/> on this view — AppKit's
    /// <c>NSView</c> peer is managed by AppKit's release path
    /// (see Rule 13 in <c>dotnet/CLAUDE.md</c>); disposing the
    /// managed peer while AppKit still holds the native side is a
    /// UAF. GC reaps the peer once AppKit releases the native side;
    /// this method only handles the *managed-side* event-handler
    /// reference that would otherwise root the row past its visible
    /// lifetime.</summary>
    public void Unsubscribe()
    {
        if (!_subscribed) return;
        _message.PropertyChanged -= OnMessagePropertyChanged;
        _subscribed = false;
    }

    /// <summary>Computes the rendered height of a message row at the
    /// given row width. Co-located with the render path so the
    /// per-role text width, font, and padding constants have a
    /// single source of truth — the structural equivalent of Swift's
    /// <c>messageBody()</c> single-source helper shared by
    /// <c>makeMessageCell</c> and <c>measureMessageHeight</c>.
    ///
    /// <para><b>Why this and not detached-MessageRowView
    /// measurement.</b> The first cut of the C# port instantiated a
    /// fresh <see cref="MessageRowView"/> per measurement, set a
    /// width anchor, called <c>LayoutSubtreeIfNeeded</c>, and read
    /// <c>FittingSize.Height</c>. That works for SwiftUI's
    /// <c>NSHostingController.sizeThatFits</c> (the Swift bridge's
    /// equivalent) because the hosting controller auto-provides
    /// window context for its SwiftUI tree; <c>NSView</c>'s
    /// constraint solver requires a window to fully drive Auto
    /// Layout, and a detached row never gets the multi-pass
    /// resolution that <see cref="Layout"/>'s
    /// <c>PreferredMaxLayoutWidth</c> set + intrinsic invalidation
    /// depend on. Result: measured height equals one line, rows
    /// clip everything past the first line.</para>
    ///
    /// <para><b>Why <see cref="NSAttributedString"/> bounding-rect
    /// preserves identity-with-render.</b> Apple's text layout
    /// engine sits below <see cref="NSTextField"/>'s intrinsic
    /// sizing — the same machinery that <c>NSTextField</c> uses to
    /// compute its <c>IntrinsicContentSize</c> with PMW set is what
    /// <see cref="NSAttributedString.GetBoundingRect(CGSize, NSStringDrawingOptions)"/>
    /// returns directly. Same font, same wrap mode, same target
    /// width → same wrapped height, pixel-for-pixel. The
    /// measurement reads the same <see cref="ThemeFonts.NS"/>
    /// font role and the same role-aware width math the live
    /// <see cref="Layout"/> override uses, so changes to either
    /// land in one place.</para>
    ///
    /// <para><b>Phase C revisit point.</b> When markdown rendering
    /// lands, the live cell stops being a plain <see cref="NSTextField"/>
    /// and bounding-rect measurement no longer corresponds to the
    /// rendered height (code blocks, lists, blockquotes have their
    /// own geometry). At that point this helper takes a new path
    /// — either a renderer-specific measurement primitive, or
    /// off-screen rendering into a backed view. The
    /// single-source-of-truth shape (one method, both paths route
    /// through it) stays the same.</para></summary>
    public static nfloat MeasureRowHeight(Message message, nfloat rowWidth)
    {
        ArgumentNullException.ThrowIfNull(message);

        // Per-role text-width and vertical-padding math — kept in
        // lockstep with Layout()'s available-width calc above.
        nfloat textWidth;
        nfloat verticalPadding;
        if (message.Role == MessageRole.User)
        {
            // Mirror Layout()'s user-case:
            //   available = min(rowWidth, UserBubbleMaxWidth) - 2*BubbleHorizontalPadding
            // Plus the bubble's vertical padding (top + bottom).
            textWidth = (nfloat)Math.Min(
                (double)rowWidth, UserBubbleMaxWidth) - 2 * BubbleHorizontalPadding;
            verticalPadding = 2 * BubbleVerticalPadding;
        }
        else
        {
            // Assistant fills the row width; no bubble padding.
            textWidth = rowWidth;
            verticalPadding = 0;
        }

        // Guard against zero-or-negative widths (pre-layout, very
        // narrow panel). 1pt floor matches the live cell's fallback
        // behavior — NSTextField won't render at width=0 either.
        if (textWidth < 1) textWidth = 1;

        var label = GetSizingLabel();
        label.StringValue = message.Text ?? "";
        // Setting PMW invalidates the cached intrinsic, then
        // reading IntrinsicContentSize forces recomputation against
        // the new PMW. The NSTextField returns (PMW, wrapped_height).
        label.PreferredMaxLayoutWidth = textWidth;
        var intrinsic = label.IntrinsicContentSize;

        var measured = intrinsic.Height + verticalPadding;
        return (nfloat)Math.Max((double)measured, 1.0);
    }

    /// <summary>Shared, reused sizing label — one NSTextField for
    /// every <see cref="MeasureRowHeight"/> call instead of a fresh
    /// allocation per measurement.
    ///
    /// <para><b>Why reuse.</b> Per-delta streaming triggers
    /// per-delta height re-measurement (cache invalidation +
    /// NSTableView re-asking heightOfRow). Live resize fires the
    /// frame-change observer at 60Hz, each tick re-measuring all
    /// visible rows. With fresh-per-call allocation, each
    /// measurement costs an NSTextField init + cell-tree setup —
    /// individually small (~0.5–1ms) but cumulatively high at high
    /// invocation rates, especially when markdown rendering lands
    /// in Phase C (per-cell measurement could grow to several ms).
    /// Reusing one NSTextField for all measurements — same font,
    /// same wrap mode, same PMW-driven intrinsic protocol — cuts
    /// per-measurement cost to just the text-layout work that
    /// can't be amortized.</para>
    ///
    /// <para><b>Why this is the structural analog of Swift's
    /// <c>sizingHost</c>.</b> The Swift bridge keeps a single
    /// reused <c>NSHostingController</c> (<c>sizingHost</c>) and
    /// swaps its <c>rootView</c> per measurement; the host's
    /// SwiftUI render-tree state is reused across calls. Our static
    /// NSTextField achieves the same per-call amortization at the
    /// NSTextField layer — the renderer that backs both the live
    /// cell and the measurement.</para>
    ///
    /// <para><b>Threading.</b> AppKit objects are main-thread-only.
    /// <see cref="MeasureRowHeight"/> is always called from
    /// NSTableView's <c>heightOfRow</c> delegate path, which runs
    /// on the main thread (Rule 12). Lazy init on first call is
    /// thus also main-thread; no cross-thread race.</para></summary>
    private static NSTextField? _sizingLabel;

    private static NSTextField GetSizingLabel()
    {
        if (_sizingLabel is null)
        {
            var label = NSTextField.CreateWrappingLabel("");
            label.Font = ThemeFonts.NS(ThemeFont.Body);
            label.LineBreakMode = NSLineBreakMode.ByWordWrapping;
            label.UsesSingleLineMode = false;
            _sizingLabel = label;
        }
        return _sizingLabel;
    }
}
