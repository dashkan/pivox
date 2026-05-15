namespace Pivox.Shared.Auth;

/// <summary>
/// Maps <see cref="AuthErrorCode"/> values to user-facing strings.
///
/// Strings are taken verbatim from the SwiftUI app's
/// <c>firebaseErrorMessage(_:)</c> in
/// <c>native/.../Auth/AuthService.swift</c> — those have been in
/// production, vetted for UX, and the SwiftUI source explicitly
/// states all client platforms must surface identical copy.
/// <see cref="UserDisabled"/> is an explicit addition here (SwiftUI
/// falls through to Firebase's default for it) because the dotnet
/// force-refresh-on-launch behavior makes the disabled-account case
/// prominent — a polished message beats Firebase's verbose
/// "The user account has been disabled by an administrator."
///
/// <see cref="AuthErrorCode.Unknown"/> doesn't get a mapping here;
/// the platform layer should pass through its native SDK's
/// localized description as the user message (the SwiftUI side's
/// rationale: an actionable SDK-specific message beats a generic
/// "something went wrong" for codes we haven't categorized yet).
/// </summary>
public static class AuthErrorMessages
{
    /// <summary>Maps an <see cref="AuthErrorCode"/> to its
    /// user-facing string. For <see cref="AuthErrorCode.Unknown"/>,
    /// returns the generic fallback — callers who can do better
    /// (i.e., have the underlying SDK error in hand) should
    /// substitute that instead.</summary>
    public static string Get(AuthErrorCode code) => code switch
    {
        AuthErrorCode.InvalidEmail =>
            "Invalid email address.",
        AuthErrorCode.WrongPassword =>
            "Incorrect email or password.",
        AuthErrorCode.EmailAlreadyInUse =>
            "An account with this email already exists.",
        AuthErrorCode.AccountExistsWithDifferentCredential =>
            "This email is already linked to a different sign-in " +
            "method. Sign in with that method first, then link the " +
            "new provider from your profile.",
        AuthErrorCode.WeakPassword =>
            "Password is too weak. Use at least 6 characters.",
        AuthErrorCode.NetworkError =>
            "Network error. Check your connection.",
        AuthErrorCode.TooManyRequests =>
            "Too many attempts. Try again later.",
        AuthErrorCode.OperationNotAllowed =>
            "This sign-in provider is not enabled.",
        AuthErrorCode.UserDisabled =>
            "This account has been disabled.",
        _ =>
            "Something went wrong. Please try again.",
    };
}
