namespace Pivox.Shared.Auth;

/// <summary>
/// Exception thrown by <see cref="IAuthService"/> implementations
/// to surface a categorized auth failure to the view-model layer.
/// <see cref="Exception.Message"/> carries a user-facing string
/// (already translated via <see cref="AuthErrorMessages"/> by the
/// platform impl) so view-models that do
/// <c>ErrorMessage = ex.Message</c> get polished UX copy without
/// any platform-aware logic. The underlying SDK exception is
/// preserved as <see cref="Exception.InnerException"/> for log
/// inspection.
///
/// Threading: thrown synchronously from
/// <see cref="IAuthService"/> methods that return failed tasks.
/// View-models catch it via the standard
/// <c>catch (Exception ex)</c> pattern.
/// </summary>
public sealed class AuthException : Exception
{
    public AuthException(
        AuthErrorCode code, string userMessage, Exception? inner = null)
        : base(userMessage, inner)
    {
        Code = code;
    }

    /// <summary>Categorized error code. View-models that need to
    /// branch on the cause (e.g., route to a different screen on
    /// <see cref="AuthErrorCode.UserDisabled"/>) read this rather
    /// than parsing the message string.</summary>
    public AuthErrorCode Code { get; }
}
