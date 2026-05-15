namespace Pivox.Shared.Auth;

/// <summary>
/// Canonical Pivox auth error codes. Both platform
/// <see cref="IAuthService"/> implementations translate their
/// native error surface (Firebase Cocoa SDK's
/// <c>FIRAuthErrorCode</c> on macOS, Firebase C++ SDK's
/// <c>AuthError</c> enum on Windows) into one of these values so
/// the cross-platform <c>AuthErrorMessages</c> mapper can emit a
/// consistent user-facing string across both stacks.
///
/// Mirrors the subset of Firebase auth errors that the SwiftUI
/// app maps to polished UX strings in
/// <c>AuthService.swift's firebaseErrorMessage(...)</c> — those
/// strings are the canonical UX (they've been in production) and
/// must match across the entire client family.
///
/// Codes not in this enum fall through to
/// <see cref="Unknown"/>. The platform impl is then free to pass
/// the underlying SDK's localized description as the user message,
/// rather than the generic "something went wrong" fallback —
/// mirrors the SwiftUI behavior where unmapped codes get
/// <c>nsError.localizedDescription</c>.
/// </summary>
public enum AuthErrorCode
{
    /// <summary>The Firebase SDK returned an error we don't have a
    /// canonical mapping for. Callers should use the platform's
    /// localized description for the user message rather than
    /// the generic fallback.</summary>
    Unknown = 0,

    /// <summary>Email failed format validation (Firebase
    /// <c>InvalidEmail</c>).</summary>
    InvalidEmail,

    /// <summary>Sign-in credential rejected. Collapses three
    /// Firebase codes (<c>WrongPassword</c>, <c>UserNotFound</c>,
    /// <c>InvalidCredential</c>) into one user-facing message to
    /// avoid leaking account-existence signal — "wrong password"
    /// vs. "no such user" lets an attacker enumerate valid emails.</summary>
    WrongPassword,

    /// <summary>An account already exists with this email
    /// (<c>EmailAlreadyInUse</c>). UI typically suggests sign-in
    /// instead.</summary>
    EmailAlreadyInUse,

    /// <summary>An account exists with this email but signs in
    /// via a different provider (<c>AccountExistsWithDifferentCredential</c>
    /// or related — credential-already-in-use,
    /// account-exists-with-different-credential). Caller has to
    /// re-route through the original sign-in method.</summary>
    AccountExistsWithDifferentCredential,

    /// <summary>Password failed Firebase's strength check
    /// (<c>WeakPassword</c>).</summary>
    WeakPassword,

    /// <summary>Network failure during the Firebase exchange
    /// (<c>NetworkError</c>).</summary>
    NetworkError,

    /// <summary>Rate-limited by Firebase (<c>TooManyRequests</c>).
    /// Typically transient.</summary>
    TooManyRequests,

    /// <summary>The sign-in provider is disabled in the Firebase
    /// project config (<c>OperationNotAllowed</c>). User can't
    /// fix; surfaces as a configuration error.</summary>
    OperationNotAllowed,

    /// <summary>The account has been disabled server-side
    /// (<c>UserDisabled</c>). Surfaces on app launch when the
    /// force-refresh during construction discovers the disabled
    /// state, and on every sign-in attempt against a disabled
    /// account (Firebase checks the disabled flag before
    /// validating the password).
    ///
    /// Security note: the user-facing string for this code is
    /// deliberately collapsed with
    /// <see cref="WrongPassword"/> in
    /// <c>AuthErrorMessages.Get</c> — distinguishing them would
    /// be an email-enumeration oracle. The discriminator stays
    /// here for internal use (logging, telemetry, admin tooling
    /// that needs to distinguish the cases without surfacing the
    /// distinction to end users).</summary>
    UserDisabled,
}
