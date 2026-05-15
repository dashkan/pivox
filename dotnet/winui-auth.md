# WinUI — Auth build brief

The dotnet macOS side of the auth flow is built and validated. This
is the build brief for matching it on WinUI.

**Canonical reference: the new dotnet macOS implementation in
`dotnet/Pivox.macOS/`.** Use it as the source of truth for layout,
behavior, state machines, theming, and lifecycle. The
`native/platform/macos/swift/Auth/` SwiftUI sources are NOT the
reference for design or wiring — they're outdated relative to the
post-audit dotnet build. The exception: `native/platform/macos/Assets.xcassets/`
remains the source of visual assets (Google logo, GitHub logo, etc.).
Copy from there; don't re-source them elsewhere.

Read alongside `dotnet/CLAUDE.md` (Rules 12–18 cover threading,
lifecycle, layout, glass, controller-ownership, and accent-color
patterns from the macOS side — translate the spirit; some have
WinUI-specific equivalents called out below).

## Shared contract

`Pivox.Shared/Auth/IAuthService.cs` defines the cross-platform auth
surface. `WindowsAuthService` implements it against the C++/WinRT
Firebase bridge.

```csharp
public interface IAuthService
{
    AuthSession? Current { get; }
    event EventHandler<AuthSession?>? CurrentChanged;

    Task<AuthSession> SignInWithEmailAsync(
        string email, string password, CancellationToken ct = default);

    Task<AuthSession> SignInWithGoogleAsync(CancellationToken ct = default);

    Task<AuthSession> SignInWithGitHubAsync(CancellationToken ct = default);

    Task<AuthSession> CreateAccountAsync(
        string email, string password, string displayName,
        CancellationToken ct = default);

    Task SendPasswordResetAsync(string email, CancellationToken ct = default);

    /// Returns the SAML/OIDC provider id (e.g. "saml.foo-corp") if
    /// the email's domain is enrolled in pivox-cloud SSO; null if
    /// password auth applies.
    Task<string?> ResolveSsoProviderAsync(
        string email, CancellationToken ct = default);

    Task<AuthSession> SignInWithSsoAsync(
        string providerId, string loginHint, CancellationToken ct = default);

    Task SignOutAsync(CancellationToken ct = default);

    Task<string> GetIdTokenAsync(CancellationToken ct = default);
}
```

`Pivox.Shared/Auth/LoginViewModel.cs` and
`Pivox.Shared/Auth/RegisterViewModel.cs` are the shared state
machines. The WinUI XAML pages bind to them; no XAML-side state.

`Pivox.Shared/Navigation/AppRoute.cs` has `Login`, `Register`, and
`Shell` as the auth-area routes. WinUI's route observer must handle
each.

## Build the C++/WinRT bridge surface

Two methods need Firebase C++ SDK access — add to
`Pivox.Firebase.Native/FirebaseAuthBridge.idl` (and `.h`/`.cpp`):

```idl
// firebase::auth::Auth::CreateUserWithEmailAndPassword(...)
// + the post-create UpdateUserProfile(displayName) step on
// firebase::auth::User. Returns the post-update ID token JWT so the
// C# adapter can build an AuthSession the same way the email/password
// path does.
IAsyncOperation<String> CreateAccountAsync(
    String email, String password, String displayName);

// firebase::auth::Auth::SendPasswordResetEmail(...)
IAsyncAction SendPasswordResetAsync(String email);
```

GitHub OAuth does NOT need a new bridge method — it uses the
existing `SignInWithCredentialAsync` (same as Google) with provider
id `"github.com"`. The OAuth flow itself runs in C# via the WebView2
popup.

SSO does NOT need a new bridge method either. The macOS implementation
uses the OIDC broker's `(id_token, raw_nonce)` callback fragment and
builds a credential via Firebase's `OAuthProvider::GetCredential
(providerID, id_token, raw_nonce)`, then signs in with that credential
through the existing `SignInWithCredentialAsync` bridge method.
Verify the Firebase C++ SDK exposes
`firebase::auth::OAuthProvider::GetCredential(providerID, idToken,
nullptr, rawNonce)` (the 4-arg overload — accessToken parameter is
nullable for OIDC). If it does, the bridge stays unchanged for SSO.

Pattern for the new bridge methods: follow the
`SignInWithEmailAsync` shape in `FirebaseAuthBridge.cpp`
(`AwaitFuture` helper, error → `winrt::hresult_error`, success →
`co_return co_await GetCurrentUserTokenAsync(false)` for the
session-returning methods).

## Build `WindowsAuthService` implementations

### CreateAccountAsync

```csharp
public async Task<AuthSession> CreateAccountAsync(
    string email, string password, string displayName,
    CancellationToken ct = default)
{
    var jwt = await _bridge.CreateAccountAsync(email, password, displayName)
        .AsTask(ct);
    var session = BuildSession(jwt);
    SetCurrent(session);
    return session;
}
```

The C++ bridge handles the two-step Firebase-side flow (create user
+ UpdateUserProfile + Reload to refresh the JWT with the `name`
claim) so the C# adapter sees a single round-trip.

### SendPasswordResetAsync

```csharp
public Task SendPasswordResetAsync(string email, CancellationToken ct = default)
    => _bridge.SendPasswordResetAsync(email).AsTask(ct);
```

### SignInWithGitHubAsync

Refactor `PerformGoogleOAuthAsync` into a generic provider-config
shape and call it for both Google and GitHub:

```csharp
private sealed record OAuthProviderConfig(
    string ProviderId,        // "google.com" or "github.com"
    string AuthorizeUrl,
    string TokenUrl,
    string ClientId,
    string CallbackScheme,
    string Scopes);
```

The macOS implementation in
`MacOsAuthService.PerformGoogleOAuthAsync` is the reference shape —
same PKCE plumbing, different provider config. The macOS
`MacOsAuthService.PerformGitHubOAuthAsync` is the GitHub-specific
variant; mirror the OAuth-response parsing (`access_token` only —
GitHub doesn't return an `id_token`; the credential is built from
the access token alone).

### ResolveSsoProviderAsync

Calls pivox-cloud's `ResolveProvider` gRPC. The endpoint lives on
`PivoxClient.Iam` (verify against the generated C# in
`Pivox.Client/bin/.../`).

```csharp
public async Task<string?> ResolveSsoProviderAsync(
    string email, CancellationToken ct = default)
{
    var resp = await _pivox.Iam.ResolveProviderAsync(
        new ResolveProviderRequest { Email = email },
        cancellationToken: ct);
    return string.IsNullOrEmpty(resp.ProviderId) ? null : resp.ProviderId;
}
```

`_pivox` is the `PivoxClient` — needs to be wired in via the
constructor (currently `WindowsAuthService` only takes the bridge;
add `PivoxClient` to the ctor parameters and propagate from
`App.OnLaunched`).

### SignInWithSsoAsync

OIDC broker flow — mirrors the macOS implementation in
`MacOsAuthService.SignInWithSsoAsync`. The broker handles the
upstream OAuth dance with the IdP server-side; the client just
parses the callback fragment for the id_token + raw_nonce and uses
them to build a Firebase OAuth credential.

1. Build the broker URL:
   `https://<broker>/internal/v1/auth/<providerId>/start?return=pivox://auth-complete&login_hint=<email>`.
   The "pivox" scheme is just the callback intercept; the client
   never registers it as a system URL handler.
2. Open WebView2 popup against the broker URL.
3. Broker → IdP authorize → user authenticates → IdP → broker callback
   → broker redirects to `pivox://auth-complete#kind=oidc_id_token&provider=<providerId>&token=<id_token>&nonce=<raw_nonce>`.
4. Intercept the redirect in WebView2's navigation handler, parse the
   fragment (NOT the query — broker carries credentials in the fragment).
5. Validate `provider == expectedProviderId` and `kind == "oidc_id_token"`,
   extract `token` and `nonce`.
6. Call `_bridge.SignInWithCredentialAsync("<providerId>", id_token, raw_nonce)`.
   The bridge wraps `firebase::auth::OAuthProvider::GetCredential(providerID,
   idToken, /*accessToken*/ nullptr, rawNonce)` and signs in with it.
   No custom token, no extra broker round-trip.

If the C++ SDK doesn't expose the 4-arg OAuthProvider overload that
accepts rawNonce, raise that as a blocker before proceeding —
**do not** route SSO through a custom-token path; that would require
a backend change (custom-token mint endpoint on pivox-cloud) and
diverge from the macOS architecture. The Firebase C++ SDK 13.x does
expose `OAuthProvider::GetCredential` per the public headers; verify
the bridge can pass it through cleanly.

## Theming on WinUI

The shared theme tokens live in `Pivox.Shared/UI/Theme.cs`:
`ThemeColor` (semantic palette), `ThemeFont` (typography roles),
`ThemeMetrics` (spacing scale + numeric design tokens). They're
platform-agnostic. Each platform realizes them.

macOS has `Pivox.macOS/UI/ThemeColors.cs` (returns `NSColor`) and
`Pivox.macOS/UI/ThemeFonts.cs` (returns `NSFont`). WinUI needs the
parallel:

```
Pivox.WinUI/UI/ThemeBrushes.cs   // ThemeColor → Brush (or Color)
Pivox.WinUI/UI/ThemeFonts.cs     // ThemeFont  → FontFamily + size
```

XAML pages bind via `{x:Bind}` to static members on those realizers,
OR reference `{ThemeResource SystemAccentColorBrush}` etc. directly
for system-level tokens.

### System accent

Windows has a single system-wide accent color — no Multicolor
fallback, no `AccentColor.colorset` asset, no `NSAccentColorName`
declaration. Bind directly to `{ThemeResource SystemAccentColor}`
(or `SystemAccentColorBrush`) and you're done. `ThemeColor.Accent`
on the WinUI realizer should map to that.

### Light / dark

WinUI handles light/dark from the system preference automatically
when you use `{ThemeResource ...}` brushes throughout. Don't bake C#
`Color` constants — let the theme dictionary resolve them.

For the root `Window`, leave `RequestedTheme = ElementTheme.Default`
(follows system). Don't hardcode `Light` or `Dark` at the window
level — that wins over the system pref and looks wrong when the user
flips appearance.

### System fonts

WinUI uses Segoe UI Variable on Win 11+ by default. The
`ThemeFont` realizer should return `FontFamily` + size pairs that
mirror the macOS sizing:

| Token | macOS (`NSFont`) | WinUI guidance |
|---|---|---|
| `BrandTitle` | 28pt, Semibold | 28pt or `{ThemeResource CaptionTextBlockFontSize}`-adjacent large title |
| `Title` | system + 2, Semibold | 17pt Semibold (`ThemeResource SubtitleTextBlockFontSize`) |
| `Body` | `SystemFontSize` | `{ThemeResource ControlContentThemeFontSize}` (14pt) |
| `BodySmall` | `SmallSystemFontSize` | 12pt (`{ThemeResource CaptionTextBlockFontSize}`) |

Don't hardcode — use the WinUI font theme resources where they map
cleanly so the realizer scales with the system text-size preference.

### Backdrop: Acrylic vs Mica

The existing `Pivox.WinUI` setup uses `DesktopAcrylicBackdrop` on
the window. Stick with that — acrylic gives desktop bleed-through
similar in spirit to the macOS glass treatment.
`MicaBackdrop` / `MicaAltBackdrop` are newer alternatives worth
trying after the basic flow lands; visual call.

### Radial accent backdrop (mirror of macOS `RadialBackdropView`)

The macOS side has `Pivox.macOS/UI/RadialBackdropView.cs` — an
`NSView` that paints two accent-tinted radial gradients (top-leading
at 0.28 alpha radius 520, bottom-trailing at 0.18 alpha radius 620)
to give the floating glass card something to refract. The visual
gain in light mode is large — without it the card reads as a faint
outline.

XAML doesn't have a `RadialGradientBrush` until WinUI 1.2+, but the
Composition API does:
`Microsoft.UI.Composition.CompositionRadialGradientBrush`. Wire it
up via a `Border` with a `CompositionBrush` set via
`ElementCompositionPreview.SetElementChildVisual`, painting two
radials (one at each anchor corner) using the system accent color
at the alpha values above. Reference shape:

```csharp
var compositor = ElementCompositionPreview.GetElementVisual(backdrop)
    .Compositor;
var brush = compositor.CreateRadialGradientBrush();
brush.EllipseCenter = new Vector2(0, 0);  // top-leading
brush.EllipseRadius = new Vector2(520, 520);
brush.ColorStops.Add(compositor.CreateColorGradientStop(
    0.0f, accentAt28Alpha));
brush.ColorStops.Add(compositor.CreateColorGradientStop(
    1.0f, accentAtZeroAlpha));
// Repeat for second radial at bottom-trailing, alpha 0.18, radius 620.
```

If `CompositionRadialGradientBrush` proves fiddly, fall back to a
single linear gradient + Mica/Acrylic for depth — but the radial
mirrors macOS closely, worth attempting first.

## Build the XAML pages

### `Pivox.WinUI/Auth/RegisterPage.xaml`

Mirror `Pivox.macOS/Auth/RegisterViewController.cs`:

- Floating card on the window's acrylic/mica backdrop, with the
  accent-tinted radial gradient backdrop (see Theming section).
- Card shape: acrylic-brushed `Border` with `CornerRadius` matching
  `ThemeMetrics.CardCornerRadius`, `Width = ThemeMetrics.AuthCardWidth`.
- Header: "Pivox" (`ThemeFont.BrandTitle`) + "Create your account"
  (`ThemeFont.Body` in `SystemColors.SecondaryText`).
- Fields: Email, Display name, Password, Confirm password.
- Primary button: "Create Account" (uses
  `RegisterViewModel.CreateAccountAsync`).
- Error label (pre-allocated height so layout doesn't shift).
- Footer: "Already have an account? Sign in" — calls
  `router.Pop()`.

Default button is "Create Account" — Enter in any field activates.

### `Pivox.WinUI/Auth/LoginPage.xaml` updates

Add to the existing page:

- **Step 1 (email-only):** primary button is "Continue". On click,
  call `LoginViewModel.SubmitEmailStepAsync(ct)`.
  - If it returns true (SSO path completed inline), router handled
    the transition.
  - If it returns false, `DidResolveAsPassword` is now true → reveal
    the password field, change primary button to "Sign In".
- **Step 2 (password revealed):** primary button is "Sign In", calls
  `LoginViewModel.SignInWithEmailAsync`. Password field has
  `Visibility = Collapsed` until `DidResolveAsPassword` flips true;
  bind via `x:Bind` with an inverse-bool-to-visibility converter.
- **Forgot password link** below the password field (step 2 only).
  Calls `auth.SendPasswordResetAsync(email)`.
- **Remember-me checkbox** above the primary button (both steps).
  Binds to `RememberedEmail` (see below).
- **GitHub button** below the Google button. Same shape; uses
  `LoginViewModel.SignInWithGitHubAsync`.
- **"Don't have an account? Create one" footer** below the social
  section. Calls `router.Push(new AppRoute.Register())`.

Edit-email-after-step-2 invalidation: when the email field's text
changes, `LoginViewModel.Email` setter is called → the VM resets
`DidResolveAsPassword = false` and clears `Password` (already
implemented in the shared VM). The password field's visibility
binding updates automatically.

### Default button handling

WinUI's XAML doesn't have an exact `KeyEquivalent = "\r"` equivalent.
The pattern is `KeyboardAccelerator` for the primary button:

```xml
<Button x:Name="SignInButton" Content="Sign In" Click="SignInButton_Click">
    <Button.KeyboardAccelerators>
        <KeyboardAccelerator Key="Enter"/>
    </Button.KeyboardAccelerators>
</Button>
```

Apply to both the Login primary button and the Register primary
button. Same effect: Enter from any field activates the default.

## Build `RememberedEmail`

Per-platform — not shared. WinUI uses
`Windows.Storage.ApplicationData.Current.LocalSettings`:

```csharp
namespace Pivox.Auth;

public sealed class RememberedEmail
{
    private const string Key = "remembered_email";
    private readonly ApplicationDataContainer _settings
        = ApplicationData.Current.LocalSettings;

    public string? Get() => _settings.Values[Key] as string;
    public void Set(string? email)
    {
        if (string.IsNullOrEmpty(email)) _settings.Values.Remove(Key);
        else _settings.Values[Key] = email;
    }
}
```

Lives at `Pivox.WinUI/Auth/RememberedEmail.cs`. Wire it into the
LoginPage code-behind (read on appear, write on successful sign-in
when the checkbox is on).

## Build the routing observer

`App.OnLaunched` already subscribes to `_router.CurrentChanged`. Add
a case for `AppRoute.Register`:

```csharp
private void OnRouteChanged(object? sender, AppRoute route)
{
    Window content = route switch
    {
        AppRoute.Login    => new LoginWindow(_auth, _router),
        AppRoute.Register => new RegisterWindow(_auth, _router),
        AppRoute.Shell    => new MainWindow(_auth, _pivox),
        _ => throw new InvalidOperationException($"No window for {route}"),
    };
    // Activate `content`, close the previous window (do NOT Dispose —
    // see CLAUDE.md Rule 13 — let GC handle the managed peer).
}
```

The auth flow is the first place `router.Push` and `router.Pop` get
exercised — verify Login → Push Register → Pop returns to Login
without crashing or stacking windows.

## Assets to copy

Glyphs and visual assets come from the native/ asset catalog —
that's the canonical source for both stacks (the dotnet macOS app
already reuses these). For WinUI:

- `GitHubLogo` from
  `native/platform/macos/Assets.xcassets/GitHubLogo.imageset/`
- `GoogleLogo` from
  `native/platform/macos/Assets.xcassets/GoogleLogo.imageset/`
  (already in `Pivox.macOS/Assets.xcassets/` — same source)

Both ship as SVG with preserves-vector-representation. WinAppSDK
1.7+ accepts SVG via `Image` source. Older SDKs need PNG at multiple
scales — run the SVG through a converter.

Sizing in the XAML: explicit `Width="16" Height="16"` on the
`Image`. The macOS side learned the hard way that
preserves-vector-representation SVGs render at their viewBox
(GitHub's is 1024×1024) unless clamped — same caveat applies in
WinUI even though the bug surface looks different.

If the WinUI side later needs additional glyphs / accent assets,
prefer copying from `native/platform/macos/Assets.xcassets/`
(or `native/platform/windows/...` if Windows-side native art exists)
over sourcing them fresh.

## Smoke checklist

- [ ] Sign up with email/password creates an account, signs in,
      lands in the Shell.
- [ ] Send password reset from step 2 succeeds (check email inbox).
- [ ] Sign in with GitHub opens the WebView2 popup, completes,
      lands in the Shell.
- [ ] Email-first two-step (non-SSO): enter email → "Continue" →
      password reveals → submit completes.
- [ ] Email-first two-step (SSO): enter SSO email → broker popup
      appears → completes → lands in the Shell.
- [ ] Forgot password link from step 2 sends the reset email.
- [ ] Remember-me persists email across sign-out + restart.
- [ ] Editing email after step 2 collapses back to step 1.
- [ ] Push Register → Pop back to Login (router back-nav works).
- [ ] Continue/Sign In disabled state is legible in BOTH light and
      dark mode. (On macOS Tahoe `TintProminence.Primary` desaturated
      the disabled fill/label too aggressively; macOS workaround was
      `AttributedTitle` with explicit foreground — may not be needed
      on WinUI's `AccentButtonStyle` but verify.)
- [ ] Light mode + dark mode visual sweep — system theme flip lands
      sane in all auth views (radial backdrop reads, card edges
      visible, text contrast holds).
- [ ] System accent flip — change the Windows accent color from blue
      to something else; the radial backdrop, primary button, and
      link text should re-tint live.

## Reference files

- `dotnet/Pivox.macOS/Auth/MacOsAuthService.cs` — the 5 new
  `IAuthService` method implementations against Firebase Cocoa.
  Same shape applies to Firebase C++ on Windows.
- `dotnet/Pivox.macOS/Auth/LoginViewController.cs` — two-step flow
  reference, including the SSO branch + the email-edit-resets-step
  invalidation, default-button wiring, and post-submit
  remember-me persistence shape.
- `dotnet/Pivox.macOS/Auth/RegisterViewController.cs` — Register
  form layout + bindings reference.
- `dotnet/Pivox.macOS/Auth/RememberedEmail.cs` — `NSUserDefaults`
  variant; WinUI swaps to `ApplicationData.LocalSettings`.
- `dotnet/Pivox.macOS/UI/ThemeColors.cs` + `ThemeFonts.cs` —
  per-platform realizers; the WinUI versions return `Brush` /
  `FontFamily+size` instead.
- `dotnet/Pivox.macOS/UI/RadialBackdropView.cs` — radial-gradient
  backdrop reference; WinUI uses `CompositionRadialGradientBrush`.
- `dotnet/Pivox.macOS/UI/AuthPrimaryButton.cs` — primary button
  shape + `AttributedTitle` workaround for macOS Tahoe
  disabled-state legibility (may or may not be needed on WinUI's
  `AccentButtonStyle` — verify, see smoke checklist).
- `dotnet/Pivox.Shared/Auth/LoginViewModel.cs` — two-step state
  machine + OAuth orchestration (`DidResolveAsPassword`,
  `IsOAuthInProgress`).
- `dotnet/Pivox.Shared/Auth/RegisterViewModel.cs` — Register state.
- `dotnet/Pivox.Shared/UI/Theme.cs` — `ThemeColor`, `ThemeFont`,
  `ThemeMetrics` enums + constants shared across platforms.
- `dotnet/CLAUDE.md` — Rules 12–18 (threading, lifecycle, layout,
  glass, controller-ownership, accent-color). Most apply identically
  to WinUI; Rule 16 (NSGlassEffectView) translates to Mica/Acrylic
  + composition; Rule 18 (NSAccentColorName) is macOS-only.
