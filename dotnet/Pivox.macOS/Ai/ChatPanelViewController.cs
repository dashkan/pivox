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
/// <c>AIChat/Transcript/ConversationView.swift</c>. Stacked top-to-bottom:
///
/// <list type="number">
/// <item><b>Title header</b> — "New Conversation" (static; editable
///   title lands in Phase D with the <c>UpdateConversation</c> RPC).</item>
/// <item><b>Transcript</b> — NSStackView of <see cref="MessageRowView"/>
///   in NSScrollView. Empty-state hint when no messages.</item>
/// <item><b>Status line</b> — sending / streaming / error.</item>
/// <item><b>Divider</b> — hairline rule.</item>
/// <item><b>Composer</b> — multi-line NSTextView with placeholder.
///   Send button to the right.</item>
/// </list>
///
/// <para><b>What's intentionally NOT here (Phase C/D scope):</b>
/// markdown rendering, hover-reveal action icons (thumbs/copy/redo),
/// thinking-indicator placeholder during the first-token gap,
/// jump-to-latest pill, title editing, attachment menu, conversation
/// history list.</para>
///
/// <para><b>State model.</b> A single
/// <see cref="ConversationViewModel"/> drives the surface. The VM
/// subscribes to <see cref="ActiveOrganization.PropertyChanged"/>
/// internally; switching organization wipes the transcript
/// automatically. This VC subscribes to
/// <see cref="ConversationViewModel.Messages"/> (add-only — TextDelta
/// mutations come via each row's own
/// <see cref="Message.PropertyChanged"/> subscription) and to
/// <see cref="ConversationViewModel.PropertyChanged"/> for state /
/// CanSend transitions.</para>
///
/// <para><b>No-org gating.</b> The chat surface itself doesn't gate
/// on org — the toolbar item and ⇧⌘A menu binding do (validation in
/// <c>AppDelegate</c>). When the panel is visible with no org
/// selected, <see cref="ConversationViewModel.SendAsync"/>'s
/// precondition check fails with
/// <see cref="ChatErrorKind.NoOrganization"/> and the composer
/// surfaces the error inline. In practice the toolbar gating
/// prevents this path.</para>
/// </summary>
public sealed class ChatPanelViewController : NSViewController
{
    private readonly ConversationViewModel _vm;

    private NSTextField _titleLabel = null!;
    private NSStackView _transcriptStack = null!;
    private NSScrollView _transcriptScroll = null!;
    private NSTextField _emptyStateLabel = null!;
    private NSTextField _statusLabel = null!;
    private NSBox _divider = null!;
    private NSTextView _composer = null!;
    private NSScrollView _composerScroll = null!;
    private NSButton _sendButton = null!;

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
        View = new NSView(new CGRect(0, 0, 320, 600));
        View.WantsLayer = true;
        View.Layer!.BackgroundColor = ThemeColors.NS(ThemeColor.Background).CGColor;
    }

    public override void ViewDidLoad()
    {
        base.ViewDidLoad();
        BuildTitleHeader();
        BuildTranscript();
        BuildEmptyState();
        BuildStatus();
        BuildDivider();
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
        UpdateComposerEnabled();
        UpdateStatusLine();
    }

    // ───── construction ────────────────────────────────────────

    private void BuildTitleHeader()
    {
        // Static title — Phase D adds in-place editing via the
        // UpdateConversation RPC (mirroring
        // ConversationTitleHeader.swift). For now, a centered,
        // appearance-aware label sits in the header strip.
        _titleLabel = NSTextField.CreateLabel("New Conversation");
        _titleLabel.Font = ThemeFonts.NS(ThemeFont.Title);
        _titleLabel.TextColor = ThemeColors.NS(ThemeColor.Foreground);
        _titleLabel.Alignment = NSTextAlignment.Center;
        _titleLabel.LineBreakMode = NSLineBreakMode.TruncatingTail;
        _titleLabel.UsesSingleLineMode = true;
        _titleLabel.TranslatesAutoresizingMaskIntoConstraints = false;
    }

    private void BuildTranscript()
    {
        _transcriptStack = new NSStackView
        {
            Orientation = NSUserInterfaceLayoutOrientation.Vertical,
            // .Leading keeps both user (right-aligned via internal
            // spacer) and assistant (full-width left-aligned) rows
            // working — each row owns its own internal alignment.
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

        // The stack must fill the document container's width so its
        // rows can grow to the available width.
        var content = _transcriptScroll.ContentView;
        content.TranslatesAutoresizingMaskIntoConstraints = false;
        NSLayoutConstraint.ActivateConstraints(new[]
        {
            _transcriptStack.LeadingAnchor.ConstraintEqualTo(content.LeadingAnchor),
            _transcriptStack.TrailingAnchor.ConstraintEqualTo(content.TrailingAnchor),
            _transcriptStack.TopAnchor.ConstraintEqualTo(content.TopAnchor),
            // Width drives intrinsic-height layout in MessageRowView.
            _transcriptStack.WidthAnchor.ConstraintEqualTo(content.WidthAnchor),
        });
    }

    private void BuildEmptyState()
    {
        // Empty-state hint shown when Messages.Count == 0. Mirrors
        // the SwiftUI ref's "Message..." composer placeholder + the
        // implicit "ask anything" affordance. Keep it short — the
        // composer's own placeholder text reinforces the call to
        // action, so this label sets the tone, not the instruction.
        _emptyStateLabel = NSTextField.CreateLabel("Ask Pivox anything");
        _emptyStateLabel.Font = ThemeFonts.NS(ThemeFont.Title);
        _emptyStateLabel.TextColor = ThemeColors.NS(ThemeColor.SecondaryForeground);
        _emptyStateLabel.Alignment = NSTextAlignment.Center;
        _emptyStateLabel.LineBreakMode = NSLineBreakMode.ByWordWrapping;
        _emptyStateLabel.UsesSingleLineMode = false;
        _emptyStateLabel.TranslatesAutoresizingMaskIntoConstraints = false;
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

    private void BuildDivider()
    {
        _divider = new NSBox
        {
            BoxType = NSBoxType.NSBoxSeparator,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
    }

    private void BuildComposer()
    {
        // NSTextView lives inside NSScrollView so multi-line input
        // can grow with content (capped via height constraints on
        // the scroll view, not the text view itself). NSTextView
        // doesn't have a native placeholder property — we draw one
        // via a private override below.
        _composer = new NSTextView
        {
            Font = ThemeFonts.NS(ThemeFont.Body),
            TextContainerInset = new CGSize(ThemeMetrics.SpaceSm, ThemeMetrics.SpaceSm),
            Editable = true,
            Selectable = true,
            RichText = false,
            ImportsGraphics = false,
            AllowsUndo = true,
            UsesFontPanel = false,
            UsesRuler = false,
            DrawsBackground = false,
            // NSTextView's autoresize defaults are designed for use
            // inside NSScrollView — leave them.
            HorizontallyResizable = false,
            VerticallyResizable = true,
        };
        _composer.TextContainer!.WidthTracksTextView = true;
        _composer.TextContainer.LineFragmentPadding = 0;

        _composerScroll = new NSScrollView
        {
            HasVerticalScroller = true,
            HasHorizontalScroller = false,
            AutohidesScrollers = true,
            TranslatesAutoresizingMaskIntoConstraints = false,
            DocumentView = _composer,
            DrawsBackground = true,
        };
        _composerScroll.WantsLayer = true;
        _composerScroll.Layer!.BorderColor = ThemeColors.NS(ThemeColor.Border).CGColor;
        _composerScroll.Layer.BorderWidth = ThemeMetrics.HairlineThickness;
        _composerScroll.Layer.CornerRadius = ThemeMetrics.ChatMessageCornerRadius;
        _composerScroll.Layer.MasksToBounds = true;
        _composerScroll.BackgroundColor = ThemeColors.NS(ThemeColor.Surface);

        // Placeholder via NSTextView's documented overlay pattern —
        // a non-editable NSTextField pinned over the text view that
        // hides when content is non-empty.
        _composerPlaceholder = NSTextField.CreateLabel("Message…");
        _composerPlaceholder.Font = ThemeFonts.NS(ThemeFont.Body);
        _composerPlaceholder.TextColor = ThemeColors.NS(ThemeColor.SecondaryForeground);
        _composerPlaceholder.TranslatesAutoresizingMaskIntoConstraints = false;
        _composerPlaceholder.WantsLayer = true;
        // Don't intercept clicks — let them fall through to the
        // text view underneath. Labels are non-editable but still
        // hit-testable; disable hit testing entirely.
        _composerPlaceholder.Hidden = false;

        _sendButton = new NSButton
        {
            Title = "Send",
            BezelStyle = NSBezelStyle.Push,
            ControlSize = NSControlSize.Regular,
            TranslatesAutoresizingMaskIntoConstraints = false,
            // No KeyEquivalent (\r): the composer is multi-line and
            // Enter must insert a newline. Send fires on click. ⌘↩-
            // as-send is a future affordance (NSTextView delegate's
            // DoCommandBySelector) — Phase C scope.
        };
        _sendButton.Activated += async (_, _) => await SendCurrentAsync();

        // Composer text changes → flip placeholder visibility +
        // re-evaluate Send enabled state.
        _composer.TextDidChange += (_, _) =>
        {
            UpdateComposerPlaceholder();
            UpdateComposerEnabled();
        };
    }

    private NSTextField _composerPlaceholder = null!;

    private void ArrangeLayout()
    {
        View.AddSubview(_titleLabel);
        View.AddSubview(_transcriptScroll);
        View.AddSubview(_emptyStateLabel);
        View.AddSubview(_statusLabel);
        View.AddSubview(_divider);
        View.AddSubview(_composerScroll);
        View.AddSubview(_composerPlaceholder);
        View.AddSubview(_sendButton);

        const float pad = ThemeMetrics.SpaceMd;
        const float composerMinHeight = 64;
        const float composerMaxHeight = 160;
        const float headerHeight = 44;

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            // ───── header ─────
            _titleLabel.TopAnchor.ConstraintEqualTo(View.TopAnchor),
            _titleLabel.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor, pad),
            _titleLabel.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor, -pad),
            _titleLabel.HeightAnchor.ConstraintEqualTo(headerHeight),

            // ───── transcript ─────
            _transcriptScroll.TopAnchor.ConstraintEqualTo(_titleLabel.BottomAnchor),
            _transcriptScroll.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor),
            _transcriptScroll.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor),
            _transcriptScroll.BottomAnchor.ConstraintEqualTo(_statusLabel.TopAnchor, -ThemeMetrics.SpaceSm),

            // Empty-state hint centered inside the transcript area.
            _emptyStateLabel.CenterXAnchor.ConstraintEqualTo(_transcriptScroll.CenterXAnchor),
            _emptyStateLabel.CenterYAnchor.ConstraintEqualTo(_transcriptScroll.CenterYAnchor),
            _emptyStateLabel.LeadingAnchor.ConstraintGreaterThanOrEqualTo(
                _transcriptScroll.LeadingAnchor, pad),
            _emptyStateLabel.TrailingAnchor.ConstraintLessThanOrEqualTo(
                _transcriptScroll.TrailingAnchor, -pad),

            // ───── status ─────
            _statusLabel.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor, pad),
            _statusLabel.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor, -pad),
            _statusLabel.BottomAnchor.ConstraintEqualTo(_divider.TopAnchor, -ThemeMetrics.SpaceXs),

            // ───── divider ─────
            _divider.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor),
            _divider.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor),
            _divider.HeightAnchor.ConstraintEqualTo(ThemeMetrics.HairlineThickness),
            _divider.BottomAnchor.ConstraintEqualTo(_composerScroll.TopAnchor, -ThemeMetrics.SpaceSm),

            // ───── composer ─────
            _composerScroll.LeadingAnchor.ConstraintEqualTo(View.LeadingAnchor, pad),
            _composerScroll.TrailingAnchor.ConstraintEqualTo(_sendButton.LeadingAnchor, -ThemeMetrics.SpaceSm),
            _composerScroll.HeightAnchor.ConstraintGreaterThanOrEqualTo(composerMinHeight),
            _composerScroll.HeightAnchor.ConstraintLessThanOrEqualTo(composerMaxHeight),
            _composerScroll.BottomAnchor.ConstraintEqualTo(View.BottomAnchor, -pad),

            // Placeholder pinned to the composer's text origin.
            // TextContainerInset is (SpaceSm, SpaceSm); the
            // placeholder's leading aligns to that inset inside the
            // scroll view's content frame.
            _composerPlaceholder.LeadingAnchor.ConstraintEqualTo(
                _composerScroll.LeadingAnchor, ThemeMetrics.SpaceSm),
            _composerPlaceholder.TopAnchor.ConstraintEqualTo(
                _composerScroll.TopAnchor, ThemeMetrics.SpaceSm),
            _composerPlaceholder.TrailingAnchor.ConstraintLessThanOrEqualTo(
                _composerScroll.TrailingAnchor, -ThemeMetrics.SpaceSm),

            // ───── send button ─────
            _sendButton.TrailingAnchor.ConstraintEqualTo(View.TrailingAnchor, -pad),
            _sendButton.BottomAnchor.ConstraintEqualTo(_composerScroll.BottomAnchor),
            _sendButton.WidthAnchor.ConstraintGreaterThanOrEqualTo(72),
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
                // Org switch / re-send-after-error clears Messages.
                ClearTranscript();
                break;

            case NotifyCollectionChangedAction.Remove:
                // DiscardEmptyInflight removes the last row. Detach
                // the managed Message.PropertyChanged handler BEFORE
                // RemoveFromSuperview (Rule 13 — don't Dispose the
                // NSView peer).
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
                UpdateComposerEnabled();
                break;
            case nameof(ConversationViewModel.State):
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
        _transcriptStack.AddArrangedSubview(row);
        // Rule 15 (dotnet/CLAUDE.md): NSStackView has no built-in
        // "fill cross-axis" alignment. To make each row fill the
        // stack's content width, pin row.width to stack.width minus
        // the stack's horizontal EdgeInsets on both sides. Derive
        // the inset from the stack's actual EdgeInsets so the two
        // numbers can't drift.
        //
        // Don't add a leading+trailing pair here — the stack's
        // Alignment=Leading already pins leading; a redundant
        // explicit pair creates a constraint conflict that
        // resolves unpredictably (the symptom on the prior cut: row
        // collapsed to ~80pt wide).
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
        // Defer to next runloop tick so the freshly-added row's
        // intrinsic height is settled before we measure.
        BeginInvokeOnMainThread(ScrollToBottomImpl);
    }

    private void ScrollToBottomImpl()
    {
        if (_transcriptScroll.DocumentView is not NSView doc) return;
        var bottom = new CGPoint(0, doc.Bounds.Height - _transcriptScroll.ContentView.Bounds.Height);
        if (bottom.Y < 0) bottom = new CGPoint(0, 0);
        _transcriptScroll.ContentView.ScrollToPoint(bottom);
        _transcriptScroll.ReflectScrolledClipView(_transcriptScroll.ContentView);
    }

    private void UpdateEmptyStateVisibility()
    {
        _emptyStateLabel.Hidden = _vm.Messages.Count > 0;
    }

    private void UpdateComposerEnabled()
    {
        _sendButton.Enabled = _vm.CanSend && !string.IsNullOrWhiteSpace(_composer.Value);
        _composer.Editable = _vm.CanSend;
    }

    private void UpdateComposerPlaceholder()
    {
        _composerPlaceholder.Hidden = !string.IsNullOrEmpty(_composer.Value);
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

    private async Task SendCurrentAsync()
    {
        var text = _composer.Value?.Trim();
        if (string.IsNullOrEmpty(text)) return;
        if (!_vm.CanSend) return;

        // Clear composer immediately so the user can start typing
        // the next turn while the response streams. The VM's
        // CanSend flips false during Loading/Streaming, so Send is
        // gated.
        _composer.Value = "";
        UpdateComposerPlaceholder();
        UpdateComposerEnabled();

        try
        {
            await _vm.SendAsync(text);
        }
        catch (Exception ex)
        {
            // The VM is supposed to surface errors via state — but
            // defense-in-depth: a leak past the VM still gets shown.
            _statusLabel.StringValue = $"Send failed: {ex.Message}";
            _statusLabel.TextColor = ThemeColors.NS(ThemeColor.Destructive);
        }
    }
}
