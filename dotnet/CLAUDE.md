# dotnet — agent conventions

Scope: `dotnet/` — cross-platform .NET implementation of Pivox's
clients. Sibling to `native/` (Swift macOS + Windows C++/WinRT plans);
parallel path, deliberately. The two stacks are not expected to be
maintained simultaneously long-term — see the strategic spike commits
that established viability for `dotnet/`.

Stack:

- **macOS**: `net10.0-macos` (Microsoft.macOS.dll bindings + Apple
  Cocoa runtime). All-code AppKit, no storyboards. Firebase Cocoa SDK
  bound via objective-sharpie.
- **Windows** (planned, not yet implemented): WinUI 3 + `net10.0-windows`.
  Firebase C++ SDK wrapped in a C++/WinRT component, consumed from C#
  via WinRT projection (no P/Invoke).
- **Shared**: plain `net10.0` class libraries — auth contracts, gRPC
  clients, view models, domain types. The same DLLs run on both
  platforms.

Defer to the Go backend's `CLAUDE.md` for cross-cutting rules
(component naming, code-quality bar, TDD) that aren't restated here.
Defer to `../native/CLAUDE.md` for the Swift-stack equivalent path.

## Status — what's validated, what's not

So a fresh session doesn't redo work that's already done. The
strategic spike that established viability is captured in git
history (search for "Spike: .NET-for-macOS" and the follow-on
commits) but the operating outcomes are:

**macOS — proven end-to-end:**

- All-code AppKit (no storyboard, no xib): NSWindow + NSSplitViewController
  + main menu, all in C#.
- Firebase Cocoa SDK bound via objective-sharpie. Sign-in
  (email/password + Google OAuth via ASWebAuthenticationSession +
  PKCE) returns real Firebase JWTs and persists sessions to the
  macOS Data Protection Keychain.
- gRPC client (`Pivox.Client`) generated from `../api/proto/`,
  Bearer-token attached via `CallCredentials`, calls pivox-cloud
  successfully. 31 typed service clients available.
- NativeAOT publish: ~12 MB total bundle, native ARM64 binary, no
  Mono runtime, fast cold-start, signed with the SwiftUI app's
  Apple Development cert + provisioning profile.

**Cross-platform contracts ready:**

- `Pivox.Shared` (auth contracts, JWT decoder, CloudConfig) and
  `Pivox.Client` (gRPC clients) are plain `net10.0` libraries.
  Both flagged `<IsAotCompatible>true</IsAotCompatible>`. The
  Windows side consumes them as-is.

**Windows — not yet implemented:**

- See `winui.md` in this directory for the implementation prompt.
- Strategic choice: Firebase C++ SDK wrapped in a C++/WinRT
  component, projected to C#. **Not P/Invoke.** `WindowsAuthService :
  IAuthService` lives in Pivox.WinUI and consumes the WinRT
  projection.

## Operating rules

These are the rules from working with this user. They are not
suggestions; treating them as suggestions burns the user's time and
gets you re-corrected. Internalize all of them before touching code.

### Grounding

- **No guessing AppKit / WinUI / .NET API names.** Always look them
  up before writing — `microsoft_docs_search` MCP for C# signatures,
  Apple docs for behavior, grep `Microsoft.macOS.dll` for binding
  names. One doc lookup is ~3s; one compile-error iteration is
  ~30-60s. The lookup wins economically every time.
- **Swift / ObjC samples are first-class research material.** Apple
  ships them; we translate them to C# via the rename rules below.
  The C# sample-code corpus is small and irrelevant — translate
  from the big corpus.
- **Verify generated code before writing consumers of it.** When
  consuming protoc / sharpie / bgen output, open the generated file
  and read the actual namespace + type names. Namespace shadowing,
  inner-class names, and binding generator quirks bite you only
  when you guess at the shape.
- **No third-party packages of dubious provenance for fundamental
  types.** Generate from source we own rather than trust an unofficial
  community port. Example: `buf/validate/validate.proto` is in
  `api/proto/buf/` already; generate locally rather than depend on
  `ProtoValidate` (third-party, unsigned port). For genuinely
  official packages (Google.Api.CommonProtos, Google.LongRunning),
  reference rather than regenerate.

### No time estimates

The user has explicitly banned them. "Tasks in AI take minutes vs
days" — duration estimates are noise. Communicate work in:

- **Order of operations** (this depends on that)
- **Concrete deliverables** (what gets created/modified)
- **Validation outcomes** (what passes/fails)

Never "this should take ~30 minutes" or "~2 hours." Just say what's
being done and what's done after it.

### Phase boundaries

- **Surface phase completion + audit recommendation + commit-ready
  signal** at every phase boundary. Don't wait to be asked.
- **Auto-spawn code-reviewer at phase boundaries** for substantive
  diffs (auth, signing, security, schema, foundational layers).
  Don't ask first.
- **Commit only when explicitly told.** "Wait for the user to say
  commit." Don't pre-emptively commit good-feeling work.

### Honest reads

- **Push back on flawed approaches.** Don't rubber-stamp. The user
  wants honest engineering reads, not agreement.
- **When the user pushes back, update the analysis honestly.** If
  they correct you with ground truth, the right response is to
  incorporate it and recompute — not to apologize-and-restate. A
  ground-truth correction (e.g. "the current SwiftUI plan has been
  a fucking PITA") is data; treat it as such.
- **Acknowledge biases.** When you've just built something, your
  read is biased toward it. Say so out loud when relevant.
- **Don't pad summaries.** Bullet lists describing every step of a
  small commit make 1-2 commit items look like multi-week work.
  Match the shape of the work.

### Git

- **Stage explicit paths** (`git add path/...`). Never `git add -A`
  or `git add .` — those can drag in conflict markers, secrets, or
  unintended files.
- **Never `git clean -fd`** without explicit confirmation. Has wiped
  the user's `.envrc` files in the past (unrecoverable, no backups).
- **Never `rm -rf bin obj`** while Rider is open. Rider's IB sync
  watcher has eaten tracked files before. Use `dotnet clean`.
- **Pre-prod freedom applies.** Drop proto fields outright (no
  `reserved`), edit init migration directly, etc. — but DESTRUCTIVE
  ops (force push, hard reset, db drop) need confirmation in the
  current conversation.

### Surfacing semantic choices

- **At semantic forks** (codes, wire messages, error shapes, library
  choice, framework choice), pause and offer options. Don't bury
  the choice in the diff.
- **Stop when stuck.** If the approach hits a wall, surface and
  ask. No silent pivots on frameworks/packages/strategy — those
  are user decisions.

### When code is wrong

- **Pushback on a shape = fix the shape**, not justify it in a
  commit message. If the user calls a name "hacky" or a method
  "kinda hacky," the work is to rename / restructure, not to
  defend.
- **"All X" / "every" / "no shortcuts" = honor literally.** Surface
  constraints; never silently triage.

## Source layout

```
dotnet/
  Pivox.slnx              solution file (XML format, .NET 9+)
  Pivox.Shared/           cross-platform contracts. No platform deps.
                          - Auth/IAuthService.cs   the auth contract
                          - Auth/AuthSession.cs    record carrying
                                                   IdToken + FirebaseIdentity
                                                   + convenience accessors
                                                   (PivoxUserId, Email, etc.)
                                                   + Principal getter for
                                                   ClaimsPrincipal-shaped
                                                   consumers.
                          - Auth/FirebaseIdentity.cs
                                                   ClaimsIdentity subclass
                                                   built from a JWT via
                                                   Microsoft.IdentityModel.
                                                   JsonWebTokens. Typed
                                                   accessors for the claims
                                                   we read. Both platforms
                                                   converge on constructing
                                                   `new FirebaseIdentity(jwt)`.
                          - CloudConfig.cs         backend URL + TLS,
                                                   reads PIVOX_GRPC_HOST
                                                   etc. Mirrors the
                                                   Swift CloudConfig.swift.
  Pivox.Client/           gRPC clients for pivox-cloud. Generates C#
                          from ../api/proto/pivox/**/*.proto + buf/.
                          - PivoxClient.cs         single client factory;
                                                   typed service accessors
                                                   (Iam, Organizations,
                                                   Spaces, …) via the
                                                   global:: qualifier.
                          - Auth/AuthCallCredentials.cs
                                                   gRPC CallCredentials
                                                   that attaches Bearer
                                                   tokens, async-correctly.
                                                   NOT an Interceptor —
                                                   see "Bearer-token
                                                   attachment" below.
  Pivox.Firebase.Bindings/      macOS-only sharpie binding of the Firebase
                          Cocoa SDK (FirebaseAuth + FirebaseCore + 9
                          embedded-only transitive xcframeworks).
                          Not consumed by Windows code.
  Pivox.macOS/               macOS app. All-code NSWindow root, no
                          storyboard. Implements IAuthService against
                          the Firebase binding. Signs with the local
                          Apple Development cert + provisioning
                          profile from the SwiftUI Pivox build.
  Pivox.WinUI/       (future) WinUI 3 app. Will implement
                          IAuthService against a C++/WinRT Firebase
                          component.
  scripts/                build-time setup (fetch-firebase-sdk.sh).
```

### Dependency-direction rule

Code may depend **downward**, never **upward**, never **sideways**.

- `Pivox.Shared` depends on nothing in this directory.
- `Pivox.Client` depends on `Pivox.Shared`.
- `Pivox.Firebase.Bindings` depends on nothing.
- `Pivox.macOS` may depend on all of the above.
- `Pivox.WinUI` (future) depends on `Pivox.Shared` + `Pivox.Client`
  + a Windows-specific binding/projection assembly. **Not** on
  `Pivox.Firebase.Bindings` (macOS-only) or `Pivox.macOS` (macOS-only).

**No platform-specific types in `Pivox.Shared` or `Pivox.Client`.**
That includes Firebase types, AppKit types, WinUI types, WinRT types.
The shared layer sees POCO records and the contracts it defines.
Anything platform-specific is an `IAuthService.cs`-style interface
implemented separately per platform.

### What goes where — decision tree

1. **Cross-platform code (logic, state, contracts, gRPC, view models)**
   → `Pivox.Shared` or `Pivox.Client`.
2. **macOS implementation of a cross-platform interface, or a macOS-only
   UI component** → `Pivox.macOS`.
3. **Windows implementation of a cross-platform interface, or a
   WinUI-only UI component** → `Pivox.WinUI`.
4. **C# binding of a third-party Cocoa SDK** → `Pivox.Firebase.Bindings`
   (or a sibling binding project per SDK).

## Build

```sh
# First-time setup: fetch Firebase xcframeworks (~111 MB, gitignored).
scripts/fetch-firebase-sdk.sh

# Debug build (incremental, fast — no AOT).
dotnet build Pivox.slnx

# Release publish (NativeAOT, code-signed, provisioning-profile
# embedded). Produces a static native binary, no Mono runtime.
dotnet publish Pivox.macOS/Pivox.macOS.csproj -c Release -r osx-arm64

# Launch (Debug). Open the .app bundle.
open Pivox.macOS/bin/Debug/net10.0-macos/osx-arm64/Pivox.app
```

Provisioning profile setup (one-time, per developer machine): copy
the SwiftUI Pivox app's Xcode-generated profile into `Pivox.macOS/`:

```sh
cp ../native/build-xcode/Debug/Pivox.app/Contents/embedded.provisionprofile \
   Pivox.macOS/embedded.provisionprofile
```

Gitignored. Refresh when the upstream Xcode build refreshes it.

## Rule 0: AppKit is AppKit. Language doesn't change it.

Swift, Objective-C, and C# all dispatch to the same `objc_msgSend`.
The runtime is the same. The cost difference between Swift and C# is
*not* in the API surface — it's in naming, documentation discoverability,
and tooling. Solvable with discipline.

## Rule 1: API discovery — MS docs first, Apple docs second

Apple's docs describe **behavior** (what does `NSSplitViewItem` do,
when is `viewDidLoad` called). They use Swift/Objective-C names.

Microsoft's docs describe the **C# surface** (what's the C# name, what
overloads exist). The binding generator renames things; Apple's docs
don't reflect those renames.

**Workflow:**

1. Know what you want to do in AppKit terms (Apple docs / WWDC).
2. Translate to C# signature via the `microsoft_docs_search` MCP, or
   `learn.microsoft.com/en-us/dotnet/api/appkit.<typename>?view=net-macos-26.0-10.0`.
3. Only fall back to grepping `Microsoft.macOS.dll` if the docs miss.

If you find yourself guessing names, **stop**. One search call beats
five compile errors.

### Binding rename rules

Deterministic transformations the binding generator applies:

| Swift / ObjC | C# |
|---|---|
| `windowBackgroundColor: NSColor` | `WindowBackground` (drops "Color" — return type already says it) |
| `controlAccentColor` | `ControlAccent` |
| `secondaryLabelColor` | `SecondaryLabel` |
| `+ sidebarWithViewController:` | static `CreateSidebar(NSViewController)` |
| `- initWithFrame:` | `new T(CGRect frame)` (positional, label dropped) |
| `+ classWithCoder:` | `new T(NSCoder coder)` |
| `setX:`, `getX` accessors | `X` property |
| `xxxForKey:` / `xxxWithName:` factory | static `CreateXxx(...)` or `FromXxx(...)` |
| ObjC `BOOL` | C# `bool` |
| ObjC `NSInteger` | C# `nint` |
| Methods with `completion:` block | `Task`-returning `XxxAsync` twin (when sharpie emits `[Async]`) |
| Methods with `error:**NSError` out-param | C# throws `NSErrorException` |

**Heuristic**: when an ObjC property name redundantly repeats its
return type, the C# binding drops the suffix.

## Rule 2: Swift / ObjC sample code is fully usable

Apple's Swift sample for `NSCollectionView` applies to C# — translate
via Rule 1. The "sparse C# sample code" complaint is a non-issue once
you can translate fluently. For complex flows, read the Swift WWDC
sample to understand the shape, then write the C# equivalent.

## Rule 3: async/await — use Task overloads where available

The binding generator auto-emits `Task`-returning twins for some
`completion:` block methods. Prefer the async overload when emitted;
when not, wrap manually with `TaskCompletionSource` (the
`MacOsAuthService.SignInWithEmailAsync` shape is the standard).

```csharp
// When sharpie didn't emit [Async]:
var tcs = new TaskCompletionSource<(FIRAuthDataResult?, NSError?)>();
FIRAuth.Auth.SignInWithEmail(email, password, (r, e) => tcs.SetResult((r, e)));
var (result, error) = await tcs.Task;
```

## Rule 4: NSViewController code-only construction

Storyboard-instantiated VCs have `protected Ctor(NativeHandle handle)`.
Code-instantiated VCs need a constructor that calls
`base((string?)null, null)` (explicit "no nib") and a `LoadView`
override:

```csharp
[Register("MyViewController")]
public sealed class MyViewController : NSViewController
{
    public MyViewController() : base((string?)null, null) { }

    public override void LoadView()
    {
        View = new NSView(new CGRect(0, 0, 480, 600));
    }

    public override void ViewDidLoad()
    {
        base.ViewDidLoad();
        // Build subview hierarchy here.
    }
}
```

Without `base((string?)null, null)`, the VC tries to load a nib named
after the class.

## Rule 5: AppDelegate must be wired explicitly when no storyboard

Without a `Main.storyboard` or `MainMenu.nib`, `NSApplicationMain` has
no mechanism to discover the `AppDelegate`. Wire it in `Main.cs`:

```csharp
NSApplication.Init();
NSApplication.SharedApplication.Delegate = new AppDelegate();
NSApplication.Main(args);
```

Assignment must happen **before** `NSApplication.Main(args)`.

## Rule 6: storyboard files auto-inject NSMainStoryboardFile

The .NET-for-macOS SDK treats any `*.storyboard` in the project as an
`InterfaceDefinition` build item and **automatically writes
`NSMainStoryboardFile` into the bundle's Info.plist**. Stripping the
key from source `Info.plist` doesn't help.

To run all-code: delete the storyboard, OR exclude via csproj:

```xml
<InterfaceDefinition Remove="Main.storyboard" />
```

Verify after build:

```sh
/usr/libexec/PlistBuddy -c "Print NSMainStoryboardFile" \
  Pivox.macOS/bin/Debug/net10.0-macos/osx-arm64/Pivox.app/Contents/Info.plist
# Expected: 'NSMainStoryboardFile' Does Not Exist
```

## Rule 7: Main menu in code

No storyboard means building NSMenu in `AppDelegate.DidFinishLaunching`.
Minimum-viable: Application menu (Quit) + Edit menu (Cut/Copy/Paste/
Select All so NSTextField shortcuts work) + Window menu.
`Pivox.macOS/AppDelegate.cs`'s `BuildMainMenu()` is the reference shape.

Selectors are standard Cocoa names dispatched up the responder chain
(`terminate:`, `cut:`, `copy:`, `paste:`, `selectAll:`,
`orderFrontStandardAboutPanel:`, `performMiniaturize:`, `performZoom:`,
`arrangeInFront:`). `NSApplication` or the active control handles them.

## Rule 8: visual debugging — AX Inspector, NOT view debugger

**Xcode's view hierarchy debugger does not work with .NET-for-macOS.**
Attach succeeds, but the view hierarchy snapshot request times out or
crashes the process. Reproducible.

Alternatives:

| Tool | Works? | Use for |
|---|---|---|
| Xcode View Debugger | ❌ | — (don't waste time) |
| Accessibility Inspector | ✅ | Live tree of all views with frames, click-to-highlight, point inspection |
| `view.RecursiveDescription()` | ✅ | Text tree dump with frames, hidden flags, layer info |
| Diagnostic background colors (`view.WantsLayer = true; view.Layer!.BackgroundColor = NSColor.SystemPink.CGColor`) | ✅ | Catches zero-size and off-screen views instantly |
| Auto Layout constraint conflict logs | ✅ | Auto-prints to console; no setup |

## Rule 9: Debug attach entitlement

Xcode and `lldb` need `com.apple.security.get-task-allow` to call
`task_for_pid`. .NET-for-macOS doesn't add this automatically. Set in
`Entitlements.plist` for Debug:

```xml
<key>com.apple.security.get-task-allow</key>
<true/>
```

Strip for production / store distribution.

## Rule 9.5: Rider's MT7139 keychain-access-groups warning is false

Rider's design-time analyzer raises MT7139 ("The app requests the
entitlement 'keychain-access-groups', but no provisioning profile
has been specified") on rebuild. The warning is **false** — our
setup is valid:

- `<CodesignProvision>Mac Team Provisioning Profile: app.pivox.native</CodesignProvision>`
  is set in `Pivox.macOS.csproj`.
- That exact profile is installed in
  `~/Library/Developer/Xcode/UserData/Provisioning Profiles/`.
- The profile's `keychain-access-groups` is `FAENDBN66M.*` (wildcard
  for the team prefix), which permits our requested
  `FAENDBN66M.app.pivox.native`.
- `dotnet build` from CLI does NOT raise MT7139 — it correctly
  detects and uses the profile.

The warning appears to be a Rider-side analyzer bug: it runs
entitlement validation at a phase that doesn't see `CodesignProvision`,
or doesn't understand wildcard matching in profile entitlements.
Don't chase it — Firebase Auth keychain persistence works correctly
at runtime, the signed bundle is valid, and the published Release
build has zero codesign issues.

## Rule 10: never `rm -rf obj` if Rider is open

Rider's IB shim-sync watcher interprets directory removal as source
deletion and propagates it back. Has eaten files before. Use
`dotnet clean` to clear build state — never `rm -rf bin obj`.

## Rule 11: AOT compatibility on shared class libraries

`Pivox.Shared` and `Pivox.Client` set `<IsAotCompatible>true</IsAotCompatible>`
so their code is analyzed under trim/AOT rules at build time. New
shared libs should do the same. Reflection-heavy patterns warn at
build time instead of breaking host-app publish.

Class libraries do **not** set `<PublishAot>` — that's executable-only.
`Pivox.macOS.csproj` sets `<PublishAot>true</PublishAot>` (unconditional;
AOT only triggers at publish time, not build).

## Rule 12: shared services raise events on the UI thread

Any state holder in `Pivox.Shared` that fires events the UI subscribes
to (`IAuthService.CurrentChanged`, `AppRouter.CurrentChanged`,
view-model `INotifyPropertyChanged`, future feature services) MUST
ensure those events deliver on the platform's UI thread, regardless of
which thread mutated the state. Subscribers should never have to
marshal in their handler — the boundary is the service's
responsibility.

**Two enforcement points, both should hold long-term:**

1. **Platform-specific `IAuthService` impls** marshal at their own
   boundary. `MacOsAuthService` gets this nearly free (FIRAuth
   callbacks fire on the main queue); `WindowsAuthService` must
   explicitly dispatch via the WinUI 3 `DispatcherQueue` before
   raising `CurrentChanged` — the C++/WinRT bridge's
   `AuthStateChanged` event arrives on Firebase's internal thread.
2. **Shared state services** (router, view-models) capture a
   `SynchronizationContext` at construction (the UI thread's
   context, present on both platforms by default) and `Post` events
   through it. `AppRouter` is the reference shape.

**Why both:** defense-in-depth. Platform impls own their own
threading, but the shared layer protects against forgotten
marshaling at any layer above. Latent bugs surface as "callbacks
fire on background thread on platform X only" — exactly the kind of
heisenbug TDD won't catch.

**Implementation pattern (shared layer):**

```csharp
public sealed class FooService
{
    private readonly SynchronizationContext _uiContext;

    public FooService()
    {
        _uiContext = SynchronizationContext.Current
            ?? throw new InvalidOperationException(
                "FooService must be constructed on the UI thread.");
    }

    public event EventHandler<TArgs>? SomethingChanged;

    public void MutateFromAnywhere(...)
    {
        if (SynchronizationContext.Current == _uiContext)
        {
            // Fast path: already on UI thread → synchronous mutation
            DoMutate();
            SomethingChanged?.Invoke(this, args);
        }
        else
        {
            _uiContext.Post(_ =>
            {
                DoMutate();
                SomethingChanged?.Invoke(this, args);
            }, null);
        }
    }
}
```

**Consequence — eventual consistency on background calls.** Mutations
posted from a background thread are not visible to that thread
immediately after the call returns. State reads from the UI thread
are always current. This is the standard async-router shape; the
alternative (lock-and-mutate-sync, post-the-event) buys nothing and
adds a lock. Document it on each service that uses the pattern.

## Rule 13: never manually `Dispose()` AppKit NS* peers

AppKit owns its native objects; the managed `NSObject` subclass
(`NSWindowController`, `NSWindow`, `NSViewController`, etc.) is a
peer pointing at the native side. Calling `.Dispose()` on the peer
disposes its handle — but AppKit may still hold the native object
(deferred close animations, autorelease pools, `ReleasedWhenClosed`
nuances). The next time AppKit messages the peer, it dereferences a
dead handle. Use-after-free shaped bug, often invisible until a
later release pass.

**Pattern for window teardown:**

```csharp
// CORRECT — let AppKit run its release path; managed peer GC's later.
previous?.Close();

// WRONG — disposes the managed peer while AppKit may still hold ref.
previous?.Close();
previous?.Dispose();
```

If you need deterministic cleanup of *managed-side* state
(unhook events, dispose injected services), do that explicitly on
the controller's content (e.g. before `Close()`, walk to the
`ContentViewController` and unsubscribe its bindings). Don't
`Dispose()` the AppKit peer itself.

This rule applies to every type derived from `NSObject` that AppKit
constructs lifetimes for: `NSWindow`, `NSWindowController`,
`NSViewController`, `NSView`, `NSMenuItem`, `NSTextField`, etc.
Manual `Dispose()` is only correct for managed wrappers we own end
to end (e.g. our own classes implementing `IDisposable` with no
AppKit handle).

## Rule 14: pick one path between Firebase listener and explicit `SetCurrent`

`FIRAuth.AddAuthStateDidChangeListener` fires for every sign-in,
sign-out, and token rotation. If your `IAuthService` impl also
calls `SetCurrent(session)` explicitly inside `SignInAsync` /
`SignOutAsync`, **`CurrentChanged` fires twice** — once from the
explicit path, once from the listener. Every subscriber runs its
handler twice. For `AppRouter.ReplaceRoot`, that means windows
build twice.

Pick one:

- **Listener-only** (recommended): explicit auth methods *just*
  call FIRAuth; the listener observes the resulting state change
  and calls `SetCurrent` exactly once.
- **Explicit-only**: don't install the listener, do everything via
  the explicit paths. Loses the "restore persisted session on
  launch" signal — usually not what you want on macOS.

If you do keep both (because the listener also catches passive
token refreshes), **dedupe in `SetCurrent`**: compare incoming
session to current by identity-relevant field (Firebase JWT
`IdToken` is canonical — same JWT means same session). Suppress
the duplicate.

This applies to the WinUI side too: `FirebaseAuthBridge`'s
`AuthStateChanged` event fires for the same triggers; the C#
adapter's `WindowsAuthService` should dedupe in `SetCurrent` the
same way.

## Rule 15: `NSStackView` has no built-in "Fill" cross-axis alignment

NSStackView's `Alignment` property controls cross-axis positioning
(Leading/Trailing/CenterX/CenterY etc.) — none of them stretch
children to fill the cross axis the way SwiftUI's
`HStackAlignment.center` + `frame(maxWidth: .infinity)` does, or
the way UIStackView's `.fill` distribution does.

To make children fill cross-axis in NSStackView:

```csharp
// Pin each child's width to the stack's content width (stack width
// minus EdgeInsets on both sides). EdgeInsets is the single source
// of truth for padding — derive the constant from it, don't hardcode
// the same magic number twice.
var inset = -2 * (float)stack.EdgeInsets.Left;
NSLayoutConstraint.ActivateConstraints(new[]
{
    child1.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor, 1, inset),
    child2.WidthAnchor.ConstraintEqualTo(stack.WidthAnchor, 1, inset),
});
```

**Anti-pattern:** combining `EdgeInsets = N` AND hardcoded
`WidthAnchor.ConstraintEqualTo(stack.WidthAnchor, 1, -2N)` — both
do the same math; if `N` changes, only one updates. Pick: derive
the constraint constant from `EdgeInsets`, OR drop EdgeInsets and
let the constraints carry the inset. See
`LoginViewController.BuildCard()` for the canonical shape.

## Rule 16: Liquid Glass scope on macOS 26

`NSGlassEffectView` is the proper macOS 26 Liquid Glass primitive.
Per WWDC 2025 session 310 ("Build an AppKit app with the new
design"): **limit Liquid Glass to top-level UI elements that float
above content** — auth cards, inline editing controls, toolbar
glass. Not for general window backgrounds.

Practical layering:

| Layer | Use |
|---|---|
| Window content | Plain `NSView` with `NSColor.WindowBackground` (appearance-aware, solid). Don't put `NSVisualEffectView` here — it competes with the glass card for the same "translucent surface" role and the glass sampling gets confused ("glass can't sample other glass"). |
| Floating cards / panels | `NSGlassEffectView` with `ContentView = yourLayout` and `CornerRadius = ThemeMetrics.CardCornerRadius` |
| Sidebar / toolbar | `NSSplitViewController.CreateSidebar` and `NSToolbar` auto-glass per the new design system. Don't apply additional `NSVisualEffectView` to a sidebar — the WWDC session calls this out as a removal-required pattern. |

`NSGlassEffectView` properties: `ContentView` (required — sets the
view to appear on glass; AppKit ties geometry via Auto Layout
automatically), `CornerRadius`, `TintColor`. See `LoginViewController.BuildCard()`.

For grouping multiple glass elements in close proximity, use
`NSGlassEffectContainerView` so they share sampling regions and
join/separate fluidly (the WWDC session calls this out for visual
correctness — light refraction breaks when two `NSGlassEffectView`
instances sample independently).

## Rule 17: set window/VC ownership via the `NSWindowController`, not the window

The AppKit ownership chain is three-way:
`WindowController ↔ Window ↔ ContentViewController`. Both
`NSWindow.ContentViewController` and
`NSWindowController.ContentViewController` exist; they do different
things.

| Setter | What it does |
|---|---|
| `window.ContentViewController = vc` | Sets `contentView = vc.View`. Window-side only. |
| `windowController.ContentViewController = vc` | Same as above, PLUS wires the controller into the responder chain (controller becomes the next responder above the VC) and ties the VC's lifetime to the controller. |

If you reach through and set `window.ContentViewController` directly
— especially **before** the window is bound to a window controller
(i.e. inside the `static NSWindow Build...()` helper called from
`base(BuildWindow())`) — you get a contentView with broken
responder-chain integration: no controller in the chain, no proper
key-view loop. Empirically on macOS 26, the window may not surface
at all.

**Pattern:**

```csharp
public sealed class MyWindowController : NSWindowController
{
    public MyWindowController(MyContentVC content)
        : base(BuildWindow())                  // window only — no content yet
    {
        ContentViewController = content;       // controller-side setter
    }

    private static NSWindow BuildWindow()
    {
        var window = new NSWindow(/*...*/) { /* chrome */ };
        window.Center();
        // Don't set ContentViewController here — the window has no
        // associated controller yet.
        return window;
    }
}
```

Same principle applies to anything available on `NSWindowController`
(e.g. `Window` — read it via the controller, not by capturing a ref
to the window from `BuildWindow`). The controller is the
construction-time owner; don't bypass it.

## Rule 18: custom accent color needs BOTH the asset AND `NSAccentColorName`

In an Xcode-built app, an `AccentColor.colorset` in `Assets.xcassets`
auto-discovers — Xcode injects the linking metadata at build time so
macOS uses the asset's color when the user's system accent is set to
Multicolor. **`.NET-for-macOS does not do that injection.**` The asset
compiles into `Assets.car` correctly (verifiable via `assetutil
--info`), but macOS's `NSColor.controlAccentColor` returns the system
default (blue) without an explicit declaration.

Required setup on .NET-for-macOS:

1. `Pivox.macOS/Assets.xcassets/AccentColor.colorset/` — defines the
   color (light + dark appearance pairs).
2. `Pivox.macOS/Info.plist`:

   ```xml
   <key>NSAccentColorName</key>
   <string>AccentColor</string>
   ```

Both are load-bearing. Drop either one and accent falls back to
system blue. Verified by removing each in turn and observing the
fallback.

### Info.plist incremental-build staleness

Edits to `Info.plist` source aren't always picked up by `dotnet
build`'s incremental cache — the build can reuse a stale bundled
`Info.plist` and silently miss new keys. If a key you added to the
source isn't showing up at runtime:

```sh
dotnet clean Pivox.macOS/Pivox.macOS.csproj
rm -rf Pivox.macOS/bin/Debug Pivox.macOS/obj
dotnet build Pivox.macOS.slnx
```

Verify with:

```sh
plutil -convert xml1 -o - \
  Pivox.macOS/bin/Debug/net10.0-macos/osx-arm64/Pivox.app/Contents/Info.plist \
  | grep -A 1 <YourKey>
```

Don't trust incremental builds to mirror your `Info.plist` source.

## Rule 19: `NativeReference` Include paths must be absolute via `$(MSBuildThisFileDirectory)`

The .NET-for-macOS SDK's `InstallNameTool` MSBuild task chokes on
relative paths in `<NativeReference Include="...\Pivox.Native\...\libfoo.dylib">`.
Symptom is a non-obvious crash:

```
error : install_name_tool: can't open file: obj/.../libfoo.dylib.tmp (No such file or directory)
error MSB4018: System.IO.FileNotFoundException
    at System.IO.File.Move
    at Xamarin.MacDev.Tasks.InstallNameTool.<...>b__0
```

The task does `Path.GetFullPath(input.ItemSpec)` and then `File.Copy` to
a `.tmp` shadow before invoking `install_name_tool`. When `ItemSpec` is
a `..\..\` relative path, MSBuild's working-directory-at-task-time and
`Path.GetFullPath`'s normalization don't always agree about where the
source actually lives — the `File.Copy` lands at a path that
`install_name_tool` doesn't see, install_name_tool exits with code 1,
the ContinueWith then tries `File.Move` on the (never-created) `.tmp`
and the build fails.

Use:

```xml
<NativeReference Include="$(MSBuildThisFileDirectory)../Pivox.Native/runtimes/osx-arm64/native/libpivox_markdown.dylib">
  <Kind>Dynamic</Kind>
</NativeReference>
```

`$(MSBuildThisFileDirectory)` is the project file's directory with a
trailing slash, resolved at evaluation time. The forward slashes after
it survive the round trip cleanly on macOS. Don't use `..\` Windows
separators (the macios examples on GitHub use them, but those tests
build via a different path that masks the issue).

Once that's right, the SDK lands the dylib at
`Pivox.app/Contents/MonoBundle/` next to the .NET runtime dylibs AND
adds the dylib to the main binary's load commands with
`@executable_path/../../Contents/MonoBundle/libfoo.dylib` — meaning the
dylib loads at process start via dyld, before any P/Invoke. Verify
with:

```sh
otool -L Pivox.macOS/bin/Debug/net10.0-macos/osx-arm64/Pivox.app/Contents/MacOS/Pivox \
  | grep libpivox_
```

Expected output (per dylib):

```
@executable_path/../../Contents/MonoBundle/libpivox_markdown.dylib (compatibility version 0.0.0, current version 0.0.0)
```

## Tooling reference

| Tool | Install | Use for |
|---|---|---|
| `sharpie` | `dotnet tool install -g Sharpie.Bind.Tool` | Generating C# bindings for third-party ObjC `.framework` / `.xcframework` |
| `microsoft_docs_search` MCP | Bundled | Looking up C# binding signatures vs Apple docs |
| Accessibility Inspector | Xcode → Open Developer Tool → Accessibility Inspector | Visual view-tree introspection (works on .NET-for-macOS) |

## Binding third-party Cocoa SDKs — the playbook

Reference implementation: `Pivox.Firebase.Bindings/` (FirebaseAuth +
FirebaseCore + 9 embedded transitive xcframeworks). Sign-in works
through the binding against the real Firebase backend with token
persistence under NativeAOT.

### Mental model — bind vs embed

| Thing | Purpose | Trigger |
|---|---|---|
| **Bind** | Generate C# classes mirroring the framework's ObjC interface so you can `using Foo;` and call methods | Only for frameworks whose APIs you call directly from C# |
| **Embed** | Copy the framework into the app bundle so the dynamic loader can resolve symbols at runtime | Every framework in the dependency closure of what you call |

For FirebaseAuth: 2 frameworks bound (FirebaseAuth + FirebaseCore),
9 embedded as `<NativeReference>` items only. Don't bind everything —
review burden scales linearly with bound surface.

### Sharpie command — the load-bearing flag is `--scope`

Sharpie's default emits bindings for everything it sees (Foundation,
AppKit, etc.). Without `--scope` the output is unworkable.

**The trap**: `--scope` filters by canonical source path. XCFrameworks
resolve through several symlinks (`/tmp` → `/private/tmp`, `Headers`
→ `Versions/A/Headers`). If you pass a `--scope` value that isn't
the fully-resolved real path, **sharpie reports "Bindings generated
successfully" and emits zero files**. Silent failure.

```sh
real_headers=$(cd Path/To/FirebaseAuth.framework/Headers && pwd -P)
sharpie bind \
  -f Path/To/FirebaseAuth.framework \
  --scope "$real_headers" \
  -sdk macosx26.5 \
  -o BindingOutput \
  -c -F"$(pwd -P)/AllFrameworks"
```

With proper scope, FirebaseAuth went from 44k LOC unfiltered →
1.5k LOC scoped, and `[Verify]` annotations from 408 → 15.

### Post-sharpie fix catalogue

Sharpie's output is not directly compilable. Every binding needs
post-processing. Patterns we hit on FirebaseAuth that will recur:

| Issue | Fix |
|---|---|
| `[Verify(...)]` annotations | **Review sentinels**, NOT real binding attributes. Read, decide, then **delete the line**. |
| Multi-line `//` comments from `NS_SWIFT_NAME(\n  Identifier\n)` macros | Prefix continuation lines with `//`. |
| Duplicate `[Static]` attribute across `partial interface Constants` blocks | C# only allows it once. Keep on first, strip from rest. |
| Duplicate method overloads with same C# signature | Two ObjC overloads (`email/password`, `email/link`) collide. Rename one or comment out. |
| `_NSZone*` parameters | Not bound. `NSObject.copyWithZone:` is provided by `INSCopying` — comment out. |
| `: IFooProtocol` reference fails | I-prefixed interface is auto-generated by bgen at codegen time; pre-compile can't see it. Strip the `: IFoo` from interface declarations with `[Protocol]` siblings. |
| `NSDictionary<NSString, FIRApp>` rejects `FIRApp` as generic arg | `[BaseType]` attr doesn't extend C# interface at language level. Replace generic with bare `NSDictionary`. |
| `byte[]` fields for C constants | bgen can't bind `byte[]` as `[Field]`. Comment out — version strings aren't critical. |
| Swift-bridged ObjC categories (`FIRUser_FirebaseAuth_Swift_NNNN`) emit non-compilable bgen output | Strip the entire category block. |
| Imports after first `namespace` declaration | After concatenating sharpie outputs, manually move usings to the top. |
| Bgen-emitted duplicate constructors (e.g., `FIROAuthCredential` from `INSSecureCoding` + base) | Strip the interface that causes the duplicate. |

Script the fixes as a single python pass. Re-run when the SDK is
updated.

### Binding project csproj — minimum-viable

```xml
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net10.0-macos</TargetFramework>
    <IsBindingProject>true</IsBindingProject>
    <SupportedOSPlatformVersion>26.0</SupportedOSPlatformVersion>
    <!-- xcframeworks are directories, not single files. Embed via
         NativeReference, NOT as resources in the binding assembly. -->
    <NoBindingEmbedding>true</NoBindingEmbedding>
  </PropertyGroup>

  <ItemGroup>
    <NativeReference Include="Frameworks/FirebaseAuth.xcframework">
      <Kind>Static</Kind>
      <ForceLoad>True</ForceLoad>
      <Frameworks>AppKit Foundation Security SystemConfiguration</Frameworks>
      <LinkerFlags>-lz</LinkerFlags>
    </NativeReference>
    <!-- ...repeat for every transitive dep, Kind=Static... -->
  </ItemGroup>

  <ItemGroup>
    <ObjcBindingApiDefinition Include="ApiDefinition.cs" />
    <ObjcBindingCoreSource Include="StructsAndEnums.cs" />
    <Compile Remove="ApiDefinition.cs" />
    <Compile Remove="StructsAndEnums.cs" />
  </ItemGroup>

  <PropertyGroup>
    <!-- Firebase README requires both. -ObjC forces all ObjC classes
         to load even if only referenced by selector; -lc++ pulls in
         libc++ for C++ code in transitive deps (GoogleUtilities). -->
    <LinkerExtraArgs>-ObjC -lc++</LinkerExtraArgs>

    <!-- FirebaseAuth's macOS slice has internal Swift code (heartbeat
         compression). Without this, crashes with doesNotRecognizeSelector
         inside Data.zipped() when the heartbeat logger fires. -->
    <LinkWithSwiftSystemLibraries>true</LinkWithSwiftSystemLibraries>
  </PropertyGroup>
</Project>
```

**Naming**: keep `ApiDefinition.cs` + `StructsAndEnums.cs` as exact
filenames (singular, no suffixes). SDK auto-detection interacts
poorly with variant names.

### Host app — bundle ID + entitlements + provisioning profile

```xml
<!-- Info.plist -->
<key>CFBundleIdentifier</key>
<string>app.pivox.native</string>  <!-- match SwiftUI app's BUNDLE_ID -->
```

Firebase API key is bundle-ID-restricted at the Google Cloud Console
level. Mismatched bundle ID → request rejected before auth.

```xml
<!-- Entitlements.plist -->
<key>keychain-access-groups</key>
<array>
  <string>FAENDBN66M.app.pivox.native</string>
</array>
```

The team ID prefix `FAENDBN66M` is **NOT** the cert's CN suffix
`T55ZHGSMZJ`. They're different identifiers. Inspect the provisioning
profile to find the team ID:

```sh
security cms -D -i embedded.provisionprofile | grep -A1 ApplicationIdentifierPrefix
```

```xml
<!-- Pivox.macOS.csproj -->
<EnableCodeSigning>true</EnableCodeSigning>
<CodesignKey>Apple Development: name@example.com (CERTSUFFIX)</CodesignKey>
<CodesignEntitlements>Entitlements.plist</CodesignEntitlements>
<CodesignProvision>Mac Team Provisioning Profile: app.pivox.native</CodesignProvision>
```

`dotnet build` cannot auto-generate provisioning profiles (no
`-allowProvisioningUpdates` equivalent). Reuse the SwiftUI app's
profile or generate one via Xcode.

### Linker errors that hit us

| Symptom | Cause | Fix |
|---|---|---|
| `doesNotRecognizeSelector` inside `Data.zipped()` | Swift system libraries not linked | `<LinkWithSwiftSystemLibraries>true</LinkWithSwiftSystemLibraries>` in binding csproj |
| `Access to the path '*.xcframework' is denied` (CS1566) | C# compiler tried to embed xcframework directory as a single-file resource | `<NoBindingEmbedding>true</NoBindingEmbedding>` |
| `Launchd job spawn failed` after adding keychain entitlement | Entitlement claimed but no provisioning profile | Set `<CodesignProvision>` to a valid profile name |
| `MT7139: app requests entitlement 'X' but no provisioning profile` | Same | Same |
| `NETSDK1135: SupportedOSPlatformVersion higher than TargetPlatformVersion` (binding project only) | Binding-project SDK doesn't auto-align platform versions | Set both projects to same `<SupportedOSPlatformVersion>` |
| `FIRAuthErrorCodeKeychainError` (17995) at sign-in | App can't write to Keychain | `keychain-access-groups` entitlement + provisioning profile that grants it |

### DO / DON'T

**DO:**

- Bind only what you call. Embed everything else.
- Use `pwd -P` for `--scope`.
- Script the post-processing fixes; they recur on every SDK bump.
- Match host app's `CFBundleIdentifier` to the SDK's expected bundle.
- Inspect existing SwiftUI app's provisioning profile to find the
  right team ID prefix.

**DON'T:**

- Don't trust sharpie's "Bindings generated successfully" — always
  check output files exist and have content.
- Don't leave `[Verify]` annotations in — they don't compile.
- Don't bind every transitive framework.
- Don't use the cert's CN suffix as the team prefix in entitlements.
  Cert CN ≠ team ID.
- Don't put `keychain-access-groups` on without a provisioning
  profile — Launch Services refuses to spawn.
- Don't `rm -rf bin obj` while Rider is open.
- Don't smuggle Firebase types (or any other platform-binding types)
  across `Pivox.Shared` boundaries.

### Maintenance trigger — when to re-bind

- Vendor ships an SDK version bump with breaking API changes.
- You need an API that wasn't in the original `--scope` filter.
- An OS update changes a Swift system library ABI (rare).

Each re-bind: regenerate, diff, re-apply post-processing fixes.
Don't try to keep the binding "evergreen" — the SDK is source of
truth.

## Logging

`Console.Error.WriteLine` is fine for spike-level debugging. When the
client grows beyond spike, wire `Microsoft.Extensions.Logging` (the
.NET equivalent of `os.Logger` / Swift's `PivoxLog`) with category-
scoped loggers (`auth`, `chat`, etc.). Don't silently swallow errors
in catch blocks.

## Generated proto + gRPC client

`Pivox.Client/` owns the C# gRPC surface. Protos source from
`../api/proto/pivox/` (Pivox's own protos) plus
`../api/proto/buf/` (for the `buf.validate.*` option types that
field options reference). Vendored googleapis types come from
NuGet (`Google.Api.CommonProtos`, `Google.LongRunning`) — we do
NOT regenerate google.* protos.

```xml
<ItemGroup>
  <PackageReference Include="Google.Protobuf" Version="3.34.1" />
  <PackageReference Include="Grpc.Net.Client" Version="2.80.0" />
  <PackageReference Include="Grpc.Tools" Version="2.80.0">
    <IncludeAssets>runtime; build; native; contentfiles; analyzers; buildtransitive</IncludeAssets>
    <PrivateAssets>all</PrivateAssets>
  </PackageReference>
  <PackageReference Include="Google.Api.CommonProtos" Version="2.17.0" />
  <PackageReference Include="Google.LongRunning" Version="3.4.0" />
</ItemGroup>

<ItemGroup>
  <Protobuf Include="../../api/proto/pivox/**/*.proto"
            ProtoRoot="../../api/proto"
            GrpcServices="Client"
            AdditionalImportDirs="../../api/proto" />
  <Protobuf Include="../../api/proto/buf/**/*.proto"
            ProtoRoot="../../api/proto"
            GrpcServices="None"
            AdditionalImportDirs="../../api/proto" />
</ItemGroup>
```

Re-generate on backend proto changes by rebuilding `Pivox.Client`.
Don't hand-edit generated C#.

### Bearer-token attachment — CallCredentials, NOT Interceptor

gRPC bearer-token attachment goes via
`AuthCallCredentials.FromAuthService(IAuthService)` (in
`Pivox.Client/Auth/`), composed with channel credentials:

```csharp
var callCredentials = AuthCallCredentials.FromAuthService(auth);
options.Credentials = ChannelCredentials.Create(
    ChannelCredentials.SecureSsl, callCredentials);
```

**Why CallCredentials and not a client `Interceptor`**: the gRPC
client `Interceptor` API's metadata-attachment seam is synchronous.
Any async token fetch (`await auth.GetIdTokenAsync()`) must be
`.Result`-ed inside the interceptor, which deadlocks the main
thread when the underlying SDK callback (FIRAuth's
`getIDTokenWithCompletion:` on macOS) dispatches back to the main
thread for resolution. `CallCredentials.FromInterceptor` accepts an
async delegate — gRPC awaits it natively, no thread blocking.

Bit history: the first cut of this used `Interceptor` and produced
a hung UI on first RPC. Don't reintroduce that pattern.

### Generated-namespace shadowing

Generated proto namespaces (`Pivox.Iam.V1`, `Pivox.Api.V1`) contain
nested service-stub classes whose names match the outer namespace
segments — e.g. `Pivox.Iam.V1.Iam.IamClient`. Inside `Pivox.Client`,
`Iam.IamClient` resolves to the namespace `Pivox.Iam`, not the
nested class. Use the `global::` qualifier:

```csharp
public global::Pivox.Iam.V1.Iam.IamClient Iam => new(_channel);
```

`PivoxClient.cs` is the reference for this pattern.

### Cloud endpoint config

`Pivox.Shared/CloudConfig.cs` is the single source of truth for the
backend URL. Defaults to `pivox.ngrok.app:443`; override host via the
`PIVOX_GRPC_HOST` env var. **Always TLS** — there is no plaintext
mode in the dotnet stack. The SwiftUI side retains a
`PIVOX_GRPC_PLAINTEXT` escape hatch for local dev; the dotnet path
deliberately doesn't (broker callbacks carry id tokens, gRPC carries
Firebase JWTs — neither can leave the device unencrypted, and the
plaintext path was never exercised on dotnet). One `.envrc`
`PIVOX_GRPC_HOST` still switches both stacks against the same backend.

### Shared HTTP client

`Pivox.Shared/Http/SharedHttp.Instance` is the process-wide
`HttpClient` for every plain-HTTP call (OAuth token exchange, SSO
provider resolution, future broker REST surfaces). Don't construct
new `HttpClient` instances — they fragment the connection pool and
exhaust ephemeral ports under load. `IHttpClientFactory` is the
heavier-weight alternative; we don't run DI so the static singleton
is the right shape at this scale.

## Naming

Don't prefix internal C# types with `Pivox` — `ChatClient`, not
`PivoxChatClient`. We *are* the Pivox client. Exception: the
solution + class library names (`Pivox.Shared`, `Pivox.Client`) use
`Pivox.` to disambiguate from BCL types.
