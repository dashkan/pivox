using System.ComponentModel;
using AppKit;
using Foundation;
using Pivox.Shared.Ai;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox.Ai;

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
    /// right-rail visual.</summary>
    private const float UserBubbleMaxWidth = 380;

    /// <summary>Horizontal padding inside the user bubble (matches
    /// SwiftUI's <c>.padding(.horizontal, 14)</c>).</summary>
    private const float BubbleHorizontalPadding = 14;

    /// <summary>Vertical padding inside the user bubble (matches
    /// SwiftUI's <c>.padding(.vertical, 10)</c>).</summary>
    private const float BubbleVerticalPadding = 10;

    private readonly Message _message;
    private readonly NSTextField _body;
    private bool _subscribed;

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
        // We DON'T set PreferredMaxLayoutWidth — that's a racy
        // pre-Auto-Layout hack. We give the body an explicit width
        // constraint below; NSTextField's intrinsicContentSize then
        // returns the height needed for that exact width.

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
        // Secondary-tinted fill at ~18% opacity — matches SwiftUI's
        // `Color.secondary.opacity(0.18)`. NSColor.SecondaryLabel is
        // appearance-aware (auto-flips light↔dark);
        // ColorWithAlphaComponent preserves that.
        bubble.Layer!.BackgroundColor = NSColor.SecondaryLabel
            .ColorWithAlphaComponent(0.18f)
            .CGColor;
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

    private void OnMessagePropertyChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (!_subscribed) return;
        if (e.PropertyName == nameof(Message.Text))
        {
            // Same-thread by Rule 12 (the VM raises events on the UI
            // thread). Safe to mutate AppKit directly.
            _body.StringValue = _message.Text;
            // NSTextField re-computes intrinsic content size on
            // StringValue change; Auto Layout picks up new height
            // automatically as long as the width constraint is set
            // (it is, in both bubble and assistant builds).
        }
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
}
