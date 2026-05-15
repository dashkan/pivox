using System.ComponentModel;
using System.Runtime.CompilerServices;

namespace Pivox.Shared.Ai;

/// <summary>
/// A single message in the conversation transcript. Mutable
/// <see cref="Text"/> with <see cref="INotifyPropertyChanged"/> so the
/// UI can subscribe per-row and only repaint the affected message
/// during streaming — appending deltas to a placeholder assistant
/// message doesn't force a full transcript redraw.
///
/// The placeholder pattern mirrors the SwiftUI side: on
/// <c>TextStartEvent</c> a new Message is appended with empty Text;
/// each <c>TextDeltaEvent</c> mutates <see cref="Text"/> in place;
/// <c>TextEndEvent</c> leaves the message as-is and transitions the
/// viewmodel back to Idle.
///
/// <see cref="MessageId"/> is the server-assigned identifier from
/// <c>TextStartEvent.message_id</c>. For user messages composed
/// locally before the server has acknowledged them, this is the empty
/// string until the server echoes back an id (Phase B leaves user
/// messages id-less — they're not persisted via the stateless calls
/// we use for the vertical slice).
/// </summary>
public sealed class Message : INotifyPropertyChanged
{
    private string _text;

    public Message(MessageRole role, string text, string messageId = "")
    {
        Role = role;
        _text = text;
        MessageId = messageId;
    }

    public MessageRole Role { get; }

    /// <summary>Server-side message id. Empty for locally-composed
    /// messages that haven't been associated with a server response
    /// yet. Once assigned, treated as immutable.</summary>
    public string MessageId { get; }

    /// <summary>Message body. Mutates during streaming for assistant
    /// messages; raises <see cref="PropertyChanged"/> on each
    /// mutation. User-message text is set once at construction and
    /// shouldn't change, but the property is mutable for symmetry
    /// (and for future "edit user turn" surfaces).</summary>
    public string Text
    {
        get => _text;
        set
        {
            if (_text == value) return;
            _text = value;
            RaisePropertyChanged();
        }
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    private void RaisePropertyChanged([CallerMemberName] string? propertyName = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
}
