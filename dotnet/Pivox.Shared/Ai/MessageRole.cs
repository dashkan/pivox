namespace Pivox.Shared.Ai;

/// <summary>
/// Role of a single conversation turn. Phase B scope: only USER and
/// ASSISTANT are surfaced. TOOL and SYSTEM exist on the wire but are
/// not part of the user-facing transcript model — system instructions
/// arrive out-of-band (a request field), tool turns are wrapped into
/// assistant output by the server.
///
/// Numbers match the proto enum (<c>pivox.ai.v1.Role</c>) so cross-
/// boundary conversions stay trivial. We deliberately don't import the
/// proto enum here — <c>Pivox.Shared</c> can't depend on
/// <c>Pivox.Client</c>, per the layering rule in
/// <c>dotnet/CLAUDE.md</c>.
/// </summary>
public enum MessageRole
{
    Unspecified = 0,
    User = 1,
    Assistant = 2,
}
