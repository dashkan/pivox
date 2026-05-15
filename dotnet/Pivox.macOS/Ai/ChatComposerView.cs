using AppKit;
using CoreGraphics;
using Foundation;
using ObjCRuntime;
using Pivox.Shared.UI;
using Pivox.UI;

namespace Pivox.Ai;

/// <summary>
/// Multi-line chat composer — the AppKit translation of SwiftUI's
/// <c>ShimmerPromptField</c>
/// (<c>native/.../Core/Foundation/Inputs/ShimmerPromptField.swift</c>).
///
/// <para><b>Visual</b>: rounded rect with thin-material fill,
/// animated iridescent border (<see cref="AIShimmerLayer"/>) that
/// fades from ambient to prominent on focus. Internally stacked:
/// <list type="bullet">
/// <item>Auto-growing <see cref="NSTextView"/> (1–10 lines) with
///   placeholder text.</item>
/// <item>Hairline divider (an <see cref="NSBox"/> separator).</item>
/// <item>Tool row: leading <c>+</c> button (attachment slot) →
///   spacer → caption hint "↩ Send · ⌥↩ New line" → primary
///   send/stop button (<see cref="IconButton"/>).</item>
/// </list>
/// </para>
///
/// <para><b>Submit semantics</b>: Return submits, Option+Return /
/// Shift+Return inserts a newline. The composer intercepts these
/// via an <see cref="NSTextViewDelegate"/> override of
/// <c>DoCommandBySelector</c>. IME-safety: when an input method
/// context has marked (uncommitted) text, Return commits the
/// candidate via the IME instead of firing submit.</para>
///
/// <para><b>Send/stop swap</b>: <see cref="IsStreaming"/> swaps the
/// primary button between an accent up-arrow ("send") and a
/// destructive stop glyph. Send is gated on
/// <see cref="CanSubmit"/> (non-whitespace text AND
/// <see cref="IsEnabled"/>).</para>
///
/// <para><b>What's NOT here</b>: attachment-menu logic behind the
/// <c>+</c> button (Phase D — file attachments are out of scope
/// for chat MVP). The <c>+</c> currently fires
/// <see cref="OnAttachmentRequested"/>; the consumer can no-op it
/// for now.</para>
/// </summary>
public sealed class ChatComposerView : NSView
{
    /// <summary>Rounded rect corner radius. Matches
    /// <c>ShimmerPromptField.cornerRadius = 14</c> in the SwiftUI
    /// reference.</summary>
    private const float CornerRadius = 14;
    private const float ContentHorizontalPadding = 12;
    private const float ContentVerticalPadding = 10;
    private const float ToolRowHeight = 32;
    private const float DividerSpacing = 8;
    private const float MinTextAreaHeight = 32;
    private const float MaxTextAreaHeight = 200;  // ~10 lines of body text

    private readonly NSVisualEffectView _backdrop;
    private readonly AIShimmerLayer _shimmer;
    private readonly ComposerTextView _textView;
    private readonly NSScrollView _textScroll;
    private readonly NSTextField _placeholder;
    private readonly NSBox _divider;
    private readonly IconButton _attachmentButton;
    private readonly NSTextField _hintLabel;
    private readonly IconButton _primaryButton;
    private readonly NSLayoutConstraint _textScrollHeight;

    private bool _isEnabled = true;
    private bool _isStreaming;
    private bool _focused;

    /// <summary>The user's current input text. Mutable; consumers
    /// can read or clear it (e.g., after a successful submit).</summary>
    public string Text
    {
        get => _textView.Value ?? "";
        set
        {
            _textView.Value = value ?? "";
            HandleTextChanged();
        }
    }

    public bool IsEnabled
    {
        get => _isEnabled;
        set
        {
            if (_isEnabled == value) return;
            _isEnabled = value;
            _textView.Editable = value;
            _primaryButton.Enabled = value && (IsStreaming || HasNonWhitespaceText);
            _attachmentButton.Enabled = value;
        }
    }

    /// <summary>True while the response is streaming. Swaps the
    /// primary button between send (up-arrow accent) and stop
    /// (red destructive glyph). The consumer drives this from the
    /// VM's State.</summary>
    public bool IsStreaming
    {
        get => _isStreaming;
        set
        {
            if (_isStreaming == value) return;
            _isStreaming = value;
            UpdatePrimaryButton();
        }
    }

    /// <summary>Fired when the user submits via Return or by
    /// clicking the send button. Receives the trimmed text.</summary>
    public event Action<string>? OnSubmit;

    /// <summary>Fired when the user clicks the stop button (or
    /// presses Escape) while a stream is in flight.</summary>
    public event Action? OnCancel;

    /// <summary>Fired when the user clicks the <c>+</c> button.
    /// Phase D wires this to an attachment picker; Phase B leaves
    /// it as a no-op.</summary>
    public event Action? OnAttachmentRequested;

    public ChatComposerView()
    {
        TranslatesAutoresizingMaskIntoConstraints = false;
        WantsLayer = true;
        Layer!.CornerRadius = CornerRadius;
        Layer.MasksToBounds = true;

        // Thin-material backdrop matches SwiftUI's
        // `.background(RoundedRectangle().fill(.thinMaterial))`.
        // NSVisualEffectView is the AppKit equivalent; the
        // NSGlassEffectView Liquid-Glass primitive is reserved for
        // top-level cards per Rule 16, not inline controls.
        _backdrop = new NSVisualEffectView
        {
            Material = NSVisualEffectMaterial.Sidebar,
            BlendingMode = NSVisualEffectBlendingMode.WithinWindow,
            State = NSVisualEffectState.Active,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        AddSubview(_backdrop);

        // Text view + scroll wrapper. NSScrollView with vertical
        // resize lets the text area grow up to MaxTextAreaHeight,
        // then scroll internally instead of pushing the rest of the
        // chat UI off-screen.
        _textView = new ComposerTextView
        {
            Font = ThemeFonts.NS(ThemeFont.Body),
            TextContainerInset = new CGSize(0, 0),
            Editable = true,
            Selectable = true,
            RichText = false,
            ImportsGraphics = false,
            AllowsUndo = true,
            UsesFontPanel = false,
            UsesRuler = false,
            DrawsBackground = false,
            HorizontallyResizable = false,
            VerticallyResizable = true,
            TextColor = ThemeColors.NS(ThemeColor.Foreground),
        };
        _textView.TextContainer!.WidthTracksTextView = true;
        _textView.TextContainer.LineFragmentPadding = 0;
        _textView.OnTextChange = HandleTextChanged;
        _textView.OnFocusChange = HandleFocusChanged;
        _textView.OnSubmitKey = HandleSubmitKey;
        _textView.OnEscapeKey = HandleEscapeKey;

        _textScroll = new NSScrollView
        {
            HasVerticalScroller = true,
            HasHorizontalScroller = false,
            AutohidesScrollers = true,
            TranslatesAutoresizingMaskIntoConstraints = false,
            DocumentView = _textView,
            DrawsBackground = false,
        };
        AddSubview(_textScroll);

        _placeholder = NSTextField.CreateLabel("Message…");
        _placeholder.Font = ThemeFonts.NS(ThemeFont.Body);
        _placeholder.TextColor = ThemeColors.NS(ThemeColor.SecondaryForeground);
        _placeholder.TranslatesAutoresizingMaskIntoConstraints = false;
        AddSubview(_placeholder);

        _divider = new NSBox
        {
            BoxType = NSBoxType.NSBoxSeparator,
            TranslatesAutoresizingMaskIntoConstraints = false,
        };
        AddSubview(_divider);

        // Tool row: `+` (attachments), spacer, hint text, send button.
        _attachmentButton = new IconButton(
            systemSymbolName: "plus",
            accessibilityLabel: "Attach content",
            toolTip: "Add attachments");
        _attachmentButton.Activated += (_, _) => OnAttachmentRequested?.Invoke();
        AddSubview(_attachmentButton);

        _hintLabel = NSTextField.CreateLabel("↩ Send  ·  ⌥↩ New line");
        _hintLabel.Font = ThemeFonts.NS(ThemeFont.BodySmall);
        _hintLabel.TextColor = ThemeColors.NS(ThemeColor.SecondaryForeground);
        _hintLabel.Alignment = NSTextAlignment.Right;
        _hintLabel.TranslatesAutoresizingMaskIntoConstraints = false;
        _hintLabel.LineBreakMode = NSLineBreakMode.TruncatingTail;
        _hintLabel.UsesSingleLineMode = true;
        AddSubview(_hintLabel);

        _primaryButton = new IconButton(
            systemSymbolName: "arrow.up.circle.fill",
            accessibilityLabel: "Send message",
            toolTip: "Send",
            isOn: false,
            showsHoverBackground: false);
        _primaryButton.Activated += (_, _) =>
        {
            if (IsStreaming) OnCancel?.Invoke();
            else SubmitIfReady();
        };
        AddSubview(_primaryButton);

        // Initialize the text-scroll height to the minimum; updated
        // dynamically as the user types/pastes more lines.
        _textScrollHeight = _textScroll.HeightAnchor
            .ConstraintEqualTo(MinTextAreaHeight);

        ActivateConstraints();

        // Shimmer attaches to self. Initial intensity is ambient
        // (0.35) — barely visible at the rounded edge. Focus flips
        // to ~0.95 for the prominent rainbow.
        _shimmer = new AIShimmerLayer(CornerRadius, lineWidth: 1.5f);
        _shimmer.Attach(this);
        _shimmer.SetIntensity(0.0, animated: false);   // start dark; show only on focus

        UpdatePrimaryButton();
    }

    public override void Layout()
    {
        base.Layout();
        _shimmer.UpdateFrame(Bounds);
    }

    private void ActivateConstraints()
    {
        var inner = ContentHorizontalPadding;
        var innerV = ContentVerticalPadding;

        NSLayoutConstraint.ActivateConstraints(new[]
        {
            // Backdrop fills the composer.
            _backdrop.TopAnchor.ConstraintEqualTo(TopAnchor),
            _backdrop.LeadingAnchor.ConstraintEqualTo(LeadingAnchor),
            _backdrop.TrailingAnchor.ConstraintEqualTo(TrailingAnchor),
            _backdrop.BottomAnchor.ConstraintEqualTo(BottomAnchor),

            // Text scroll spans the top, with horizontal padding.
            _textScroll.TopAnchor.ConstraintEqualTo(TopAnchor, innerV),
            _textScroll.LeadingAnchor.ConstraintEqualTo(LeadingAnchor, inner),
            _textScroll.TrailingAnchor.ConstraintEqualTo(TrailingAnchor, -inner),
            _textScrollHeight,

            // Placeholder pinned to the text origin so it sits where
            // the first character would render. Hidden when text
            // is non-empty.
            _placeholder.TopAnchor.ConstraintEqualTo(_textScroll.TopAnchor),
            _placeholder.LeadingAnchor.ConstraintEqualTo(_textScroll.LeadingAnchor),
            _placeholder.TrailingAnchor.ConstraintLessThanOrEqualTo(_textScroll.TrailingAnchor),

            // Divider below the text area.
            _divider.TopAnchor.ConstraintEqualTo(_textScroll.BottomAnchor, DividerSpacing),
            _divider.LeadingAnchor.ConstraintEqualTo(LeadingAnchor, inner),
            _divider.TrailingAnchor.ConstraintEqualTo(TrailingAnchor, -inner),
            _divider.HeightAnchor.ConstraintEqualTo(ThemeMetrics.HairlineThickness),

            // Tool row below the divider.
            _attachmentButton.TopAnchor.ConstraintEqualTo(_divider.BottomAnchor, DividerSpacing),
            _attachmentButton.LeadingAnchor.ConstraintEqualTo(LeadingAnchor, inner - 6),
            _attachmentButton.BottomAnchor.ConstraintEqualTo(BottomAnchor, -(innerV - 6)),

            _primaryButton.CenterYAnchor.ConstraintEqualTo(_attachmentButton.CenterYAnchor),
            _primaryButton.TrailingAnchor.ConstraintEqualTo(TrailingAnchor, -(inner - 6)),

            _hintLabel.CenterYAnchor.ConstraintEqualTo(_attachmentButton.CenterYAnchor),
            _hintLabel.TrailingAnchor.ConstraintEqualTo(_primaryButton.LeadingAnchor, -4),
            _hintLabel.LeadingAnchor.ConstraintGreaterThanOrEqualTo(_attachmentButton.TrailingAnchor, 8),
        });
    }

    private bool HasNonWhitespaceText
        => !string.IsNullOrWhiteSpace(_textView.Value);

    private bool CanSubmit
        => IsEnabled && !IsStreaming && HasNonWhitespaceText;

    private void HandleTextChanged()
    {
        _placeholder.Hidden = !string.IsNullOrEmpty(_textView.Value);
        // Grow the text scroll height up to MaxTextAreaHeight.
        // Use the layoutManager's used rect to measure the content's
        // natural height; clamp into [Min, Max].
        var lm = _textView.LayoutManager;
        var tc = _textView.TextContainer;
        if (lm is not null && tc is not null)
        {
            lm.EnsureLayoutForTextContainer(tc);
            // Binding name is GetUsedRect (selector
            // `usedRectForTextContainer:`) on modern Microsoft.macOS.dll;
            // the legacy Xamarin.Mac binding had GetUsedRectForTextContainer.
            var used = lm.GetUsedRect(tc);
            var desired = (float)Math.Clamp(used.Height, MinTextAreaHeight, MaxTextAreaHeight);
            if (Math.Abs(_textScrollHeight.Constant - desired) > 0.5)
            {
                _textScrollHeight.Constant = desired;
            }
        }
        UpdatePrimaryButton();
    }

    private void HandleFocusChanged(bool focused)
    {
        _focused = focused;
        // Ambient when unfocused (essentially invisible at small
        // scale), prominent when focused — matches the SwiftUI
        // intensity values (0.35 ambient → 0.95 focused).
        _shimmer.SetIntensity(_focused ? 0.95 : 0.0);
    }

    private bool HandleSubmitKey(NSEvent ev)
    {
        // IME safety: don't fire submit while the IME has marked
        // text. NSTextInputContext.CurrentInputContext exposes the
        // current marked-text state.
        // HasMarkedText is a property in the binding (selector
        // `hasMarkedText`), not a method.
        if (_textView.HasMarkedText) return false;
        // Modifier check: Option or Shift on Return → newline (let
        // the text view handle the default).
        var mods = ev.ModifierFlags;
        if (mods.HasFlag(NSEventModifierMask.AlternateKeyMask)
            || mods.HasFlag(NSEventModifierMask.ShiftKeyMask))
        {
            return false;
        }
        SubmitIfReady();
        return true;
    }

    private bool HandleEscapeKey()
    {
        if (IsStreaming)
        {
            OnCancel?.Invoke();
            return true;
        }
        return false;
    }

    private void SubmitIfReady()
    {
        if (!CanSubmit) return;
        var trimmed = (_textView.Value ?? "").Trim();
        if (string.IsNullOrEmpty(trimmed)) return;
        OnSubmit?.Invoke(trimmed);
    }

    private void UpdatePrimaryButton()
    {
        if (_isStreaming)
        {
            _primaryButton.Image = NSImage.GetSystemSymbol(
                "stop.circle.fill", "Stop generating");
            _primaryButton.ToolTip = "Stop";
            _primaryButton.IsDestructive = true;
            _primaryButton.IsOn = false;
            _primaryButton.Enabled = IsEnabled;
        }
        else
        {
            _primaryButton.Image = NSImage.GetSystemSymbol(
                "arrow.up.circle.fill", "Send message");
            _primaryButton.ToolTip = "Send";
            _primaryButton.IsDestructive = false;
            _primaryButton.IsOn = HasNonWhitespaceText;
            _primaryButton.Enabled = CanSubmit;
        }
    }

    /// <summary>Move focus to the text view. Useful for the ⇧⌘A
    /// path that opens the panel and immediately wants typing.</summary>
    public void FocusTextInput()
    {
        Window?.MakeFirstResponder(_textView);
    }

    /// <summary>Internal NSTextView subclass — the only way to hook
    /// Return / Escape and to detect focus / blur on a text view.
    /// AppKit doesn't expose these as events on the text view
    /// itself; we override the responder methods and call into the
    /// owning <see cref="ChatComposerView"/> via delegates.</summary>
    private sealed class ComposerTextView : NSTextView
    {
        public Action? OnTextChange;
        public Action<bool>? OnFocusChange;
        public Func<NSEvent, bool>? OnSubmitKey;
        public Func<bool>? OnEscapeKey;

        public override void DidChangeText()
        {
            base.DidChangeText();
            OnTextChange?.Invoke();
        }

        public override bool BecomeFirstResponder()
        {
            var ok = base.BecomeFirstResponder();
            if (ok) OnFocusChange?.Invoke(true);
            return ok;
        }

        public override bool ResignFirstResponder()
        {
            var ok = base.ResignFirstResponder();
            if (ok) OnFocusChange?.Invoke(false);
            return ok;
        }

        public override void KeyDown(NSEvent theEvent)
        {
            // Intercept Return for submit semantics.
            // 36 = kVK_Return; 76 = kVK_ANSI_KeypadEnter.
            if (theEvent.KeyCode is 36 or 76)
            {
                if (OnSubmitKey?.Invoke(theEvent) == true) return;
            }
            // Intercept Escape for stream-cancel when streaming.
            // 53 = kVK_Escape.
            else if (theEvent.KeyCode == 53)
            {
                if (OnEscapeKey?.Invoke() == true) return;
            }
            base.KeyDown(theEvent);
        }
    }
}
