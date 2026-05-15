# WinUI — AI Chat build brief

The dotnet macOS side of AI chat is being built in phases. This is
the build brief for matching each phase on WinUI as it lands.

**Canonical reference: the macOS implementation in
`dotnet/Pivox.macOS/Ai/` and the cross-platform layer in
`dotnet/Pivox.Shared/Ai/`.** Use those as the source of truth for
state machines, threading, and error mapping. The
`native/platform/macos/swift/AIChat/` SwiftUI sources are NOT the
reference for the dotnet port — they predate the redesign and use
SwiftUI patterns that don't translate to AppKit/WinUI.

Read alongside `dotnet/CLAUDE.md` (Rule 12 covers the threading
model — `SynchronizationContext` capture + UI-thread call
preconditions — which applies identically to WinUI).

## Status across the phases

The chat port is structured A → E. Phase A and Phase B step 1 are
landed on macOS. WinUI work resumes when macOS finishes Phase B
step 2 (UI scaffolding) — the goal is for both platforms to track
each other phase by phase so neither stack diverges.

| Phase | Scope | macOS status | WinUI status |
|---|---|---|---|
| A. Native libs | C++ markdown parser + Rust tree-sitter via P/Invoke (`Pivox.Native`) | ✅ done | 🔜 pending |
| B.1 Wire path | Cross-platform state machine + gRPC adapter, no UI | ✅ done | 🔜 pending (this doc) |
| B.2 UI scaffolding | Transcript view + composer + window chrome | ⏳ next on macOS | — |
| C. Markdown + highlighting | Wire `Pivox.Native` into rendering | — | — |
| D. Interactive polish | Hover actions, scroll preservation, shortcuts | — | — |
| E. History + window chrome | Popover list, detached window, dock/detach | — | — |

This doc covers what WinUI needs to do for **Phase A + Phase B
step 1**. Phase B step 2 (UI) gets its own brief once the macOS UI
shape stabilizes.

## Phase A — native lib pipeline

The macOS side ships two native libs P/Invoked from C#:

- `libpivox_markdown.dylib` — C++ wrapper around cmark-gfm
  (markdown parser → JSON block list)
- `libpivox_highlight.dylib` — Rust cdylib wrapping tree-sitter
  (syntax highlighting, 14 language grammars)

Sources live at:

```
dotnet/native/markdown/         CMake project, vcpkg-driven cmark-gfm
dotnet/native/highlight/        Cargo project, Rust cdylib
dotnet/scripts/build-ai-native.sh   host-RID build orchestrator
dotnet/Pivox.Native/                P/Invoke wrappers + carries runtimes/<rid>/native/
dotnet/Pivox.Native.Tests/          xunit smoke tests
```

### What WinUI does for Phase A

The native sources are cross-platform-ready by design. Both build
on Windows with no source changes:

- **markdown** — CMake + vcpkg. cmark-gfm has Windows MSVC support;
  `WINDOWS_EXPORT_ALL_SYMBOLS` in `CMakeLists.txt` produces the
  `.dll` with all required exports.
- **highlight** — Cargo. tree-sitter + grammars build on
  `x86_64-pc-windows-msvc` without modification.

To enable Windows native builds, edit
`dotnet/scripts/build-ai-native.sh` so the existing `win-x64`
branch produces:

```
dotnet/Pivox.Native/runtimes/win-x64/native/pivox_markdown.dll
dotnet/Pivox.Native/runtimes/win-x64/native/pivox_highlight.dll
```

The script already detects `win-x64` via `uname -s`. On a Windows
dev machine, the same script runs under git-bash / WSL bash, or
port to PowerShell as `dotnet/scripts/build-ai-native.ps1`
(precedent: `dotnet/scripts/fetch-firebase-cpp-sdk.ps1`).

Pivox.Native.csproj's existing host-RID guard already includes the
Windows branch — the build will fail with an actionable "run
`make ai-native`" message until the DLLs are staged.

### Phase A validation checklist (WinUI)

- [ ] `dotnet/scripts/build-ai-native.ps1` (or .sh under git-bash)
      produces both DLLs in `runtimes/win-x64/native/`
- [ ] `Pivox.Native.Tests` passes on Windows (12 tests, same set
      that runs on macOS — they're platform-agnostic P/Invoke
      tests)
- [ ] `dotnet build Pivox.WinUI` succeeds with `Pivox.Native`
      as a ProjectReference
- [ ] At runtime, the P/Invoke `[DllImport("pivox_markdown")]`
      and `[DllImport("pivox_highlight")]` resolve against the
      DLLs deployed alongside the WinUI app binary

The DLL placement should be automatic — the `<Content>` items in
`Pivox.Native.csproj` already copy to the consumer's output dir.
For WinUI specifically, that should be next to `Pivox.exe`.

## Phase B step 1 — wire path

Shared cross-platform code is done — `Pivox.Shared/Ai/` carries
the contracts, state machine, and DTOs. WinUI consumes them
as-is.

### What's in `Pivox.Shared/Ai/`

| File | Purpose |
|---|---|
| `MessageRole.cs` | enum (Unspecified/User/Assistant). Numeric values match proto `pivox.ai.v1.Role`. |
| `Message.cs` | Mutable-Text class with INPC. Used for the placeholder-streaming pattern. |
| `ConversationState.cs` | enum (Idle/Loading/Streaming/Error). |
| `ChatStreamEvent.cs` | Discriminated union — abstract record + sealed `TextStartEvent` / `TextDeltaEvent` / `TextEndEvent`. |
| `ChatErrorKind.cs` | enum (NotSignedIn/AuthenticationRequired/PermissionDenied/Network/Server/Cancelled). |
| `ChatException.cs` | Exception subclass with `Kind` + inner exception. |
| `IChatService.cs` | The cross-platform contract. **Updated in Phase B step 2a**: now `IAsyncEnumerable<ChatStreamEvent> StreamGenerateAsync(string organizationName, IReadOnlyList<ChatTurn>, CancellationToken)`. Organization name is now passed per-call, not bound at construction. |
| `ConversationViewModel.cs` | State machine. Owns Messages, drives the service, handles streaming + cancellation + errors. **Updated in Phase B step 2a**: constructor now takes `ActiveOrganization` alongside `IChatService`. Subscribes to `ActiveOrganization.PropertyChanged` and clears all chat state when `Current` changes (org switch wipes per-org history). Reads `ActiveOrganization.Current` on each `SendAsync` and fails with `ChatErrorKind.Server` if no org is selected. |

### What WinUI builds

One file: `dotnet/Pivox.WinUI/Ai/WindowsChatService.cs` —
implements `IChatService` against `PivoxClient.Ai` (the generated
`AiChat.AiChatClient`). Mirror the shape of
`dotnet/Pivox.macOS/Ai/MacOsChatService.cs` exactly. **Note the
signature change in Phase B step 2a** — `organizationName` is now
a per-call parameter, not bound at construction:

```csharp
public sealed class WindowsChatService : IChatService
{
    private readonly PivoxClient _client;

    public WindowsChatService(PivoxClient client)
    {
        ArgumentNullException.ThrowIfNull(client);
        _client = client;
        // Call AssertRoleAlignment() once to catch proto enum drift.
    }

    public async IAsyncEnumerable<ChatStreamEvent> StreamGenerateAsync(
        string organizationName,
        IReadOnlyList<ChatTurn> turns,
        [EnumeratorCancellation] CancellationToken cancellationToken = default)
    {
        // Validate organizationName non-empty and starts with "organizations/".
        // Build Pivox.Ai.V1.GenerateContentRequest:
        //   - Parent = organizationName
        //   - Each ChatTurn → InputMessage with TextPart
        // Open AsyncServerStreamingCall via _client.Ai.StreamGenerateContent.
        // Iterate response stream, mapping ServerEvent.EventOneofCase to
        // ChatStreamEvent. Drop non-text-track events with Debug.WriteLine.
        // Map RpcException → ChatException via status-code switch.
    }
}
```

The macOS implementation handles all the edge cases — copy its
structure. The proto types are identical (both platforms reference
`Pivox.Client` which owns the generated stubs).

### Status of `PivoxClient.Ai`

`PivoxClient.cs` exposes `Ai` already:

```csharp
public global::Pivox.Ai.V1.AiChat.AiChatClient Ai => new(_channel);
```

Same as `Iam`, `Organizations`, `Spaces`. WinUI's `PivoxClient`
instance (constructed with `WindowsAuthService`) gets this for free.

### Phase B step 1 validation checklist (WinUI)

- [ ] `WindowsChatService` compiles with `Pivox.Shared` reference
- [ ] `AssertRoleAlignment` runs on construction (catches any proto
      regeneration that shifts enum values)
- [ ] Manual probe: construct `WindowsChatService` against a real
      `PivoxClient`, call `StreamGenerateAsync` with a single user
      turn, observe text events arriving (log to console for now;
      UI in Phase B step 2)
- [ ] RpcException paths exercised: 16 (UNAVAILABLE) → Network,
      7 (PERMISSION_DENIED) → PermissionDenied,
      16 (UNAUTHENTICATED) → AuthenticationRequired,
      4 (CANCELLED) → ChatException(Cancelled) → VM → Idle

## Phase B step 2a — shared state foundation

This step landed alongside the IChatService signature change. WinUI
consumes three new shared types:

### What's new in `Pivox.Shared/`

| File | Purpose |
|---|---|
| `Persistence/IKeyValueStore.cs` | Cross-platform abstraction for non-secret user preferences. Methods: `GetString` / `SetString` (null clears), `TryGetBool` / `SetBool`, `TryGetDouble` / `SetDouble`. Plus `TryGetEnum<T>` / `SetEnum<T>` extension methods (string-encoded, AOT-trim-safe). |
| `Persistence/IKeyValueStore.cs` (extensions) | `KeyValueStoreExtensions.TryGetEnum<T>` / `SetEnum<T>` for storing enums as their name strings. |
| `Organization/ActiveOrganization.cs` | Observable holder for the currently-active organization resource name (e.g. `organizations/acme`). INPC-based. Persists `Current` through the injected `IKeyValueStore` under the `pivox.active_organization` key. Null = no organization selected. |

### What WinUI builds

One file: `dotnet/Pivox.WinUI/Persistence/ApplicationDataKeyValueStore.cs`.
Backs `IKeyValueStore` with `Windows.Storage.ApplicationData.Current.LocalSettings`
— the WinUI-canonical user-preferences store, per-app-package per-user
container, automatic persistence. macOS reference impl is at
`dotnet/Pivox.macOS/Persistence/NsUserDefaultsKeyValueStore.cs` and
shows the expected method semantics (null-string normalization,
absent-key handling, the `ObjectForKey != null` trick to
disambiguate "set to false" from "absent" for bool/double).

Skeleton:

```csharp
public sealed class ApplicationDataKeyValueStore : IKeyValueStore
{
    private static ApplicationDataContainer Settings
        => ApplicationData.Current.LocalSettings;

    public string? GetString(string key)
    {
        if (!Settings.Values.TryGetValue(key, out var v)) return null;
        var s = v as string;
        return string.IsNullOrEmpty(s) ? null : s;
    }

    public void SetString(string key, string? value)
    {
        if (string.IsNullOrEmpty(value)) Settings.Values.Remove(key);
        else Settings.Values[key] = value;
    }

    public bool TryGetBool(string key, out bool value)
    {
        if (Settings.Values.TryGetValue(key, out var v) && v is bool b)
        {
            value = b;
            return true;
        }
        value = false;
        return false;
    }

    public void SetBool(string key, bool value) => Settings.Values[key] = value;

    // TryGetDouble / SetDouble — mirror TryGetBool / SetBool with double.
}
```

### What WinUI wires up

In `App.OnLaunched` (composition root), construct and pass through:

```csharp
_keyValueStore = new ApplicationDataKeyValueStore();
_activeOrganization = new ActiveOrganization(_keyValueStore);
_auth = new WindowsAuthService();
_pivox = new PivoxClient(_auth);
_chat = new WindowsChatService(_pivox);    // no organizationName!
// ...
_rememberedEmail = new RememberedEmail(_keyValueStore);  // refactored too
```

The chat panel page (when it lands in Phase B step 2b) will
construct `new ConversationViewModel(_chat, _activeOrganization)`.

`RememberedEmail` on the macOS side has been refactored onto
`IKeyValueStore`. The WinUI side's `RememberedEmail` (currently
backed directly by `ApplicationData.LocalSettings`) should be
similarly refactored to consume `IKeyValueStore` for consistency.

### Org switch behavior

When `ActiveOrganization.Current` changes:

- `ConversationViewModel.OnActiveOrganizationChanged` fires.
- Any in-flight stream is cancelled via the CTS.
- `Messages` is cleared.
- `LastErrorKind` / `LastErrorMessage` are reset.
- `State` returns to `ConversationState.Idle`.

The viewmodel is per-org by behavior, not by lifetime — a single
viewmodel instance handles every org the user switches between.
The platform UI doesn't need to recreate the VM on org switch;
binding to `ActiveOrganization` from the UI layer is enough to
trigger the reset automatically.

### Where the org dropdown lives (macOS reference)

`dotnet/Pivox.macOS/DetailViewController.cs` hosts an
`NSPopUpButton` populated from
`Organizations.ListOrganizationsAsync`. Selection writes through
`ActiveOrganization.Current`, which:

1. Persists the new value via `IKeyValueStore`.
2. Fires `PropertyChanged` → any subscribed `ConversationViewModel`
   wipes its per-org state.

WinUI's equivalent is a `ComboBox` somewhere in the shell chrome
(the natural home is wherever WinUI's "you're signed in as X"
header lives). Same data source, same write target. The UI choice
is up to the WinUI side; the state binding is fixed by the shared
contract.

### Phase B step 2a validation checklist (WinUI)

- [ ] `ApplicationDataKeyValueStore` implements `IKeyValueStore`,
      builds clean
- [ ] Composition root constructs `IKeyValueStore` →
      `ActiveOrganization` → wires through to consumers
- [ ] `RememberedEmail` refactored onto `IKeyValueStore` (the
      existing direct-`ApplicationData` usage replaced)
- [ ] `WindowsChatService` constructor drops the
      `organizationName` parameter
- [ ] `WindowsChatService.StreamGenerateAsync` accepts
      `organizationName` as the first parameter and validates it
- [ ] Org selector somewhere in the shell writes
      `ActiveOrganization.Current`
- [ ] Manual probe: switch organization while a chat stream is
      mid-flight, verify the transcript clears and state returns
      to Idle without a window flicker
- [ ] Manual probe: select an organization, quit, relaunch, verify
      the same organization is preselected from
      `ApplicationDataKeyValueStore`

## Cross-cutting rules (from `dotnet/CLAUDE.md`)

These apply identically on WinUI. Internalize before writing code:

### Rule 12 — UI-thread events

`ConversationViewModel` captures `SynchronizationContext.Current`
at construction and **throws** if absent. WinUI 3 installs a
dispatcher-backed sync context on the UI thread via
`DispatcherQueueSynchronizationContext` — the VM construction must
happen on the dispatcher thread. The viewmodel relies on
`ConfigureAwait(true)` (default) to keep stream-iteration
continuations on the captured context; the caller's contract is
"call `SendAsync` from the UI thread."

`WindowsChatService` itself doesn't need explicit `Post` —
`Grpc.Net.Client`'s async iterators resume on the captured context.

### Rule 13–18 — AppKit-specific, not applicable

Window/VC ownership, glass effects, accent colors — all AppKit
patterns. WinUI has its own equivalents (DispatcherQueue ownership,
Mica/Acrylic, theme colors); apply the spirit, not the letter.

### Naming

`WindowsChatService`, not `WinUIChatService` (mirrors
`WindowsAuthService` vs `MacOsAuthService`). Internal types stay
unprefixed — `ChatTurn`, not `PivoxChatTurn`.

### gRPC plumbing

`AuthCallCredentials.FromAuthService(IAuthService)` attaches the
Bearer JWT. No interceptor pattern (synchronous metadata seam,
deadlocks under async token fetch). Same shape as Pivox.macOS;
already wired through `PivoxClient`.

## What's NOT in this phase

Surfaces to defer until later briefs:

- **UI** — transcript view, composer, window. Phase B step 2 on
  macOS first, then a WinUI brief.
- **Markdown rendering** — Phase C. `Pivox.Native.MarkdownParser`
  is ready; wire when UI lands.
- **Syntax highlighting** — Phase C. `Pivox.Native.CodeHighlighter`
  is ready.
- **Conversation persistence** — Phase B is stateless calls (full
  transcript sent each turn). Phase D adds `conversation_id`
  threading + `ListConversations` / `GetConversation` history.
- **Tools, reasoning, artifacts** — Phase D+. The
  `WindowsChatService` drops these events silently in Phase B
  (with `Debug.WriteLine` for observability when they show up).

## Maintenance trigger

When the macOS Phase B step 2 (UI) lands, a follow-up doc
(`winui-chat-ui.md` or similar) will land alongside it. The shape
will mirror this brief — canonical reference + checklist + rules.
The pattern is working; keep it.

If proto regeneration shifts `pivox.ai.v1.Role` numbering, the
`AssertRoleAlignment` static check on `WindowsChatService` (and
`MacOsChatService`) trips on first construction — fix the cast
in `BuildRequest` rather than the assertion.
