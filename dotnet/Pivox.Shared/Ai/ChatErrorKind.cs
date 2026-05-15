namespace Pivox.Shared.Ai;

/// <summary>
/// Coarse error categories surfaced from <see cref="IChatService"/>.
/// Kept deliberately small — the user-facing routing differs by
/// category (sign-in prompt vs. retry button vs. fatal-error sheet),
/// but the underlying SDK exception details are NOT surfaced to UI
/// (they'd leak account-existence signal on
/// <see cref="NotSignedIn"/> / <see cref="AuthenticationRequired"/>,
/// and they're not actionable for users on the other categories).
///
/// Detailed underlying errors are logged internally by the
/// implementation. The UI shows a generic message keyed off the kind.
/// </summary>
public enum ChatErrorKind
{
    /// <summary>No Firebase user is currently signed in. UI should
    /// route to the sign-in screen.</summary>
    NotSignedIn,

    /// <summary>Auth was expected but failed (token fetch threw,
    /// session expired, etc.). UI should prompt for re-authentication.</summary>
    AuthenticationRequired,

    /// <summary>Authenticated, but the caller lacks permission for
    /// this organization / resource (e.g., not a member, role
    /// doesn't include chat). UI should show "you don't have access"
    /// rather than route to re-sign-in — re-auth won't fix this.</summary>
    PermissionDenied,

    /// <summary>Network failure (no connection, gRPC channel down,
    /// stream interrupted mid-flight). UI should offer retry.</summary>
    Network,

    /// <summary>The server returned a non-success gRPC status that
    /// isn't auth-related (INVALID_ARGUMENT, RESOURCE_EXHAUSTED,
    /// INTERNAL, etc.). UI should show a generic "something went
    /// wrong" sheet and offer retry.</summary>
    Server,

    /// <summary>The stream was cancelled by the caller. Not a real
    /// "error" in the user sense — included so the viewmodel can
    /// distinguish cancellation from network-interrupted-mid-stream
    /// when transitioning state.</summary>
    Cancelled,
}
