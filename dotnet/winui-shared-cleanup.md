# WinUI — shared-infra cleanup consumption brief

Three shared-infra changes landed on `main` (commits `f4feef2` +
`5d97b34`) from the audit items the WinUI side raised. WinUI now
needs to consume them — the changes are intentional improvements
sitting in shared code; both platforms benefit equally.

Commit 3 (force token refresh on startup) was already done on
WinUI in `b47914c` — macOS got the matching fix in `51dc50b`. Not
covered here.

Status across the two open consumption items:

| Item | What's in shared code | WinUI consumes |
|---|---|---|
| 1. Plaintext stripped | `CloudConfig` always TLS; `PivoxClient` always SecureSsl | ✅ already builds against the new shape (only the 2-arg ctor remains) |
| 2. SharedHttp + SsoProviderResolver | `Pivox.Shared.Http.SharedHttp.Instance`; `Pivox.Shared.Auth.SsoProviderResolver.ResolveAsync` | 🔜 swap `s_http` + delegate `ResolveSsoProviderAsync` |
| 3. AuthErrorCode + AuthErrorMessages | `Pivox.Shared.Auth.AuthErrorCode` enum, `AuthErrorMessages.Get`, `AuthException` | 🔜 translate native `AuthError` → `AuthErrorCode`, throw `AuthException` |

Read alongside `dotnet/CLAUDE.md` (the cloud-endpoint-config and
shared-HttpClient sections were updated as part of commit `f4feef2`).

---

## Item 1: plaintext is gone

`CloudConfig.GrpcUri` always returns `https://`. `PivoxClient`'s
`usePlaintext` constructor parameter and the
`UnsafeUseInsecureChannelCallCredentials` branch are removed. The
default ctor (`new PivoxClient(IAuthService auth)`) now takes one
argument; the second ctor signature is
`PivoxClient(Uri endpoint, IAuthService auth)`.

### What WinUI does

Likely nothing — WinUI's call sites already use the parameter-less
default ctor. Verify by:

```sh
cd dotnet && dotnet build Pivox.WinUI/Pivox.WinUI.csproj
```

If a call site explicitly passed `usePlaintext`, it stops compiling
and the fix is to drop the parameter. CloudConfig's `UsePlaintext`
property is also gone; no consumers expected.

The dotnet stack deliberately diverges from
`CloudConfig.swift` here — Swift keeps `PIVOX_GRPC_PLAINTEXT`,
dotnet doesn't. Documented in
`Pivox.Shared/CloudConfig.cs`'s class-doc so the next reader
doesn't try to "restore parity."

---

## Item 2: SharedHttp + SsoProviderResolver

### `Pivox.Shared.Http.SharedHttp.Instance`

Single process-wide `HttpClient` with a 30 s timeout. Use it for
**every** plain-HTTP call — OAuth token exchanges, SSO resolution,
future broker REST endpoints. Don't construct new `HttpClient`
instances; they fragment the connection pool and exhaust ephemeral
ports under load.

WinUI's `WindowsAuthService.s_http` field (line 40 in the file as
of `28eecd6`) is now redundant. Same field on the macOS side has
been deleted in commit `f4feef2` — every `s_http.SendAsync` /
`s_http.PostAsync` call routes through `SharedHttp.Instance`.

#### Changes WinUI needs

```diff
- private static readonly HttpClient s_http = new();
```

Delete that field. Then in any method that used it:

```diff
- using var resp = await s_http.SendAsync(req, ct);
+ using var resp = await Pivox.Shared.Http.SharedHttp.Instance.SendAsync(req, ct);

- var tokenResponse = await s_http.PostAsync(
+ var tokenResponse = await Pivox.Shared.Http.SharedHttp.Instance.PostAsync(
```

Add `using Pivox.Shared.Http;` so the type resolves unqualified.

The OAuth token-exchange site (Google PKCE flow, around line 271
of `WindowsAuthService.cs` in the current revision) is the primary
non-resolver consumer. Anywhere else that touches `s_http`
similarly.

### `Pivox.Shared.Auth.SsoProviderResolver.ResolveAsync`

Static helper that performs the
`{BrokerBaseUrl}/internal/v1/auth:resolveProvider` HTTP POST,
JSON-encodes the email field via `JavaScriptEncoder` (AOT-safe, no
`JsonSerializer<T>` reflection), and parses the response.

Returns `string?` — the provider id on 200, `null` on 404 (or
empty input), throws `InvalidOperationException` on other status
codes.

#### Changes WinUI needs

Replace the entire body of
`WindowsAuthService.ResolveSsoProviderAsync` with a one-line
delegation:

```diff
 public Task<string?> ResolveSsoProviderAsync(
     string email, CancellationToken ct = default)
-{
-    var trimmed = email.Trim();
-    if (string.IsNullOrEmpty(trimmed)) return null;
-    // … 30 lines of duplicated HTTP-call ceremony …
-}
+    => Pivox.Shared.Auth.SsoProviderResolver.ResolveAsync(email, ct);
```

Note the signature change: `async Task<string?>` → `Task<string?>`
(no async, no await, no body braces). Add `using Pivox.Shared.Auth;`
if the type doesn't resolve unqualified.

The `IAuthService.ResolveSsoProviderAsync` contract is unchanged;
this is an implementation extraction. No call-site changes
upstream.

---

## Item 3: AuthErrorCode + AuthErrorMessages + AuthException

Three new shared types replace the "throw exception with raw
Firebase message" pattern that both platforms had:

- `Pivox.Shared.Auth.AuthErrorCode` — canonical Pivox enum
  (Unknown / InvalidEmail / WrongPassword / EmailAlreadyInUse /
  AccountExistsWithDifferentCredential / WeakPassword /
  NetworkError / TooManyRequests / OperationNotAllowed /
  UserDisabled). Mirrors the subset of Firebase codes that the
  SwiftUI app maps to polished UX strings.
- `Pivox.Shared.Auth.AuthErrorMessages.Get(AuthErrorCode)` — static
  mapper returning the user-facing string for each code. Strings
  taken verbatim from SwiftUI's `firebaseErrorMessage(_:)` in
  `native/.../Auth/AuthService.swift`, with one deliberate exception:
  `AuthErrorCode.UserDisabled` returns the SAME string as
  `WrongPassword` ("Incorrect email or password."). Firebase's
  email/password endpoint checks the disabled-account flag BEFORE
  password validation, so a distinct "This account has been
  disabled." message would be a clean email-enumeration oracle —
  any password attempt against a disabled email leaks "this email
  exists in our system." The discriminator code stays separate for
  telemetry/admin-tooling use; only the user-facing message is
  unified. **Don't undo this when implementing the WinUI mapping.**
- `Pivox.Shared.Auth.AuthException` — exception with `.Code` +
  user message + inner exception. ViewModels that do
  `ErrorMessage = ex.Message` get polished copy automatically.

### Two-tier translation pattern

Each platform decodes its native SDK error into the canonical
`AuthErrorCode`, then `AuthErrorMessages.Get(code)` produces the
user message:

```
NativeSDK-specific error code  ──translate──▶  AuthErrorCode  ──map──▶  user message
       (platform impl)                            (shared)            (shared)
```

This keeps SDK knowledge at the platform boundary and UX copy in
one shared place.

### macOS reference implementation

`MacOsAuthService.cs` (commit `5d97b34`):

```csharp
private static AuthException ToAuthException(NSError nsError, string contextLabel)
{
    var code = MapToAuthCode(nsError);
    var message = code == AuthErrorCode.Unknown
        ? (nsError.LocalizedDescription
            ?? AuthErrorMessages.Get(AuthErrorCode.Unknown))
        : AuthErrorMessages.Get(code);

    Console.Error.WriteLine(
        $"[Auth] {contextLabel} failed: code={nsError.Code} → {code}, "
        + $"raw='{nsError.LocalizedDescription}'");

    return new AuthException(code, message);
}

private static AuthErrorCode MapToAuthCode(NSError nsError)
{
    if (nsError.Domain != "FIRAuthErrorDomain") return AuthErrorCode.Unknown;
    return (FIRAuthErrorCode)(long)nsError.Code switch
    {
        FIRAuthErrorCode.InvalidEmail => AuthErrorCode.InvalidEmail,
        FIRAuthErrorCode.WrongPassword
            or FIRAuthErrorCode.UserNotFound
            or FIRAuthErrorCode.InvalidCredential
            => AuthErrorCode.WrongPassword,
        FIRAuthErrorCode.EmailAlreadyInUse
            => AuthErrorCode.EmailAlreadyInUse,
        FIRAuthErrorCode.AccountExistsWithDifferentCredential
            or FIRAuthErrorCode.CredentialAlreadyInUse
            => AuthErrorCode.AccountExistsWithDifferentCredential,
        FIRAuthErrorCode.WeakPassword => AuthErrorCode.WeakPassword,
        FIRAuthErrorCode.NetworkError => AuthErrorCode.NetworkError,
        FIRAuthErrorCode.TooManyRequests => AuthErrorCode.TooManyRequests,
        FIRAuthErrorCode.OperationNotAllowed => AuthErrorCode.OperationNotAllowed,
        FIRAuthErrorCode.UserDisabled => AuthErrorCode.UserDisabled,
        _ => AuthErrorCode.Unknown,
    };
}
```

Note the `Unknown` branch surfaces Firebase's localized description
as the user message rather than the generic "something went wrong"
— SwiftUI's rationale: an actionable SDK-specific message beats a
useless generic for codes we haven't categorized yet (MFA,
verification, etc.).

### WinUI implementation

WinUI catches errors from the C++/WinRT bridge differently —
the Firebase C++ SDK returns error codes via the
`AuthError` enum (or surfaces an exception with a code on the
projected async method). Inspect what
`FirebaseAuthBridge` actually surfaces today; the binding may
project as a numeric code or as a string-coded exception.

Whatever shape the WinUI bridge produces, the conversion lives in
`WindowsAuthService` as a static helper:

```csharp
private static AuthException ToAuthException(
    /* native error type */ err, string contextLabel)
{
    var code = MapToAuthCode(err);
    var message = code == AuthErrorCode.Unknown
        ? /* SDK's English message */ ?? AuthErrorMessages.Get(AuthErrorCode.Unknown)
        : AuthErrorMessages.Get(code);

    Console.Error.WriteLine(
        $"[Auth] {contextLabel} failed: code={/* native code */} → {code}");

    return new AuthException(code, message);
}

private static AuthErrorCode MapToAuthCode(/* native error type */ err)
{
    return /* native code */ switch
    {
        /* Firebase C++ InvalidEmail */ => AuthErrorCode.InvalidEmail,
        /* WrongPassword or UserNotFound or InvalidCredential */
            => AuthErrorCode.WrongPassword,
        /* EmailAlreadyInUse */ => AuthErrorCode.EmailAlreadyInUse,
        // … rest mirroring the macOS switch …
        _ => AuthErrorCode.Unknown,
    };
}
```

The Firebase C++ `AuthError` enum lives in
`firebase/auth.h` (Firebase C++ SDK) — codes are numeric and
mostly mirror the iOS `FIRAuthErrorCode` values. Check
`Pivox.Firebase.Native/FirebaseAuthBridge.cpp` / `.h` /
`.idl` to see how the bridge exposes them (the `.idl` is the
projected surface; if it strips the code and only surfaces a
message string, the bridge needs to grow a code-carrying field
— which is a bigger change but worth it for this mapping).

#### Throw sites to replace

Every `WindowsAuthService` site that today surfaces a Firebase
error message via `throw new InvalidOperationException($"… {sdkErr}")`
should become `throw ToAuthException(sdkErr, "context-label")`.
Similarly, defensive "shouldn't happen" sites (null user after
sign-in, etc.) should use a parallel `InternalAuthError(string
context)` helper:

```csharp
private static AuthException InternalAuthError(string context)
{
    Console.Error.WriteLine($"[Auth] internal: {context}");
    return new AuthException(
        AuthErrorCode.Unknown,
        AuthErrorMessages.Get(AuthErrorCode.Unknown));
}
```

### ViewModel side — no change

`LoginViewModel` and `RegisterViewModel` in `Pivox.Shared` already
do `ErrorMessage = ex.Message` in their catch blocks. `AuthException
.Message` IS the user message, so the existing catch automatically
surfaces polished copy. No view-model changes needed.

If a view-model later wants to branch on the error category (e.g.,
route to a different screen on `UserDisabled`), it can downcast:

```csharp
catch (AuthException ex) when (ex.Code == AuthErrorCode.UserDisabled)
{
    // disabled-account flow
}
catch (Exception ex)
{
    ErrorMessage = ex.Message;
}
```

Not needed for this commit — Phase B / future work.

---

## Validation checklist

After consuming all three items:

- [ ] `dotnet build Pivox.WinUI/Pivox.WinUI.csproj` succeeds with
      zero warnings
- [ ] `s_http` field is deleted from `WindowsAuthService` —
      `grep -rn 's_http' dotnet/Pivox.WinUI/` returns no hits
- [ ] `ResolveSsoProviderAsync` is a single-line delegation to
      `SsoProviderResolver.ResolveAsync`
- [ ] Every former
      `throw new InvalidOperationException($"Firebase … {err}…")`
      site is now `throw ToAuthException(err, label)` or
      `throw InternalAuthError(label)`
- [ ] LoginPage / RegisterPage surface SwiftUI-equivalent copy
      on each canonical error path (manual test: trigger
      InvalidEmail → "Invalid email address.", trigger
      WrongPassword → "Incorrect email or password.")
- [ ] Disabled-account flow (force-refresh, commit `b47914c` +
      this commit's mapper) surfaces "This account has been
      disabled." rather than Firebase's verbose default

## Maintenance trigger

If the Firebase C++ SDK ever updates its `AuthError` numeric values
(rare — they're stable across versions), the WinUI `MapToAuthCode`
switch falls through to `Unknown` for renumbered codes and the
user message degrades to the SDK's English. The fix is to update
the switch arms; the canonical
`AuthErrorCode` enum doesn't have to change.

Same applies on macOS if `FIRAuthErrorCode` ever shifts — `Unknown`
fallback degrades gracefully, fix the switch when noticed.
