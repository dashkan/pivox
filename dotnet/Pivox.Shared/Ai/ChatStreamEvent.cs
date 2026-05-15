namespace Pivox.Shared.Ai;

/// <summary>
/// Domain event union for streaming chat responses. Maps to the proto
/// <c>pivox.ai.v1.ServerEvent</c> oneof, but stays in
/// <c>Pivox.Shared</c> so the cross-platform viewmodel can pattern-
/// match on these types without depending on the generated proto
/// surface in <c>Pivox.Client</c>.
///
/// Phase B scope: only the text-track events are surfaced
/// (TextStart/TextDelta/TextEnd). The reasoning, tool, and artifact
/// tracks exist on the wire but aren't rendered by the vertical
/// slice — the macOS chat service consumes them and drops them on the
/// floor for now. Adding them is a Phase C/D matter when the
/// corresponding UI lands.
///
/// Abstract record + sealed subtypes models the discriminated union
/// pattern: switch expressions on the union are exhaustive when every
/// sealed subtype is covered, and there's no nullable-or-default
/// payload pollution.
/// </summary>
public abstract record ChatStreamEvent;

/// <summary>Server has begun an assistant response. Carries the
/// message id that the server will associate with this stream's
/// final persisted message. Triggers the viewmodel to append a
/// placeholder assistant <see cref="Message"/>.</summary>
public sealed record TextStartEvent(string MessageId) : ChatStreamEvent;

/// <summary>A chunk of assistant text. Appended to the in-flight
/// message's <see cref="Message.Text"/> by the viewmodel.</summary>
public sealed record TextDeltaEvent(string Delta) : ChatStreamEvent;

/// <summary>End of the assistant text track. Viewmodel finalizes the
/// in-flight message and returns to <see cref="ConversationState.Idle"/>.</summary>
public sealed record TextEndEvent : ChatStreamEvent;
