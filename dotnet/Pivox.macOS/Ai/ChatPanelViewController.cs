using System.Collections.Specialized;
using System.ComponentModel;
using AppKit;
using CoreGraphics;
using Foundation;
using Pivox.Shared.Ai;
using Pivox.Shared.Organization;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox.Ai;

/// <summary>
/// macOS chat panel — the trailing inspector pane of the shell
/// window. Visual reference: SwiftUI native
/// <c>AIChat/Window/AIChatContainerView.swift</c> +
/// <c>AIChat/Transcript/ConversationView.swift</c>. Stacked
/// top-to-bottom:
///
/// <list type="number">
/// <item><b>Header strip</b> — history button (left, opens
///   conversation list popover in Phase D), title label (center,
///   currently static "New Conversation"), new-conversation button
///   + detach button (right, Phase D wires them).</item>
/// <item><b>Hairline divider</b>.</item>
/// <item><b>Transcript</b> — NSStackView of <see cref="MessageRowView"/>
///   in NSScrollView. Empty-state icon + heading when no messages.</item>
/// <item><b>Status line</b> — sending / streaming / error.</item>
/// <item><b>Composer</b> — <see cref="ChatComposerView"/> with the
///   animated shimmer border, attachment button, hint, and circular
///   send button.</item>
/// </list>
///
/// <para><b>Phase B step 2b scope</b>: chrome only. Markdown
/// rendering / code blocks / hover-reveal action icons / history
/// popover / title editing / attachment menu are Phase C/D.</para>
///
/// <para><b>No-org gating</b>: the toolbar item and ⇧⌘A menu
/// binding validate against <c>ActiveOrganization.Current</c>; this
/// VC itself doesn't gate. If a stale state lets SendAsync run
/// without an org, the VM's precondition surfaces
/// <see cref="ChatErrorKind.NoOrganization"/> in the status line.</para>
/// </summary>
public sealed class ChatPanelViewController : NSViewController
{
    private readonly ConversationViewModel _vm;

    private IconButton _historyButton = null!;
    private NSTextField _titleLabel = null!;
    private IconButton _newConversationButton = null!;
    private IconButton _detachButton = null!;
    private NSBox _headerDivider = null!;
    private FlippedStackView _transcriptStack = null!;
    private NSScrollView _transcriptScroll = null!;
    private NSView _emptyStateView = null!;
    private NSTextField _statusLabel = null!;
    private ChatComposerView _composer = null!;

    public ChatPanelViewController(ConversationViewModel vm)
        : base((string?)null, null)
    {
        _vm = vm;
    }

    public override void LoadView()
    {
        // Initial frame is a sensible default for the trailing
        // inspector. NSSplitViewController honors its constraints
        // once attached, but a non-zero LoadView frame avoids a
        // zero-size first layout.
        //
        // Don't set an explicit Layer.BackgroundColor here. The
        // detail pane's NSViewController doesn't set one either, so
        // it inherits NSWindow.backgroundColor (= NSColor.WindowBackground
        // by default). Explicit layer-background on the chat panel
        // produced a different rendered tone than the layer-less
        // detail pane (compositor vs. CG draw path) — the SwiftUI
        // ref has both panes at the same color because both flow
        // through theme.background = windowBackgroundColor. Match
        // the SwiftUI shape by letting AppKit fill via the window's
        // background.
        View = new NSView(new CGRect(0, 0, 360, 600));
    }

    public override void ViewDidLoad()
    {
        base.ViewDidLoad();
        BuildHeader();
        BuildTranscript();
        BuildEmptyState();
        BuildStatus();
        BuildComposer();
        ArrangeLayout();
        SubscribeToVm();
        // Seed the transcript with any messages already on the VM.
        // At Phase B step 2b this is usually empty; sets up the
        // path for re-entry once history persistence lands (Phase D).
        foreach (var m in _vm.Messages)
        {
            AppendRow(m);
        }
        UpdateEmptyStateVisibility();
        UpdateComposerState();
        UpdateStatusLine();
    }

    // ───── construction ────────────────────────────────────────

    private void BuildHeader()
    {
        // History button — Phase D wires this to a ConversationListPopover
        // (mirrors AIChatPanel.swift line 105-128). For now it's a no-op
        // visually present so the layout matches.
        _historyButton = new IconButton(
            systemSymbolName: "clock.arrow.circlepath",
            accessibilityLabel: "Show conversation history",
            toolTip: "Conversation history");
        _historyButton.Activated += (_, _) =>
        {
            // Phase D: present the conversation-list popover.
        };

        _titleLabel = NSTextField.CreateLabel("New Conversation");
        // RowTitle = body.semibold — matches SwiftUI's
        // `Text("New Conversation").font(.headline)` in
        // AIChatContainerView.swift.
        _titleLabel.Font = ThemeFonts.NS(ThemeFont.RowTitle);
        _titleLabel.TextColor = ThemeColors.NS(ThemeColor.Foreground);
        _titleLabel.Alignment = NSTextAlignment.Center;
        _titleLabel.LineBreakMode = NSLineBreakMode.TruncatingTail;
        _titleLabel.UsesSingleLineMode = true;
        _titleLabel.TranslatesAutoresizingMaskIntoConstraints = false;

        // New-conversation button — Phase D wires to "reset to
        // NewChat view." For now it just clears the transcript
        // (which is a reasonable Phase B placeholder; the VM
        // doesn't yet have a "reset conversation" method, so we
        // can't formally start a new one without losing the in-
        // memory turn context).
        _newConversationButton = new IconButton(
            systemSymbolName: "plus.bubble",
            accessibilityLabel: "New conversation",
            toolTip: "New conversation");
        _newConversationButton.Activated += (_, _) =>
        {
            // Phase D wires the proper reset; this is a placeholder.
        };

        // Detach button — Phase D wires to "open chat in a floating
        // NSPanel." Placeholder for layout parity.
        _detachButton = new IconButton(
            systemSymbolName: "arrow.up.right.square",
            accessibilityLabel: "Open in floating window",
            toolTip: "Open in window");
        _detachButton.Activated += (_, _) =>
        {
            // Phase D wires the float-detach.
        };

        _headerDivider = new NSBox
        {
            BoxType = NSBoxType.NSBoxSeparator,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
    }

    private void BuildTranscript()
    {
        // FlippedStackView overrides IsFlipped to true so subviews
        // stack from the TOP DOWN (first added at top, latest at
        // bottom) — the natural reading order for a transcript.
        //
        // Distribution = GravityAreas lets us anchor rows to the
        // BOTTOM of the stack via `AddView(view, NSStackViewGravity.Bottom)`.
        // When the transcript is sparse (fewer messages than fill
        // the stack), the center gravity area absorbs the slack and
        // pushes messages down against the composer — matching
        // SwiftUI's chat shape where empty space sits ABOVE the
        // messages, not below. Once the messages overflow the
        // stack height, scrolling kicks in normally.
        _transcriptStack = new FlippedStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Vertical,
            Distribution = NSStackViewDistribution.GravityAreas,
            Alignment = NSLayoutAttribute.Leading,
            Spacing = ThemeMetrics.SpaceSm,
            TranslatesAutoresizingMaskIntoConstraints = false,
            EdgeInsets = new NSEdgeInsets(
                ThemeMetrics.SpaceMd, ThemeMetrics.SpaceMd,
                ThemeMetrics.SpaceMd, ThemeMetrics.SpaceMd),
        };

        _transcriptScroll = new NSScrollView
        {
            HasVerticalScroller = true,
            HasHorizontalScroller = false,
            AutohidesScrollers = true,
            TranslatesAutoresizingMaskIntoConstraints = false,
            DrawsBackground = false,
            DocumentView = _transcriptStack,
        };

        var content = _transcriptScroll.ContentView;
        content.TranslatesAutoresizingMaskIntoConstraints = false;
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            _transcriptStack.LeadingAnchor.ConstraintEqualTo(content.LeadingAnchor),
            _transcriptStack.TrailingAnchor.ConstraintEqualTo(content.TrailingAnchor),
            _transcriptStack.TopAnchor.ConstraintEqualTo(content.TopAnchor),
            _transcriptStack.WidthAnchor.ConstraintEqualTo(content.WidthAnchor),
            // Stack must be AT LEAST as tall as the clip view so the
            // GravityAreas distribution has slack to absorb above
            // the Bottom-gravity rows. Without this, the stack
            // hugs its content (sum of message heights), the center
            // gravity area collapses to 0, and the bottom-anchoring
            // visually disappears.
            _transcriptStack.HeightAnchor.ConstraintGreaterThanOrEqualTo(content.HeightAnchor),
        });
    }

    /// <summary>Empty-state view: centered chat-bubble glyph + heading.
    /// Mirrors SwiftUI's NewChatView shape — Image(systemName:
    /// "bubble.left.and.text.bubble.right") + "Start a conversation"
    /// in title3.</summary>
    private void BuildEmptyState()
    {
        var icon = new NSImageView
        {
            Image = NSImage.GetSystemSymbol(
                "bubble.left.and.text.bubble.right",
                "Empty conversation"),
            TranslatesAutoresizingMaskIntoConstraints = false,
            // Tertiary tint reads as ambient hint, not active UI.
            // Matches the SwiftUI ref's `.foregroundStyle(.tertiary)`
            // on the empty-state glyph.
            ContentTintColor = ThemeColors.NS(ThemeColor.TertiaryForeground),
        };
        icon.SymbolConfiguration = NSImageSymbolConfiguration.Create(
            36, (double)NSFontWeight.Regular, NSImageSymbolScale.Large);

        var heading = NSTextField.CreateLabel("Start a conversation");
        // SectionHeading = title3.semibold — matches SwiftUI's
        // `Text("Start a conversation").font(.title3)` on NewChatView.
        heading.Font = ThemeFonts.NS(ThemeFont.SectionHeading);
        heading.TextColor = ThemeColors.NS(ThemeColor.SecondaryForeground);
        heading.Alignment = NSTextAlignment.Center;
        heading.TranslatesAutoresizingMaskIntoConstraints = false;

        _emptyStateView = new NSView
        {
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        _emptyStateView.AddSubview(icon);
        _emptyStateView.AddSubview(heading);

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            icon.CenterXAnchor.ConstraintEqualTo(_emptyStateView.CenterXAnchor),
            icon.TopAnchor.ConstraintEqualTo(_emptyStateView.TopAnchor),
            heading.CenterXAnchor.ConstraintEqualTo(_emptyStateView.CenterXAnchor),
            heading.TopAnchor.ConstraintEqualTo(icon.BottomAnchor, ThemeMetrics.SpaceSm),
            heading.BottomAnchor.ConstraintEqualTo(_emptyStateView.BottomAnchor),
            heading.LeadingAnchor.ConstraintGreaterThanOrEqualTo(_emptyStateView.LeadingAnchor),
            heading.TrailingAnchor.ConstraintLessThanOrEqualTo(_emptyStateView.TrailingAnchor),
        });
    }

    private void BuildStatus()
    {
        _statusLabel = NSTextField.CreateLabel(" ");
        _statusLabel.Font = ThemeFonts.NS(ThemeFont.BodySmall);
        _statusLabel.TextColor = ThemeColors.NS(ThemeColor.SecondaryForeground);
        _statusLabel.LineBreakMode = NSLineBreakMode.ByWordWrapping;
        _statusLabel.UsesSingleLineMode = false;
        _statusLabel.TranslatesAutoresizingMaskIntoConstraints = false;
    }

    private void BuildComposer()
    {
        _composer = new ChatComposerView();
        _composer.OnSubmit += async text => await SendAsync(text);
        _composer.OnCancel += () => _vm.Cancel();
        _composer.OnAttachmentRequested += () =>
        {
            // Phase D: attachment menu (file picker, slash commands).
        };
    }

    private void ArrangeLayout()
    {
        View.AddSubview(_historyButton);
        View.AddSubview(_titleLabel);
        View.AddSubview(_newConversationButton);
        View.AddSubview(_detachButton);
        View.AddSubview(_headerDivider);
        View.AddSubview(_transcriptScroll);
        View.AddSubview(_emptyStateView);
        View.AddSubview(_statusLabel);
        View.AddSubview(_composer);

        const float pad = ThemeMetrics.SpaceMd;
        const float headerPadH = 12;
        const float headerPadV = 8;

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            // ───── header ─────
            _historyButton.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor, headerPadH - 6),
            _historyButton.TopAnchor.ConstraintEqualTo(View.TopAnchor, headerPadV - 6),

            _detachButton.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor, -(headerPadH - 6)),
            _detachButton.CenterYAnchor.ConstraintEqualTo(_historyButton.CenterYAnchor),

            _newConversationButton.TrailingAnchor.ConstraintEqualTo(_detachButton.LeadingAnchor),
            _newConversationButton.CenterYAnchor.ConstraintEqualTo(_historyButton.CenterYAnchor),

            _titleLabel.CenterXAnchor.ConstraintEqualTo(View.CenterXAnchor),
            _titleLabel.CenterYAnchor.ConstraintEqualTo(_historyButton.CenterYAnchor),
            _titleLabel.LeadingAnchor.ConstraintGreaterThanOrEqualTo(
                _historyButton.TrailingAnchor, ThemeMetrics.SpaceSm),
            _titleLabel.TrailingAnchor.ConstraintLessThanOrEqualTo(
                _newConversationButton.LeadingAnchor, -ThemeMetrics.SpaceSm),

            _headerDivider.TopAnchor.ConstraintEqualTo(_historyButton.BottomAnchor, headerPadV - 6),
            _headerDivider.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor),
            _headerDivider.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor),
            _headerDivider.HeightAnchor.ConstraintEqualTo(ThemeMetrics.HairlineThickness),

            // ───── transcript ─────
            _transcriptScroll.TopAnchor.ConstraintEqualTo(_headerDivider.BottomAnchor),
            _transcriptScroll.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor),
            _transcriptScroll.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor),
            _transcriptScroll.BottomAnchor.ConstraintEqualTo(_statusLabel.TopAnchor, -ThemeMetrics.SpaceSm),

            // Empty-state vertically centered in the transcript area.
            _emptyStateView.CenterXAnchor.ConstraintEqualTo(_transcriptScroll.CenterXAnchor),
            _emptyStateView.CenterYAnchor.ConstraintEqualTo(_transcriptScroll.CenterYAnchor),
            _emptyStateView.LeadingAnchor.ConstraintGreaterThanOrEqualTo(
                _transcriptScroll.LeadingAnchor, pad),
            _emptyStateView.TrailingAnchor.ConstraintLessThanOrEqualTo(
                _transcriptScroll.TrailingAnchor, -pad),

            // ───── status ─────
            _statusLabel.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor, pad),
            _statusLabel.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor, -pad),
            _statusLabel.BottomAnchor.ConstraintEqualTo(_composer.TopAnchor, -ThemeMetrics.SpaceXs),

            // ───── composer ─────
            _composer.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor, pad),
            _composer.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor, -pad),
            _composer.BottomAnchor.ConstraintEqualTo(View.BottomAnchor, -pad),
        });
    }

    // ───── VM ↔ view binding ───────────────────────────────────

    private void SubscribeToVm()
    {
        ((INotifyCollectionChanged)_vm.Messages).CollectionChanged += OnMessagesChanged;
        _vm.PropertyChanged += OnVmPropertyChanged;
    }

    private void OnMessagesChanged(object? sender, NotifyCollectionChangedEventArgs e)
    {
        // Rule 12: VM events fire on the UI thread; no marshaling.
        switch (e.Action)
        {
            case NotifyCollectionChangedAction.Add:
                if (e.NewItems is null) break;
                foreach (Message m in e.NewItems)
                {
                    AppendRow(m);
                }
                ScrollTranscriptToBottom();
                break;

            case NotifyCollectionChangedAction.Reset:
                ClearTranscript();
                break;

            case NotifyCollectionChangedAction.Remove:
                // DiscardEmptyInflight removes the last row. Detach
                // managed Message.PropertyChanged BEFORE removing
                // from the view hierarchy (Rule 13 — never
                // Dispose() the NSView peer).
                if (e.OldStartingIndex >= 0
                    && e.OldStartingIndex < _transcriptStack.ArrangedSubviews.Length)
                {
                    var view = _transcriptStack.ArrangedSubviews[e.OldStartingIndex];
                    (view as MessageRowView)?.Unsubscribe();
                    _transcriptStack.RemoveArrangedSubview(view);
                    view.RemoveFromSuperview();
                }
                break;
        }
        UpdateEmptyStateVisibility();
    }

    private void OnVmPropertyChanged(object? sender, PropertyChangedEventArgs e)
    {
        switch (e.PropertyName)
        {
            case nameof(ConversationViewModel.CanSend):
            case nameof(ConversationViewModel.State):
                UpdateComposerState();
                UpdateStatusLine();
                break;
            case nameof(ConversationViewModel.LastErrorMessage):
                UpdateStatusLine();
                break;
        }
    }

    private void AppendRow(Message m)
    {
        var row = new MessageRowView(m);
        // GravityAreas distribution + Bottom gravity: messages anchor
        // to the bottom of the stack so a sparse transcript hugs the
        // composer (matching SwiftUI's chat shape). AddView is the
        // gravity-aware insertion API; AddArrangedSubview ignores
        // gravity and falls back to Top.
        _transcriptStack.AddView(row, NSStackViewGravity.Bottom);
        // Rule 15 (dotnet/CLAUDE.md): pin row width to stack content
        // width via single WidthAnchor constraint. Pair of
        // leading+trailing conflicts with NSStackView.Alignment.
        var horizontalInset = -2f * (float)_transcriptStack.EdgeInsets.Left;
        row.WidthAnchor
            .ConstraintEqualTo(_transcriptStack.WidthAnchor, 1, horizontalInset)
            .Active = true;
    }

    private void ClearTranscript()
    {
        foreach (var view in _transcriptStack.ArrangedSubviews.ToArray())
        {
            (view as MessageRowView)?.Unsubscribe();
            _transcriptStack.RemoveArrangedSubview(view);
            view.RemoveFromSuperview();
        }
    }

    private void ScrollTranscriptToBottom()
    {
        BeginInvokeOnMainThread(ScrollToBottomImpl);
    }

    private void ScrollToBottomImpl()
    {
        if (_transcriptScroll.DocumentView is not NSView doc) return;
        var clip = _transcriptScroll.ContentView;
        var docHeight = (float)doc.Bounds.Height;
        var clipHeight = (float)clip.Bounds.Height;
        // FlippedStackView's IsFlipped = true puts the bottom of the
        // document at y = docHeight - clipHeight in clip-bounds
        // coordinates. Negative target means the doc is shorter than
        // the clip; clamp to 0 to avoid over-scrolling.
        var targetY = (float)Math.Max(0, docHeight - clipHeight);
        clip.ScrollToPoint(new CGPoint(0, targetY));
        _transcriptScroll.ReflectScrolledClipView(clip);
    }

    /// <summary>NSStackView in a vertical orientation inside an
    /// <see cref="NSScrollView"/> needs <see cref="NSView.IsFlipped"/>
    /// = true for "top down" stacking to match user expectation. The
    /// AppKit default (non-flipped) stacks subviews from the
    /// bottom up, which inverts the apparent order and breaks
    /// scroll-to-bottom math.</summary>
    private sealed class FlippedStackView : NSStackView
    {
        public override bool IsFlipped => true;
    }

    private void UpdateEmptyStateVisibility()
    {
        _emptyStateView.Hidden = _vm.Messages.Count > 0;
    }

    private void UpdateComposerState()
    {
        _composer.IsStreaming = _vm.State is ConversationState.Loading
            or ConversationState.Streaming;
        _composer.IsEnabled = _vm.CanSend
            || _vm.State is ConversationState.Loading
                or ConversationState.Streaming;
    }

    private void UpdateStatusLine()
    {
        _statusLabel.StringValue = _vm.State switch
        {
            ConversationState.Loading => "Sending…",
            ConversationState.Streaming => "Pivox is responding…",
            ConversationState.Error => _vm.LastErrorMessage ?? "Something went wrong.",
            _ => " ",
        };
        _statusLabel.TextColor = _vm.State == ConversationState.Error
            ? ThemeColors.NS(ThemeColor.Destructive)
            : ThemeColors.NS(ThemeColor.SecondaryForeground);
    }

    private async Task SendAsync(string text)
    {
        if (string.IsNullOrWhiteSpace(text)) return;
        if (!_vm.CanSend) return;

        // Clear composer immediately so the user can start typing
        // the next turn while the response streams. CanSend flips
        // false during Loading/Streaming, so the field disables
        // until the response completes.
        _composer.Text = "";

        try
        {
            await _vm.SendAsync(text);
        }
        catch (Exception ex)
        {
            // Defense-in-depth: the VM surfaces errors via State,
            // but a leak past it still shows up in the status line.
            _statusLabel.StringValue = $"Send failed: {ex.Message}";
            _statusLabel.TextColor = ThemeColors.NS(ThemeColor.Destructive);
        }
    }

    /// <summary>Move keyboard focus to the composer. Called from
    /// the ⇧⌘A path when the panel opens — matches the SwiftUI
    /// .aiChatFocusRequested behavior.</summary>
    public void FocusComposer() => _composer.FocusTextInput();
}
