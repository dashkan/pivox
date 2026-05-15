using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Runtime.CompilerServices;
using Pivox.Shared.Organization;

namespace Pivox.Shared.Ai;

/// <summary>
/// Cross-platform viewmodel that owns the chat transcript state and
/// drives the <see cref="IChatService"/> stream. Mirrors the SwiftUI
/// <c>ConversationViewModel</c> in shape — same state machine, same
/// placeholder-message pattern for streaming — but exposes its state
/// to AppKit / WinUI via <see cref="INotifyPropertyChanged"/> and an
/// <see cref="ObservableCollection{T}"/> instead of SwiftUI's
/// <c>@Observable</c> macro.
///
/// Threading (dotnet/CLAUDE.md Rule 12). The viewmodel captures
/// <see cref="SynchronizationContext.Current"/> at construction and
/// throws <see cref="InvalidOperationException"/> if absent — this
/// validates that the consumer constructs on a UI-attached thread.
/// <see cref="SendAsync"/> must also be called from the UI thread:
/// it uses the default <c>ConfigureAwait(true)</c> behavior so each
/// stream-iteration continuation resumes on the captured context,
/// keeping every <c>Apply</c> mutation and <c>INotifyPropertyChanged</c>
/// event on the UI thread.
///
/// Tests install a <see cref="SynchronizationContext"/> via the
/// test class fixture; the captured context flows across
/// <see cref="ExecutionContext"/> boundaries so the marshal-back
/// behavior is exercised the same way as production. The viewmodel
/// does NOT explicitly <c>Post</c> events through the captured
/// context — that would require a real single-threaded pump in tests
/// and adds complexity without observable production benefit when
/// the caller honors the UI-thread contract.
///
/// Streaming pattern: on first <see cref="TextStartEvent"/>, a
/// placeholder assistant <see cref="Message"/> is appended to
/// <see cref="Messages"/> with empty <c>Text</c>. Each subsequent
/// <see cref="TextDeltaEvent"/> mutates that message's <c>Text</c>
/// in place — UI rows subscribed to <c>Message.PropertyChanged</c>
/// repaint that single row without disturbing the rest of the
/// transcript. <see cref="TextEndEvent"/> finalizes the message and
/// transitions to <see cref="ConversationState.Idle"/>.
///
/// Protocol expectations: text tracks emit Start → 0+ Deltas → End
/// in order. Out-of-order events (Delta before Start, stream close
/// without End) are protocol violations and surface as
/// <see cref="ChatErrorKind.Server"/> errors — they're not silently
/// recovered. The viewmodel's contract with
/// <see cref="IChatService"/> is the proto contract; if the wire
/// violates it, fail visibly.
/// </summary>
public sealed class ConversationViewModel : INotifyPropertyChanged, IDisposable
{
    private readonly IChatService _chat;
    private readonly ActiveOrganization _activeOrganization;
    // Captured at construction to validate UI-thread placement. Not
    // used for explicit Post — see class doc. Kept as a field rather
    // than discarded so future explicit marshaling (e.g., a `Refresh`
    // method called from a background timer) has the hook ready.
    private readonly SynchronizationContext _uiContext;

    private ConversationState _state = ConversationState.Idle;
    private ChatErrorKind? _lastErrorKind;
    private string? _lastErrorMessage;
    private CancellationTokenSource? _streamCts;
    private Message? _inflight;
    private bool _disposed;

    public ConversationViewModel(
        IChatService chat, ActiveOrganization activeOrganization)
    {
        ArgumentNullException.ThrowIfNull(chat);
        ArgumentNullException.ThrowIfNull(activeOrganization);
        _chat = chat;
        _activeOrganization = activeOrganization;
        _uiContext = SynchronizationContext.Current
            ?? throw new InvalidOperationException(
                "ConversationViewModel must be constructed on a thread " +
                "with a SynchronizationContext. macOS and Windows apps " +
                "install one via their event-loop runtimes; tests install " +
                "one via the test class fixture.");

        // Subscribe to organization switches. The UI is single-org —
        // when the user picks a different organization, any in-flight
        // chat is invalidated (the assistant context is org-scoped on
        // the server) and the transcript is wiped. The state-reset
        // logic lives in OnActiveOrganizationChanged.
        _activeOrganization.PropertyChanged += OnActiveOrganizationChanged;
    }

    /// <summary>Ordered transcript of completed and in-flight messages.
    /// Caller observes <c>CollectionChanged</c> for adds; per-row
    /// updates during streaming come from
    /// <see cref="Message.PropertyChanged"/> on the last item while
    /// <see cref="State"/> is <see cref="ConversationState.Streaming"/>.</summary>
    public ObservableCollection<Message> Messages { get; } = [];

    public ConversationState State
    {
        get => _state;
        private set
        {
            if (_state == value) return;
            _state = value;
            RaisePropertyChanged();
        }
    }

    /// <summary>Category of the most recent error, if any. Null when
    /// <see cref="State"/> is not <see cref="ConversationState.Error"/>
    /// or before any error has occurred.</summary>
    public ChatErrorKind? LastErrorKind
    {
        get => _lastErrorKind;
        private set
        {
            if (_lastErrorKind == value) return;
            _lastErrorKind = value;
            RaisePropertyChanged();
        }
    }

    /// <summary>Human-readable message for the most recent error.
    /// Generic; doesn't echo SDK-level detail.</summary>
    public string? LastErrorMessage
    {
        get => _lastErrorMessage;
        private set
        {
            if (_lastErrorMessage == value) return;
            _lastErrorMessage = value;
            RaisePropertyChanged();
        }
    }

    /// <summary>True when <see cref="SendAsync"/> can be safely called.
    /// False during Loading / Streaming.</summary>
    public bool CanSend => State is ConversationState.Idle or ConversationState.Error;

    /// <summary>Issue a new user turn. Appends a user
    /// <see cref="Message"/> to <see cref="Messages"/>, transitions to
    /// <see cref="ConversationState.Loading"/>, and consumes the
    /// service's streaming response until completion, cancellation, or
    /// error. Returns when the stream terminates.
    ///
    /// Re-entrant calls while a stream is in flight throw
    /// <see cref="InvalidOperationException"/> — callers gate via
    /// <see cref="CanSend"/>.</summary>
    public async Task SendAsync(string userText)
    {
        ArgumentNullException.ThrowIfNull(userText);
        ObjectDisposedException.ThrowIf(_disposed, this);
        if (!CanSend)
        {
            throw new InvalidOperationException(
                $"SendAsync called while State={State}; gate via CanSend.");
        }

        // Resolve the active organization once at the top of Send.
        // The UI ensures CanSend is only true when an organization is
        // selected (composer disabled with a hint otherwise), but
        // re-check here so an off-UI caller still gets a clean error
        // instead of a server-side INVALID_ARGUMENT rejection.
        var organizationName = _activeOrganization.Current;
        if (string.IsNullOrEmpty(organizationName))
        {
            FailWith(
                ChatErrorKind.NoOrganization,
                "Select an organization before sending a message.");
            return;
        }

        // Append the user message immediately so the UI shows the turn
        // before the network round-trip starts. The user-message Text
        // is final at construction; no streaming mutation needed.
        Messages.Add(new Message(MessageRole.User, userText));

        // Build the request payload. Phase B stateless slice: the
        // turns we send are the full visible transcript, so the server
        // has the conversation context without a stored conversation
        // id. The viewmodel's Messages collection IS the source of
        // truth. We project to ChatTurn — the DTO shape IChatService
        // consumes — instead of leaking proto types.
        //
        // Project via a switch so an added MessageRole enum member
        // surfaces as a compiler warning here (vs. an open .Where
        // filter that would silently drop unknown roles).
        var turns = Messages
            .Select(m => new ChatTurn(m.Role, m.Text))
            .Where(t => t.Role switch
            {
                MessageRole.User => true,
                MessageRole.Assistant => true,
                MessageRole.Unspecified => false,
                _ => false,
            })
            .ToList()
            .AsReadOnly();

        // Reset error state and transition to Loading. The next event
        // (TextStartEvent) transitions to Streaming; an error during
        // the initial RPC dispatch transitions to Error.
        LastErrorKind = null;
        LastErrorMessage = null;
        State = ConversationState.Loading;
        RaisePropertyChanged(nameof(CanSend));

        // Atomically swap in a fresh CTS, disposing any prior one
        // immediately. Prior CTS should be null on a healthy second
        // send (FinishStream / FailWith dispose it), but the
        // Interlocked.Exchange handles re-entry edges safely.
        var ownedCts = new CancellationTokenSource();
        var oldCts = Interlocked.Exchange(ref _streamCts, ownedCts);
        oldCts?.Dispose();
        var token = ownedCts.Token;

        try
        {
            // ConfigureAwait(true) (default) preserves the captured
            // UI sync context across each iteration — Apply runs on
            // the UI thread because the caller is on the UI thread
            // (precondition; see class doc).
            await foreach (var evt in _chat
                .StreamGenerateAsync(organizationName, turns, token)
                .WithCancellation(token)
                .ConfigureAwait(true))
            {
                Apply(evt, ownedCts);
            }

            // Stream closed cleanly. If we're still mid-stream (an
            // inflight assistant message exists but never got
            // TextEnd), that's a server-side protocol violation —
            // text tracks must emit TextEnd. Fail visibly so the bug
            // surfaces rather than getting masked.
            if (_inflight is not null)
            {
                FailWith(
                    ChatErrorKind.Server,
                    "stream ended without TextEnd (protocol violation)");
            }
            else
            {
                // No in-flight message — stream ended cleanly before
                // any text track started. Treat as Idle (server
                // produced no output for this turn).
                FinishStream();
            }
        }
        catch (OperationCanceledException)
        {
            // Ownership check: if another path (org-switch handler,
            // re-entrant Send, Dispose) already swapped our CTS out
            // and reset the VM state, our cleanup would corrupt the
            // NEW stream's state (clear its inflight, reset its
            // CTS). Bail out leaving the new owner in control.
            // Without this, two interleaved sends — even when gated
            // by the UI thread — can scramble each other's state if
            // a runloop tick lets a Send dispatch between an
            // org-change handler's clear and the cancelled iterator's
            // catch block.
            if (!ReferenceEquals(_streamCts, ownedCts)
                && Volatile.Read(ref _streamCts) is not null)
            {
                return;
            }
            // Cancel() was called or token tripped externally. Drop
            // the in-flight placeholder if it's still empty (no
            // deltas arrived), keep it if it has partial content
            // (the user can see what arrived). Transition to Idle,
            // not Error.
            DiscardEmptyInflight();
            _inflight = null;
            State = ConversationState.Idle;
            RaisePropertyChanged(nameof(CanSend));
            DisposeCts();
        }
        catch (ChatException ex) when (ex.Kind == ChatErrorKind.Cancelled)
        {
            // Server returned Cancelled status — treat as a clean
            // cancel, not as an error. Keeps the state-machine
            // diagram honest ("* --Cancel--> Idle"). Same ownership
            // gate as above.
            if (!ReferenceEquals(_streamCts, ownedCts)
                && Volatile.Read(ref _streamCts) is not null)
            {
                return;
            }
            DiscardEmptyInflight();
            _inflight = null;
            State = ConversationState.Idle;
            RaisePropertyChanged(nameof(CanSend));
            DisposeCts();
        }
        catch (ChatException ex)
        {
            // Same ownership gate. If our CTS was already swapped
            // out, the failure belongs to a stream the new owner is
            // managing — don't double-report.
            if (!ReferenceEquals(_streamCts, ownedCts)
                && Volatile.Read(ref _streamCts) is not null)
            {
                return;
            }
            FailWith(ex.Kind, ex.Message);
        }
        catch (Exception ex)
        {
            // Unexpected — wrap as Server. The implementation should
            // have caught and rewrapped, but defense-in-depth.
            if (!ReferenceEquals(_streamCts, ownedCts)
                && Volatile.Read(ref _streamCts) is not null)
            {
                return;
            }
            FailWith(ChatErrorKind.Server, ex.Message);
        }
    }

    /// <summary>Abort the in-flight stream, if any. No-op when
    /// <see cref="State"/> is not Loading/Streaming. Safe to call
    /// from any thread.</summary>
    public void Cancel()
    {
        var cts = _streamCts;
        cts?.Cancel();
    }

    private void Apply(ChatStreamEvent evt, CancellationTokenSource ownedCts)
    {
        // Stale-stream gate (audit H3). The iterator that produced this
        // event was launched against `ownedCts`. If something else
        // (org-switch handler, re-entrant Send, Dispose) already swapped
        // `_streamCts` out from under us, applying this event would
        // corrupt the new stream's state — most visibly, a TextDelta
        // arriving here AFTER OnActiveOrganizationChanged nulled
        // `_inflight` would hit the "TextDelta before TextStart"
        // protocol-violation branch and surface a spurious
        // Server-kind error, attributed to the freshly-switched org.
        //
        // On a real UI sync context (AppKit main thread / WinUI
        // dispatcher), the handler and the iterator continuation are
        // serial, so this can't race. But we run on multi-threaded
        // test sync contexts (UiThread.Run installs a real one for
        // ours, but defense-in-depth here costs nothing) and a future
        // Cancel() caller from a background thread could trigger the
        // race in production too. Bail silently when stale; the
        // owner of the new `_streamCts` is responsible for the new
        // stream's state.
        if (!ReferenceEquals(_streamCts, ownedCts)) return;

        switch (evt)
        {
            case TextStartEvent start:
                _inflight = new Message(MessageRole.Assistant, "", start.MessageId);
                Messages.Add(_inflight);
                State = ConversationState.Streaming;
                RaisePropertyChanged(nameof(CanSend));
                break;

            case TextDeltaEvent delta:
                // Protocol violation: TextDelta without a preceding
                // TextStart on the same track. Fail visibly rather
                // than silently fabricate a placeholder (which would
                // produce an assistant message with no MessageId and
                // hide the wire-protocol bug).
                if (_inflight is null)
                {
                    FailWith(
                        ChatErrorKind.Server,
                        "TextDelta before TextStart (protocol violation)");
                    return;
                }
                _inflight.Text += delta.Delta;
                break;

            case TextEndEvent:
                FinishStream();
                break;
        }
    }

    private void FinishStream()
    {
        _inflight = null;
        State = ConversationState.Idle;
        RaisePropertyChanged(nameof(CanSend));
        DisposeCts();
    }

    private void FailWith(ChatErrorKind kind, string message)
    {
        DiscardEmptyInflight();
        _inflight = null;
        LastErrorKind = kind;
        LastErrorMessage = message;
        State = ConversationState.Error;
        RaisePropertyChanged(nameof(CanSend));
        DisposeCts();
    }

    private void DisposeCts()
    {
        var cts = Interlocked.Exchange(ref _streamCts, null);
        cts?.Dispose();
    }

    /// <summary>Handles <see cref="ActiveOrganization.Current"/>
    /// changes: cancels any in-flight stream, wipes the transcript,
    /// and resets the error/state slots. The UI operates on one
    /// organization at a time, so the chat history from a previous
    /// organization is invalidated the moment a switch occurs.
    ///
    /// Race-safety: <see cref="Cancel"/> trips the CTS synchronously,
    /// but the async <c>await foreach</c> in <see cref="SendAsync"/>
    /// observes the cancellation on its next iteration. Clearing
    /// <see cref="Messages"/> here happens BEFORE the cancellation
    /// handler runs; that handler's
    /// <see cref="DiscardEmptyInflight"/> path is a safe no-op
    /// against an empty list (the
    /// <c>Messages.Count &gt; 0</c> guard skips cleanly). The
    /// in-flight <see cref="Message"/> instance, if any, becomes
    /// orphaned from the collection — any late
    /// <c>_inflight.Text += delta</c> from a yielded-but-not-yet-
    /// processed event just mutates the orphaned object, which the
    /// UI is no longer subscribed to. No exception, no torn UI.</summary>
    private void OnActiveOrganizationChanged(
        object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName != nameof(ActiveOrganization.Current)) return;
        if (_disposed) return;

        Cancel();
        Messages.Clear();
        _inflight = null;
        LastErrorKind = null;
        LastErrorMessage = null;
        State = ConversationState.Idle;
        RaisePropertyChanged(nameof(CanSend));
        DisposeCts();
    }

    private void DiscardEmptyInflight()
    {
        if (_inflight is not null
            && _inflight.Text.Length == 0
            && Messages.Count > 0
            && ReferenceEquals(Messages[^1], _inflight))
        {
            Messages.RemoveAt(Messages.Count - 1);
        }
        else if (_inflight is not null && _inflight.Text.Length == 0)
        {
            // Reference-equals failure means something inserted
            // between _inflight and the tail. Impossible today;
            // a future mid-stream Messages mutation would trip this.
            // Log so the inconsistency is observable without
            // crashing the UI.
            System.Diagnostics.Debug.WriteLine(
                "[ConversationViewModel] DiscardEmptyInflight: " +
                "_inflight is not the tail of Messages; skipping. " +
                "Indicates an unexpected mid-stream Messages mutation.");
        }
    }

    public void Dispose()
    {
        if (_disposed) return;
        _disposed = true;
        _activeOrganization.PropertyChanged -= OnActiveOrganizationChanged;
        var cts = Interlocked.Exchange(ref _streamCts, null);
        if (cts is not null)
        {
            try { cts.Cancel(); } catch { /* already disposed */ }
            cts.Dispose();
        }
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    private void RaisePropertyChanged([CallerMemberName] string? propertyName = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
}
