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

## Source layout

```
dotnet/
  Pivox.slnx              solution file (XML format, .NET 9+)
  Pivox.Shared/           cross-platform contracts. No platform deps.
                          IAuthService, AuthSession, JwtClaims, future
                          view models / domain types.
  Pivox.Client/           gRPC clients. Consumes ../api/*.proto via
                          Grpc.Tools. Bearer-token auth provided via
                          IAuthService from Pivox.Shared.
  Firebase.Bindings/      macOS-only sharpie binding of the Firebase
                          Cocoa SDK (FirebaseAuth + FirebaseCore + 9
                          embedded-only transitive xcframeworks).
                          Not consumed by Windows code.
  PivoxApp/               macOS app. All-code NSWindow root, no
                          storyboard. Implements IAuthService against
                          the Firebase binding. Signs with the local
                          Apple Development cert + provisioning
                          profile from the SwiftUI Pivox build.
  PivoxApp.Windows/       (future) WinUI 3 app. Will implement
                          IAuthService against a C++/WinRT Firebase
                          component.
  scripts/                build-time setup (fetch-firebase-sdk.sh).
```

### Dependency-direction rule

Code may depend **downward**, never **upward**, never **sideways**.

- `Pivox.Shared` depends on nothing in this directory.
- `Pivox.Client` depends on `Pivox.Shared`.
- `Firebase.Bindings` depends on nothing.
- `PivoxApp` may depend on all of the above.
- `PivoxApp.Windows` (future) depends on `Pivox.Shared` + `Pivox.Client`
  + a Windows-specific binding/projection assembly. **Not** on
  `Firebase.Bindings` (macOS-only) or `PivoxApp` (macOS-only).

**No platform-specific types in `Pivox.Shared` or `Pivox.Client`.**
That includes Firebase types, AppKit types, WinUI types, WinRT types.
The shared layer sees POCO records and the contracts it defines.
Anything platform-specific is an `IAuthService.cs`-style interface
implemented separately per platform.

### What goes where — decision tree

1. **Cross-platform code (logic, state, contracts, gRPC, view models)**
   → `Pivox.Shared` or `Pivox.Client`.
2. **macOS implementation of a cross-platform interface, or a macOS-only
   UI component** → `PivoxApp`.
3. **Windows implementation of a cross-platform interface, or a
   WinUI-only UI component** → `PivoxApp.Windows`.
4. **C# binding of a third-party Cocoa SDK** → `Firebase.Bindings`
   (or a sibling binding project per SDK).

## Build

```sh
# First-time setup: fetch Firebase xcframeworks (~111 MB, gitignored).
scripts/fetch-firebase-sdk.sh

# Debug build (incremental, fast — no AOT).
dotnet build Pivox.slnx

# Release publish (NativeAOT, code-signed, provisioning-profile
# embedded). Produces a static native binary, no Mono runtime.
dotnet publish PivoxApp/PivoxApp.csproj -c Release -r osx-arm64

# Launch (Debug). Open the .app bundle.
open PivoxApp/bin/Debug/net10.0-macos/osx-arm64/PivoxApp.app
```

Provisioning profile setup (one-time, per developer machine): copy
the SwiftUI Pivox app's Xcode-generated profile into `PivoxApp/`:

```sh
cp ../native/build-xcode/Debug/Pivox.app/Contents/embedded.provisionprofile \
   PivoxApp/embedded.provisionprofile
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
  PivoxApp/bin/Debug/net10.0-macos/osx-arm64/PivoxApp.app/Contents/Info.plist
# Expected: 'NSMainStoryboardFile' Does Not Exist
```

## Rule 7: Main menu in code

No storyboard means building NSMenu in `AppDelegate.DidFinishLaunching`.
Minimum-viable: Application menu (Quit) + Edit menu (Cut/Copy/Paste/
Select All so NSTextField shortcuts work) + Window menu.
`PivoxApp/AppDelegate.cs`'s `BuildMainMenu()` is the reference shape.

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
`PivoxApp.csproj` sets `<PublishAot>true</PublishAot>` (unconditional;
AOT only triggers at publish time, not build).

## Tooling reference

| Tool | Install | Use for |
|---|---|---|
| `sharpie` | `dotnet tool install -g Sharpie.Bind.Tool` | Generating C# bindings for third-party ObjC `.framework` / `.xcframework` |
| `microsoft_docs_search` MCP | Bundled | Looking up C# binding signatures vs Apple docs |
| Accessibility Inspector | Xcode → Open Developer Tool → Accessibility Inspector | Visual view-tree introspection (works on .NET-for-macOS) |

## Binding third-party Cocoa SDKs — the playbook

Reference implementation: `Firebase.Bindings/` (FirebaseAuth +
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
<!-- PivoxApp.csproj -->
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

## Generated proto

gRPC clients live in `Pivox.Client/`. Protos source from `../api/`
via `Grpc.Tools` MSBuild integration:

```xml
<ItemGroup>
  <Protobuf Include="../../api/**/*.proto"
            ProtoRoot="../../api"
            GrpcServices="Client" />
</ItemGroup>
```

Re-generate on backend proto changes by rebuilding `Pivox.Client`.
Don't hand-edit generated C#.

## Naming

Don't prefix internal C# types with `Pivox` — `ChatClient`, not
`PivoxChatClient`. We *are* the Pivox client. Exception: the
solution + class library names (`Pivox.Shared`, `Pivox.Client`) use
`Pivox.` to disambiguate from BCL types.
