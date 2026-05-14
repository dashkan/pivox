# AppKit + C# (.NET-for-macOS) — working notes

Captures the empirically-discovered rules for building native macOS apps
with .NET-for-macOS (`net10.0-macos` TFM, `Microsoft.macOS.dll`
bindings). These notes are grounded in actual spike work in the
`MacOsPivoxApp` sibling repo — not theory.

The default lazy reach is "guess from Swift docs, hit compile errors,
iterate." That wastes hours. The rules below cut the loop to seconds.

## Rule 0: this is AppKit. The language doesn't change it.

Swift, Objective-C, and C# all dispatch to the same `objc_msgSend`. The
runtime is the same. The cost difference between Swift and C# is *not*
in the API surface — it's in naming, documentation discoverability, and
tooling. All three are solvable with discipline.

## Rule 1: API discovery — MS docs first, Apple docs second

Apple's docs describe **behavior** (what does `NSSplitViewItem` do, when
is `viewDidLoad` called, what's the responder chain). They use
Swift/Objective-C names.

Microsoft's docs describe the **C# surface** (what's the C# name, what
overloads exist, what's the parameter type). The binding generator
renames things; Apple's docs don't reflect those renames.

**Workflow:**

1. Know what you want to do in AppKit terms (from Apple docs / WWDC).
2. Translate to C# signature via `microsoft_docs_search` MCP, or
   `learn.microsoft.com/en-us/dotnet/api/appkit.<typename>?view=net-macos-26.4-10.0`.
3. Only fall back to grepping `Microsoft.macOS.dll` if the docs miss.

If you find yourself guessing names, **stop**. One search call beats
five compile errors.

### Binding rename rules

Deterministic transformations the binding generator applies. Internalize
these and most translations become reflex:

| Swift / ObjC | C# |
|---|---|
| `windowBackgroundColor: NSColor` | `WindowBackground` (drops "Color" — return type already says it) |
| `controlAccentColor` | `ControlAccent` |
| `secondaryLabelColor` | `SecondaryLabel` |
| `+ sidebarWithViewController:` (Swift `init(sidebarWithViewController:)`) | static `CreateSidebar(NSViewController)` |
| `- initWithFrame:` | `new T(CGRect frame)` (positional, label dropped) |
| `+ classWithCoder:` | `new T(NSCoder coder)` |
| `setX:`, `getX` accessors | `X` property |
| `xxxForKey:` / `xxxWithName:` factory | static `CreateXxx(...)` or `FromXxx(...)` |
| ObjC `BOOL` | C# `bool` |
| ObjC `NSInteger` | C# `nint` |
| Methods with `completion:` block | `Task`-returning `XxxAsync` twin |
| Methods with `error:**NSError` out-param | C# throws `NSErrorException` |

**Heuristic**: when an ObjC property name redundantly repeats its
return type, the C# binding drops the suffix. `windowBackgroundColor`
returns `NSColor`, so C# is `WindowBackground`.

## Rule 2: Swift / ObjC sample code is fully usable

When Apple's docs ship a Swift sample for `NSCollectionView`, that
sample applies to C# — just translated via Rule 1. The "sparse C#
sample code" complaint is a non-issue once you can translate fluently.

For complex flows, read the Swift WWDC sample first to understand the
shape, then write the C# equivalent. Don't try to find a C# example —
just translate.

## Rule 3: async/await — use Task overloads

The binding generator auto-emits `Task`-returning twins for every
`completion:` block method. Always prefer the async overload:

```csharp
// Don't:
client.SignInWithEmail(email, password, (result, error) => { /*…*/ });

// Do:
var result = await client.SignInWithEmailAsync(email, password);
```

This is genuinely better than Swift's older Cocoa async story (Swift
needs `withCheckedContinuation` shims for un-bridged APIs; C# bindings
ship the bridge).

## Rule 4: NSViewController code-only construction

Storyboard-instantiated VCs have `protected Ctor(NativeHandle handle) : base(handle)`.
Code-instantiated VCs need a parameterless constructor and a `LoadView`
override:

```csharp
[Register("MyViewController")]
public sealed class MyViewController : NSViewController
{
    public MyViewController() : base((string?)null, null) { }

    public override void LoadView()
    {
        // Provide the view ourselves — otherwise NSViewController's
        // default LoadView tries to load a nib of the same name.
        View = new NSView(new CGRect(0, 0, 480, 600));
    }

    public override void ViewDidLoad()
    {
        base.ViewDidLoad();
        // Build subview hierarchy here.
    }
}
```

The `base((string?)null, null)` calls
`init(nibName:bundle:)` with both nil — explicit "no nib." Without it,
the VC tries to load a nib named after the class.

## Rule 5: AppDelegate must be wired explicitly when no storyboard

Without a `Main.storyboard` or `MainMenu.nib`, `NSApplicationMain` has
no mechanism to discover and instantiate your `AppDelegate` class —
`[Register]` makes it visible to the ObjC runtime but nothing
constructs it. Wire it explicitly in `Main.cs`:

```csharp
NSApplication.Init();
NSApplication.SharedApplication.Delegate = new AppDelegate();
NSApplication.Main(args);
```

Assignment must happen **before** `NSApplication.Main(args)` —
`Main` runs the event loop and calls `applicationDidFinishLaunching:`
on whatever delegate is set when it starts.

## Rule 6: storyboard files auto-inject `NSMainStoryboardFile`

The .NET-for-macOS SDK treats any `*.storyboard` in the project as an
`InterfaceDefinition` build item and **automatically writes
`NSMainStoryboardFile` into the bundle's Info.plist**. Stripping the
key from source `Info.plist` doesn't help — the build re-injects it.

If you want an all-code app, choose one:

- **Delete the storyboard from source** (simplest)
- **Exclude via csproj**: `<InterfaceDefinition Remove="Main.storyboard" />`

Verify after build:

```bash
/usr/libexec/PlistBuddy -c "Print NSMainStoryboardFile" \
  bin/Debug/net10.0-macos/osx-arm64/<App>.app/Contents/Info.plist
# Expected: "Print: Entry, "NSMainStoryboardFile", Does Not Exist"
```

## Rule 7: Main menu in code

No storyboard means no MainMenu.xib either. Build it in
`AppDelegate.DidFinishLaunching`:

```csharp
private static NSMenu BuildMainMenu()
{
    var main = new NSMenu();

    // Application menu (first item; title pulled from CFBundleName)
    var appMenu = new NSMenu();
    appMenu.AddItem(new NSMenuItem("About Pivox",
        new Selector("orderFrontStandardAboutPanel:"), ""));
    appMenu.AddItem(NSMenuItem.SeparatorItem);
    appMenu.AddItem(new NSMenuItem("Quit Pivox",
        new Selector("terminate:"), "q"));
    main.AddItem(new NSMenuItem { Submenu = appMenu });

    // Edit menu — required for NSTextField shortcuts to work
    var editMenu = new NSMenu("Edit");
    editMenu.AddItem(new NSMenuItem("Cut", new Selector("cut:"), "x"));
    editMenu.AddItem(new NSMenuItem("Copy", new Selector("copy:"), "c"));
    editMenu.AddItem(new NSMenuItem("Paste", new Selector("paste:"), "v"));
    editMenu.AddItem(new NSMenuItem("Select All",
        new Selector("selectAll:"), "a"));
    main.AddItem(new NSMenuItem("Edit") { Submenu = editMenu });

    return main;
}
```

Selectors are the standard Cocoa names (`terminate:`, `cut:`, `copy:`,
`paste:`, `selectAll:`, `orderFrontStandardAboutPanel:`,
`performMiniaturize:`, `performZoom:`, `arrangeInFront:`). These dispatch
up the responder chain — the active control or `NSApplication` handles
them.

## Rule 8: visual debugging — AX Inspector + RecursiveDescription, NOT view debugger

**Xcode's view hierarchy debugger does not work with .NET-for-macOS.**
Attach succeeds (with `get-task-allow` entitlement), but the view
hierarchy snapshot request times out or crashes the process.
Reproducible.

The alternatives:

| Tool | Works? | Use for |
|---|---|---|
| **Xcode → View Debugger** | ❌ | — (don't waste time) |
| **Accessibility Inspector** | ✅ | Live tree of all views with frames, click-to-highlight, point inspection |
| **`view.RecursiveDescription()`** | ✅ | Text tree dump with frames, hidden flags, layer info — log on demand |
| **Diagnostic background colors** (`view.WantsLayer = true; view.Layer!.BackgroundColor = NSColor.SystemPink.CGColor`) | ✅ | Catches zero-size and off-screen views instantly |
| **Auto Layout constraint conflict logs** | ✅ | Auto-prints to console; no setup |

For most layout debugging, **diagnostic colors during initial authoring
+ AX Inspector for "where did my view go" + RecursiveDescription for
periodic tree dumps** covers ~95% of what the view debugger would give
you in a native Swift app.

## Rule 9: Debug attach entitlement

Xcode and `lldb` need `com.apple.security.get-task-allow` to call
`task_for_pid` on the target process. .NET-for-macOS doesn't add this
automatically (Xcode does for native Debug builds). Add it for Debug:

```xml
<!-- Entitlements.plist (Debug) -->
<key>com.apple.security.get-task-allow</key>
<true/>
```

Strip for Release.

## Rule 10: never `rm -rf obj` if Rider is open

Rider's IB shim-sync watcher interprets directory removal as source
deletion and propagates it back. Lost an `AssetCellView.xib` once.
Use `dotnet clean` to clear build state — never `rm -rf`.

## Tooling reference

| Tool | Install | Use for |
|---|---|---|
| `sharpie` | `dotnet tool install -g Sharpie.Bind.Tool` | Generating C# bindings for third-party ObjC `.framework` / `.xcframework` |
| `mslearn_search` MCP | Bundled | Looking up C# binding signatures vs Apple docs |
| Accessibility Inspector | Xcode → Open Developer Tool → Accessibility Inspector | Visual view-tree introspection (works on .NET-for-macOS) |

## Build / launch reference

```bash
# Build Debug (incremental — fast)
dotnet build -c Debug

# Full clean rebuild — required after switching commits that
# delete-then-restore files; dotnet's incremental cache gets confused
dotnet clean -c Debug && dotnet build -c Debug

# Launch from terminal so stderr is visible
open bin/Debug/net10.0-macos/osx-arm64/<App>.app

# Verify Info.plist contents in bundle
/usr/libexec/PlistBuddy -c "Print" \
  bin/Debug/net10.0-macos/osx-arm64/<App>.app/Contents/Info.plist

# Inspect signed entitlements
codesign -d --entitlements - \
  bin/Debug/net10.0-macos/osx-arm64/<App>.app
```

## Binding third-party Cocoa SDKs — the real playbook

We ran this end-to-end with **FirebaseAuth 12.13.0** (FirebaseCore +
FirebaseAuth + ~10 transitive xcframeworks). Sign-in works through the
binding against the real Firebase backend with token persistence. The
rules below are written from that experience — not theory.

### Mental model

Two distinct things:

| Thing | Purpose | Trigger |
|---|---|---|
| **Bind** | Generate C# classes that mirror the framework's ObjC interface so you can `using Foo;` and call methods | Only for frameworks whose APIs you call directly from C# |
| **Embed** | Copy the framework into the app bundle so the dynamic loader can resolve symbols at runtime | Every framework in the dependency closure of what you call |

For FirebaseAuth specifically: 2 frameworks bound (`FirebaseAuth`,
`FirebaseCore`), 9 more embedded as `<NativeReference>` items. Don't
bind everything — review burden scales linearly with bound surface
area.

### Tooling — install once

```sh
dotnet tool install -g Sharpie.Bind.Tool
sharpie --version          # expect 26.4.x or newer
```

`sharpie` is now a .NET global tool. The old `.pkg` installer path
in older docs is deprecated.

### Sharpie command — the load-bearing flag is `--scope`

Sharpie's default is to emit bindings for **everything it sees**,
including all of Foundation, AppKit, CoreFoundation that the framework
transitively imports. Without `--scope` the output is ~44k lines of
mostly noise.

**The trap**: `--scope` filters by canonical source path. XCFrameworks
on macOS resolve through several symlinks (`/tmp` → `/private/tmp`,
`Headers` → `Versions/A/Headers`). If you pass a `--scope` value that
isn't the fully-resolved real path, **sharpie reports "Bindings
generated successfully" and emits zero files**. Silent failure.

Correct invocation:

```sh
real_headers=$(cd Path/To/FirebaseAuth.framework/Headers && pwd -P)
sharpie bind \
  -f Path/To/FirebaseAuth.framework \
  --scope "$real_headers" \
  -sdk macosx26.5 \
  -o BindingOutput \
  -c -F"$(pwd -P)/AllFrameworks"   # all peer framework search paths
```

With proper scope, FirebaseAuth's output dropped from 44k LOC →
1.5k LOC, and `[Verify]` annotations from 408 → 15. That's the
difference between "unworkable" and "manageable."

### Post-sharpie fix catalogue

Sharpie's output is not directly compilable. Every binding needs
post-processing. The fixes below are the ones we hit on FirebaseAuth —
similar patterns will recur on any non-trivial SDK.

| Issue | Fix |
|---|---|
| `[Verify(...)]` annotations everywhere | These are **review sentinels**, NOT real binding attributes. Each one is sharpie saying "I'm not sure about this binding." Read, decide, then **delete the line**. They don't compile if left in. |
| Multi-line `//` comments from macros like `NS_SWIFT_NAME(\n  Identifier\n)` | The macro's newline turned a comment into broken code. Prefix continuation lines with `//`. |
| Duplicate `[Static]` attribute across `partial interface Constants` blocks | Sharpie emits `[Static]` on every partial; C# only allows it once. Keep on first, strip from rest. |
| Duplicate method overloads with same C# signature (e.g., `SignInWithEmail(string, string, completion)` twice) | Two ObjC overloads with `email/password` and `email/link` map to identical C# signatures. Rename one or comment out (we commented `:link:` variants — not on critical path). |
| `_NSZone*` parameters | `_NSZone` isn't bound in `Microsoft.macOS.dll`. `NSObject.copyWithZone:` is provided by `INSCopying` anyway — comment out the duplicate. |
| `: IFooProtocol` reference fails to resolve | The `I`-prefixed interface is auto-generated by bgen at codegen time, but the pre-compile pass doesn't see it. Strip the `: IFoo` from interface declarations that have `[Protocol]` siblings. |
| `NSDictionary<NSString, FIRApp>` rejects `FIRApp` as generic arg | `FIRApp` interface (with `[BaseType(typeof(NSObject))]`) doesn't inherit `INativeObject` at the **C# language level** — only via attribute at codegen. Replace generic with bare `NSDictionary`. |
| `byte[]` fields for C constants like `extern const unsigned char[] FirebaseAuthVersionString` | bgen can't bind `byte[]` as a `[Field]`. Comment out — version strings aren't critical. |
| Swift-bridged ObjC categories (`FIRUser_FirebaseAuth_Swift_2100`) emit non-compilable bgen output | These are Swift extensions exposed via ObjC bridging headers. The main class has the same methods. Strip the entire category block. |
| Imports after first `namespace` declaration | `using` must come before namespace. After concatenating sharpie outputs from multiple frameworks, manually move usings to the top of the file. |
| Bgen-emitted duplicate constructors (e.g., `FIROAuthCredential` from `INSSecureCoding` + base class) | Bgen has trouble with multi-inheritance ctor synthesis. Strip the interface that causes the duplicate, or comment out the whole class if you don't need it. |

We scripted these as a single python pass against the raw sharpie
output. Re-run when the SDK is updated.

### Binding project — minimum-viable csproj

```xml
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net10.0-macos</TargetFramework>
    <IsBindingProject>true</IsBindingProject>
    <SupportedOSPlatformVersion>26.0</SupportedOSPlatformVersion>
    <!-- xcframeworks are directories, not single files. Embed
         via NativeReference (copies into host .app/Contents/),
         not as resources in the binding assembly. -->
    <NoBindingEmbedding>true</NoBindingEmbedding>
  </PropertyGroup>

  <ItemGroup>
    <!-- Bound: APIs we call from C#. ForceLoad ensures ObjC
         classes survive linker dead-code stripping. -->
    <NativeReference Include="Frameworks/FirebaseAuth.xcframework">
      <Kind>Static</Kind>
      <ForceLoad>True</ForceLoad>
      <Frameworks>AppKit Foundation Security SystemConfiguration</Frameworks>
      <LinkerFlags>-lz</LinkerFlags>
    </NativeReference>
    <NativeReference Include="Frameworks/FirebaseCore.xcframework">
      <Kind>Static</Kind>
      <ForceLoad>True</ForceLoad>
    </NativeReference>

    <!-- Embedded only — runtime symbol resolution, no C# API. -->
    <NativeReference Include="Frameworks/FirebaseAuthInterop.xcframework">
      <Kind>Static</Kind>
    </NativeReference>
    <!-- ...repeat for every transitive dep... -->
  </ItemGroup>

  <ItemGroup>
    <ObjcBindingApiDefinition Include="ApiDefinition.cs" />
    <ObjcBindingCoreSource Include="StructsAndEnums.cs" />
    <Compile Remove="ApiDefinition.cs" />
    <Compile Remove="StructsAndEnums.cs" />
  </ItemGroup>

  <PropertyGroup>
    <!-- Firebase README requires these. -ObjC forces all ObjC
         classes to load even if only referenced by selector;
         -lc++ pulls in libc++ for GoogleUtilities's C++ code. -->
    <LinkerExtraArgs>-ObjC -lc++</LinkerExtraArgs>

    <!-- FirebaseAuth's macOS slice has internal Swift code
         (heartbeat compression, Swift-bridged headers). Without
         this, you crash with doesNotRecognizeSelector inside
         Data.zipped() when the heartbeat logger fires. -->
    <LinkWithSwiftSystemLibraries>true</LinkWithSwiftSystemLibraries>
  </PropertyGroup>
</Project>
```

**Naming**: keep `ApiDefinition.cs` and `StructsAndEnums.cs` as exact
filenames (singular, no suffixes). The SDK has stale auto-detection
for these names that interacts poorly with explicit Includes —
explicit + matching convention works; explicit + variant names
double-registers and breaks.

### Host app csproj — referencing the binding

```xml
<ItemGroup>
  <ProjectReference Include="..\Firebase.Bindings\Firebase.Bindings.csproj" />
</ItemGroup>

<ItemGroup>
  <!-- Firebase needs GoogleService-Info.plist in bundle Resources. -->
  <BundleResource Include="GoogleService-Info.plist" />
</ItemGroup>
```

### Host app Info.plist — bundle ID must match

Firebase API key in `GoogleService-Info.plist` is restricted to a
specific bundle ID at the Google Cloud Console level. Match it:

```xml
<key>CFBundleIdentifier</key>
<string>app.pivox.native</string>   <!-- same as iOS app's BUNDLE_ID -->
```

Mismatched bundle ID → Firebase rejects the request before it even
hits Auth servers.

### Entitlements + provisioning profile — required for Firebase Auth

FirebaseAuth on macOS persists user sessions to the **Data Protection
Keychain**. Without `keychain-access-groups`, sign-in fails with
`FIRAuthErrorCodeKeychainError` (17995). And the entitlement only
works if backed by a real provisioning profile.

`.NET-for-macOS` can't auto-generate provisioning profiles
(`dotnet build` has no `-allowProvisioningUpdates` equivalent). You
have two options:

1. **Reuse the SwiftUI app's profile** — Xcode auto-generates and
   refreshes a profile when the SwiftUI app builds. Copy
   `Pivox.app/Contents/embedded.provisionprofile` into your .NET
   app's source dir. The cert it authorizes is your local Apple
   Development cert, which `dotnet build` can sign with.
2. **Generate a fresh profile via Xcode** — create a throwaway
   Xcode project with bundle ID `app.pivox.native` and enable
   automatic signing. Xcode will create the profile. Copy out.

Either way, the `.NET-for-macOS` csproj wires it via:

```xml
<EnableCodeSigning>true</EnableCodeSigning>
<CodesignKey>Apple Development: name@example.com (CERTSUFFIX)</CodesignKey>
<CodesignEntitlements>Entitlements.plist</CodesignEntitlements>
<CodesignProvision>Mac Team Provisioning Profile: app.pivox.native</CodesignProvision>
```

And `Entitlements.plist` uses the **team ID prefix** (NOT the cert CN
suffix — they're different!):

```xml
<key>keychain-access-groups</key>
<array>
    <!-- Team ID prefix from the provisioning profile, not the cert. -->
    <string>FAENDBN66M.app.pivox.native</string>
</array>
```

**Trap**: `T55ZHGSMZJ` in the cert name `Apple Development: …
(T55ZHGSMZJ)` is the *cert's* subject suffix, NOT a team identifier.
Inspect the provisioning profile to find the real team ID
(`security cms -D -i embedded.provisionprofile | grep -A1
ApplicationIdentifierPrefix`).

### Calling the bound API from C#

```csharp
using FirebaseAuth;
using FirebaseCore;

// Once per process, main thread, before any FIRAuth API.
if (FIRApp.DefaultApp is null)
    FIRApp.Configure();  // reads GoogleService-Info.plist from bundle

// Async wrap — sharpie didn't emit [Async] overloads.
var tcs = new TaskCompletionSource<(FIRAuthDataResult? r, NSError? e)>();
FIRAuth.Auth.SignInWithEmail(email, password, (result, error) =>
{
    tcs.SetResult((result, error));
});
var (result, error) = await tcs.Task;
```

Note the static accessor pattern: `FIRAuth.Auth` (no parens) maps to
`+[FIRAuth auth]`. The binding generator turned the static factory
into a static property.

### Linker errors that hit us

| Symptom | Cause | Fix |
|---|---|---|
| `doesNotRecognizeSelector` inside `Data.zipped()` on first Firebase call | Swift system libraries not linked | `<LinkWithSwiftSystemLibraries>true</LinkWithSwiftSystemLibraries>` in binding csproj |
| `Access to the path '*.xcframework' is denied` (CSC error CS1566) | C# compiler tried to embed xcframework directory as a single-file resource | `<NoBindingEmbedding>true</NoBindingEmbedding>` in binding csproj |
| `Launchd job spawn failed` on launch after adding keychain entitlement | Entitlement claimed but no provisioning profile | Set `<CodesignProvision>` to a valid profile name |
| `MT7139: app requests the entitlement 'X' but no provisioning profile` (warning) | Same as above | Same as above |
| `error NETSDK1135: SupportedOSPlatformVersion 26.4 cannot be higher than TargetPlatformVersion 26.0` (binding project only) | Binding-project SDK doesn't auto-align platform versions | Set both projects to same `<SupportedOSPlatformVersion>` |

### DO / DON'T checklist

**DO:**

- Bind only what you call. Embed everything else.
- Use `pwd -P` to get canonical paths for `--scope`.
- Script the post-processing fixes — they recur on every SDK bump.
- Match host app's `CFBundleIdentifier` to the SDK's expected bundle.
- Inspect the existing SwiftUI app's provisioning profile to find
  the right team ID prefix.
- Re-run sharpie when the SDK version changes. Diff the output.

**DON'T:**

- Don't trust sharpie's "Bindings generated successfully" message —
  always check the output files exist and have content.
- Don't leave `[Verify]` annotations in — they don't compile.
- Don't bind every transitive framework. The `[Verify]` count goes
  linear with bound surface; review burden becomes intractable.
- Don't use the cert's CN suffix as the team prefix in entitlements.
  Cert CN ≠ team ID.
- Don't try to compile the sharpie output without post-processing.
  It WILL have ~10+ structural errors per framework.
- Don't put the `keychain-access-groups` entitlement on without
  a provisioning profile — Launch Services refuses to spawn the app.
- Don't `rm -rf bin obj` while Rider is open (Rider's file watcher
  re-syncs deletions back into Xcode's storyboard sibling project,
  has eaten files before). Use `dotnet clean`.

### Maintenance trigger — when to re-bind

- Vendor ships an SDK version bump with breaking API changes (same
  trigger as `pod update` in Swift world).
- You need an API that wasn't in the original `--scope` filter.
- An OS update changes a Swift system library ABI (rare).

Each re-bind: regenerate, diff, re-apply the post-processing fixes.
Don't try to keep the binding "evergreen" — the SDK is the source of
truth. The binding is a derived artifact.

### Reference — FirebaseAuth case study

What it took to get FirebaseAuth working end-to-end in
`MacOsPivoxApp`:

| Step | Time | What we learned |
|---|---|---|
| Install sharpie | 30s | `dotnet tool install -g Sharpie.Bind.Tool` |
| Download Firebase iOS SDK 12.13.0 | ~2 min | 328 MB zip |
| Map dep tree | 5 min | 12 xcframeworks for Auth (2 bound + 10 embedded) |
| Wrong sharpie scope path | 20 min | Silent failure — sharpie said "success" but emitted 0 files. Resolved by using `pwd -P` for canonical scope path. |
| Sharpie generation | <10s | 1500 LOC, 48 FIR types, 15 [Verify] sentinels |
| Post-processing fixes | ~1 hour | 12 categories of fixes, scripted in python |
| Binding project setup | 15 min | Discovered `NoBindingEmbedding=true` for xcframeworks |
| Link error: `doesNotRecognizeSelector Data.zipped()` | 15 min | Swift system libs not linked. Fixed via `LinkWithSwiftSystemLibraries`. |
| Bundle ID mismatch | 5 min | Firebase API key restricted to `app.pivox.native` |
| Keychain entitlement | 30 min | Discovered we need a real provisioning profile, not just a cert. Reused the SwiftUI app's profile. |
| Team ID prefix discovery | 10 min | The cert CN suffix `T55ZHGSMZJ` is NOT the team prefix. Real team ID `FAENDBN66M` is in the provisioning profile. |
| **End-to-end: real sign-in working** | ~3 hours total | First-time learning curve; subsequent re-binds would be ~30 min |

## Things we tested and confirmed in the spike

The MacOsPivoxApp sibling repo demonstrates:

- ✅ Code-only `NSWindow` + `NSSplitViewController` + sidebar/detail VCs
- ✅ Code-only main menu (App / Edit / Window)
- ✅ NativeAOT compilation (2.6 MB native binary, 4.2 MB total bundle)
- ✅ Apple Development cert signing via `<CodesignKey>` property
- ✅ Firebase Auth REST flow (`identitytoolkit.googleapis.com`) from
  C# with the `X-Ios-Bundle-Identifier` header for iOS-key-restricted
  projects
- ✅ Full chain: .NET-for-macOS UI → Firebase REST → Pivox
  `beforeSignIn` blocking function → `pivox-cloud syncIdentity` →
  returns id token
- ❌ Xcode view-hierarchy debugger (does not work — see Rule 8)

## What this doc is not

Not a tutorial. Not a "getting started." Not exhaustive. This is the
working set of empirical rules that let you write .NET-for-macOS code
at roughly the same speed as Swift after the first ~15 minutes of
project setup. Update it when a new rule earns its place — i.e. when
you hit a problem twice.
