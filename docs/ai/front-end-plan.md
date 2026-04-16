# AI Chat — macOS Frontend Implementation Plan

Scoped to the macOS AI Chat feature: how the native SwiftUI app consumes the Go BE (`docs/ai/go-be-plan.md`) via the `PivoxModels` SPM package and the shared C++ core.

**Not in scope**:
- The full `AIElements` component library (`docs/ai/plan.md`)
- WinUI port (handoff from `docs/ai/plan.md` M6)
- Go backend (`docs/ai/go-be-plan.md`)

## Engineering principles — non-negotiable

**No hacky code. Ever.** Local refactors and reverting unmerged work proceed freely. **Sweeping changes — touching files outside the current feature area, changing public interfaces, modifying merged work, crossing package boundaries, or rewriting >200 lines of existing working code — require user consultation before starting.**

**Complex engineering issues get consulted with Gemini before implementation.** When the obvious path leads somewhere hacky, stop, understand the root cause, prompt Gemini, capture the outcome in an adjacent design note, implement cleanly.

## TL;DR

- **Feature placement**: new sidebar navigation item in the existing SwiftUI app, next to Operator / Library / Designer / Engineering / Admin. v1 is a dedicated chat view; inline-in-context use comes later.
- **Proto types**: consumed via the `PivoxModels` SPM package (generated from `pivox.ai.v1` by swift-protobuf). Shared across all macOS → Pivox service workflows, not just AI chat.
- **Transport**: bidi gRPC `AiChat.Stream` via the shared C++ core. Swift doesn't speak gRPC directly — shared core owns the transport, Swift consumes via FFI + typed value semantics.
- **Storage session**: Swift calls `StorageGateways.CreateStorageSession` once after Firebase login → `pivox_session` cookie set on `.pivox.app` → all asset/gateway fetches work via `AsyncImage` / similar with credentials attached automatically by `URLSession`.
- **Conversation state**: server-authoritative. Client loads conversations via the AIP resource RPCs, streams new turns via the bidi method, never shadows history locally beyond view state.
- **Tool machinery**: wired end-to-end in v1. Client tool dispatch loop is functional but the tool registry is empty — adding a tool is a Swift registration call, not a new phase.
- **Artifact rendering**: inline-only in v1 (code, markdown, svg). Asset-backed artifacts and image generation are deferred.
- **Token counting / history**: client doesn't do token budgeting — that's server-side in the Go BE. Swift just renders what the model turn produces.
- **Testing**: TDD end-to-end — XCTest for unit/logic, `swift-snapshot-testing` for SwiftUI views, XCUITest for feature flows.
- **Swift version**: Swift 6 language mode, strict concurrency complete, C++ interop enabled. Existing Pivox Swift code migrates first if needed (M0 foundation spike from `docs/ai/plan.md`).

## Dependencies on other work

| Dependency | Source plan | Required before | Status |
|---|---|---|---|
| Go BE gRPC service live (`AiChat.Stream`, resource methods, `:content` handler, `ai:stream` SSE) | `docs/ai/go-be-plan.md` | Any integration testing | Pending |
| `PivoxModels` SPM package generated from `pivox.ai.v1` protos | this plan (Phase 0) | Any Swift code touching chat types | Pending |
| Shared C++ `AiChatClient` implementation (grpc-cpp bidi client, state machine, event decoding) | `docs/ai/plan.md` shared-core section + new work here | Any Swift code opening streams | Pending |
| Swift 6 language mode + C++ interop CMake settings | `docs/ai/plan.md` M0 | Any new Swift file in this plan | Pending — spike required |
| `AIElements` foundation layer (theme, surface, markdown pipeline, highlighter, disclosure style, tooltip, SVG renderer) | `docs/ai/plan.md` M0 | Chat view implementation | Pending — this plan blocks on `AIElements` M0 |
| Firebase auth + `MacAppState` bearer-token retrieval | Existing in `platform/macos/swift/Auth/` | Stream auth | Present |
| Asset system `storage_url` on AssetVersion | `docs/ai/go-be-plan.md` prerequisite | Rendering asset-backed artifacts | Deferred — not needed for v1 |

**v1 blockers**: Go BE live, `PivoxModels` package, shared C++ `AiChatClient`, Swift 6 + interop CMake settings, `AIElements` M0 foundation layer.

## Repo landing

| Concern | Location |
|---|---|
| SPM package with generated proto types | `native/platform/macos/swift-packages/PivoxModels/` |
| `buf.gen.yaml` extension for swift-protobuf | existing `buf.gen.yaml` |
| Chat client Swift layer | `native/platform/macos/swift/AIChat/` |
| SwiftUI views for chat feature | `native/platform/macos/swift/AIChat/Views/` |
| Shared C++ `AiChatClient` | `native/core/ai_chat/client/` |
| Obj-C++ or C FFI bridge | `native/platform/macos/objcpp/AIChatBridge.{h,mm}` |
| Tests — unit | `native/tests/macos/AIChatTests/` |
| Tests — snapshot (swift-snapshot-testing) | `native/tests/macos/AIChatTests/__Snapshots__/` |
| Tests — UI flows | `native/tests/macos/ui/AIChatUITests.swift` |

The existing `platform/macos/swift/` layout already has `Auth/`, `ImageEditor/`, `Components/`, `App/`. Adding a top-level `AIChat/` folder follows the same pattern. `AIElements` gets its own sibling folder per the AIElements plan — they're distinct modules.

## Architecture

Three layers, each with a clear boundary:

```
┌──────────────────────────────────────────────────────────────┐
│  SwiftUI app code (native/platform/macos/swift/AIChat/)      │
│  - Sidebar entry, conversation list, chat view               │
│  - Consumes `AIElements` for Message / PromptInput / etc.   │
│  - Uses typed proto values from PivoxModels                  │
│  - No direct gRPC, no transport logic                         │
└──────────────────────┬───────────────────────────────────────┘
                       │ typed Swift API
                       │ `ChatClient` façade
                       │ AsyncThrowingStream<ServerEvent, Error>
                       ▼
┌──────────────────────────────────────────────────────────────┐
│  Swift chat client (native/platform/macos/swift/AIChat/)     │
│  - `ChatClient` class — thin facade                          │
│  - Wraps shared C++ `AiChatClient` via FFI                   │
│  - Serializes outbound Swift protos to bytes                 │
│  - Receives bytes from C++, parses into typed Swift protos   │
│  - Exposes AsyncThrowingStream + async methods               │
└──────────────────────┬───────────────────────────────────────┘
                       │ C FFI
                       │ `pivox_ai_chat_*` functions
                       ▼
┌──────────────────────────────────────────────────────────────┐
│  Shared C++ core (native/core/ai_chat/client/)               │
│  - `AiChatClient` — grpc-cpp bidi stream lifecycle            │
│  - State machine (disconnected / connecting / streaming)      │
│  - Event decoding (grpc-cpp ServerEvent → bytes)              │
│  - Outbound serialization (bytes → grpc-cpp ClientEvent)      │
│  - DispatchQueue marshaling for Mac (main queue)              │
│  - Callbacks via C function pointers                          │
└──────────────────────┬───────────────────────────────────────┘
                       │ gRPC bidi stream
                       │ over TLS to Cloud Controller
                       ▼
              Go `AiChat.Stream`
              (docs/ai/go-be-plan.md)
```

**Key property**: Swift app code only ever sees typed `Pivox_Ai_V1_*` values. Bytes are an implementation detail of the bridge layer. The shared C++ core never exposes C++ proto types to Swift directly (those use template-heavy code that doesn't interop cleanly).

## Phases

Each phase is a commit or small set of closely related commits. TDD throughout.

### Phase 0 — Foundations (~1 day)

**Blocked by**: Swift 6 + C++ interop CMake settings from `docs/ai/plan.md` M0 spike.

Tasks:

1. **Add `PivoxModels` SPM package**
   - Create `native/platform/macos/swift-packages/PivoxModels/Package.swift`
   - Depend on `apple/swift-protobuf`
   - Declare the library target with C++ interop disabled (types are pure Swift)
   - Empty `Sources/PivoxModels/Generated/` directory for codegen output

2. **Extend `buf.gen.yaml`** with swift-protobuf plugin:
   ```yaml
     - local: protoc-gen-swift
       out: native/platform/macos/swift-packages/PivoxModels/Sources/PivoxModels/Generated
       opt:
         - Visibility=Public
         - FileNaming=FullPath
   ```

3. **Run `buf generate`** → emits `.pb.swift` files for every `pivox.*.v1` proto (ai, api, iam, assets, storage, agent) into the package

4. **Wire the package into the main macOS target** via CMake. Add `PivoxModels` as an SPM dependency of the app target. First SPM package in the Pivox Xcode project — validates the spike.

5. **Smoke test**: import `PivoxModels` from a Swift file in the main app, instantiate a `Pivox_Ai_V1_UserMessage`, print it. Confirms the whole pipeline works end to end.

**Deliverable**: `PivoxModels` package importable from the main macOS app, `import PivoxModels` resolves, types compile.

### Phase 1 — Shared C++ `AiChatClient` + C FFI bridge (~2 days)

New code under `native/core/ai_chat/client/`:

```
native/core/ai_chat/client/
├── CMakeLists.txt
├── chat_client.h              C++ public API
├── chat_client.cc             bidi stream lifecycle, grpc-cpp ClientBidiReactor
├── chat_client_c.h            Pure C FFI header (extern "C")
├── chat_client_c.cc           C wrapper delegating to the C++ client
├── dispatch_mac.h             Mac-only header for dispatch_async wrapping
├── dispatch_mac.mm            Mac-only Obj-C++ for main queue marshaling
└── dispatch_win.cc            Win-only for DispatcherQueue marshaling
```

**`chat_client.h`** — C++ API used internally:

```cpp
namespace pivox::ai_chat {

class ChatClient {
public:
    ChatClient(const std::string& endpoint);
    ~ChatClient();

    // Opens the bidi stream. Callbacks fire from a caller-chosen thread
    // (main queue on Mac, DispatcherQueue on Win).
    void StartStream(
        std::function<void(const uint8_t* bytes, size_t size)> on_event,
        std::function<void(const std::string& msg)> on_error,
        std::function<void()> on_complete);

    // Send a client event (already serialized to bytes by Swift).
    void Send(const uint8_t* bytes, size_t size);

    // Cancel the in-flight stream.
    void Cancel();
};

}
```

**`chat_client_c.h`** — C FFI, what Swift calls directly:

```c
#ifdef __cplusplus
extern "C" {
#endif

typedef struct PivoxAiChatClient PivoxAiChatClient;

PivoxAiChatClient* pivox_ai_chat_client_create(const char* endpoint);
void pivox_ai_chat_client_destroy(PivoxAiChatClient* client);

typedef void (*pivox_ai_chat_on_event)(void* ctx, const uint8_t* bytes, size_t size);
typedef void (*pivox_ai_chat_on_error)(void* ctx, const char* msg);
typedef void (*pivox_ai_chat_on_complete)(void* ctx);

void pivox_ai_chat_client_start_stream(
    PivoxAiChatClient* client,
    void* ctx,
    pivox_ai_chat_on_event on_event,
    pivox_ai_chat_on_error on_error,
    pivox_ai_chat_on_complete on_complete);

void pivox_ai_chat_client_send(
    PivoxAiChatClient* client,
    const uint8_t* bytes,
    size_t size);

void pivox_ai_chat_client_cancel(PivoxAiChatClient* client);

#ifdef __cplusplus
}
#endif
```

Pure C function pointers with an opaque `void* ctx` — the standard C FFI pattern Swift handles natively via `@convention(c)` closures.

**Internal**: the C++ `ChatClient` uses grpc-cpp's `ClientBidiReactor` (callback API, not CompletionQueue) to manage the bidi stream. Each incoming `ServerEvent` proto is serialized via `SerializeToArray()` to raw bytes, and the bytes are passed to the Swift callback via the C FFI. Outbound events come in as raw bytes from Swift, parsed into `ClientEvent` protos via `ParseFromArray()`, and sent via the reactor.

**Threading**: grpc-cpp's callback API invokes callbacks on an internal thread pool. The C++ client marshals every callback to the main queue (Mac) or DispatcherQueue (Win) before firing the C function pointer. Swift code always runs on the main thread.

**Why bytes on the C FFI, not typed pointers**: the Swift side has its own typed proto class (`Pivox_Ai_V1_ServerEvent` from `PivoxModels`), and swift-protobuf's `ParseFromArray`-equivalent (`init(serializedData:)`) is the clean way to get a typed Swift value from bytes. The typed-ness is preserved at every layer the developer actually touches — Swift app code sees typed values, C++ core uses typed C++ proto, and the wire between them is protobuf bytes (not void* — it's semantically "serialized protobuf"). No type safety is lost because swift-protobuf's `init(serializedData:)` is a parse operation with compile-time return type.

TDD: gtest coverage for `ChatClient` lifecycle, reconnect, cancel, error propagation, thread marshaling. Fake grpc server used in tests.

**Deliverable**: Shared C++ library builds. gtest passes. C FFI header compiles clean.

### Phase 2 — Swift `ChatClient` façade (~1 day)

New code under `native/platform/macos/swift/AIChat/Client/`:

```
native/platform/macos/swift/AIChat/Client/
├── ChatClient.swift           Swift facade wrapping the C FFI
├── ChatError.swift            Swift Error type for stream failures
└── ChatClientTests.swift      XCTest unit tests
```

**`ChatClient.swift`**:

```swift
import Foundation
import PivoxModels

public final class ChatClient: Sendable {
    private let handle: OpaquePointer
    private let endpoint: String

    public init(endpoint: String) throws {
        guard let handle = pivox_ai_chat_client_create(endpoint) else {
            throw ChatError.initFailed
        }
        self.handle = handle
        self.endpoint = endpoint
    }

    deinit {
        pivox_ai_chat_client_destroy(handle)
    }

    public func stream() -> AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error> {
        AsyncThrowingStream { continuation in
            let ctx = Unmanaged.passRetained(StreamContext(continuation: continuation))
            pivox_ai_chat_client_start_stream(
                handle,
                ctx.toOpaque(),
                { ctx, bytes, size in
                    let streamCtx = Unmanaged<StreamContext>
                        .fromOpaque(ctx!).takeUnretainedValue()
                    let data = Data(bytes: bytes!, count: size)
                    if let event = try? Pivox_Ai_V1_ServerEvent(serializedData: data) {
                        streamCtx.continuation.yield(event)
                    }
                },
                { ctx, msg in
                    let streamCtx = Unmanaged<StreamContext>
                        .fromOpaque(ctx!).takeRetainedValue()
                    streamCtx.continuation.finish(
                        throwing: ChatError.streamFailed(String(cString: msg!)))
                },
                { ctx in
                    let streamCtx = Unmanaged<StreamContext>
                        .fromOpaque(ctx!).takeRetainedValue()
                    streamCtx.continuation.finish()
                }
            )
            continuation.onTermination = { [handle] _ in
                pivox_ai_chat_client_cancel(handle)
            }
        }
    }

    public func send(_ event: Pivox_Ai_V1_ClientEvent) throws {
        let data = try event.serializedData()
        data.withUnsafeBytes { buf in
            pivox_ai_chat_client_send(
                handle,
                buf.bindMemory(to: UInt8.self).baseAddress,
                buf.count)
        }
    }
}

private final class StreamContext {
    let continuation: AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error>.Continuation
    init(continuation: AsyncThrowingStream<Pivox_Ai_V1_ServerEvent, Error>.Continuation) {
        self.continuation = continuation
    }
}
```

**What Swift consumer code looks like**:

```swift
let client = try ChatClient(endpoint: "grpc+tls://api.pivox.io:443")

Task {
    // Send the initial user message
    try client.send(.with {
        $0.event = .message(.with {
            $0.conversationName = "organizations/acme/conversations/abc"
            $0.parts = [.with { $0.text = "Hello" }]
        })
    })

    // Iterate the event stream
    for try await event in client.stream() {
        switch event.event {
        case .textDelta(let delta):
            // Update view state
            print(delta.delta)
        case .artifactStart(let start):
            // Open artifact pane
            print("artifact: \(start.title)")
        case .finish:
            break
        default:
            break
        }
    }
}
```

Clean. Typed. Async. No manual threading, no C++ types visible, no Obj-C++ bridge visible.

TDD: mock the C FFI functions for unit tests. Use a fake gRPC server for integration tests.

**Deliverable**: `ChatClient` Swift class usable from any consumer. All methods typed. All errors Swift-native.

### Phase 3 — Conversation and resource RPCs via grpc-swift shortcut? No.

**Decision**: the non-streaming resource RPCs (`ListConversations`, `GetConversation`, `CreateConversation`, etc.) are **also routed through the shared C++ core**, not grpc-swift.

Reason: adding grpc-swift as a second gRPC stack in the app just for unary calls introduces a parallel transport with its own auth, retry, interceptor config, and error semantics. The shared C++ core already has grpc-cpp. Sending unary calls through it is ~50 lines of C++ wrapper per RPC, and keeps one transport in one place.

New files:

```
native/core/ai_chat/client/
├── resources.h              Unary RPC wrappers
└── resources.cc             grpc-cpp unary calls, returning bytes (serialized proto)
```

**Pattern** for each RPC — C++ function accepting bytes-in, emitting bytes-out via a callback:

```cpp
void pivox_ai_chat_list_conversations(
    PivoxAiChatClient* client,
    const uint8_t* request_bytes, size_t request_size,
    void* ctx,
    void (*on_response)(void* ctx, const uint8_t* bytes, size_t size),
    void (*on_error)(void* ctx, const char* msg));
```

Swift façade method:

```swift
public func listConversations(
    _ request: Pivox_Ai_V1_ListConversationsRequest
) async throws -> Pivox_Ai_V1_ListConversationsResponse {
    let data = try request.serializedData()
    return try await withCheckedThrowingContinuation { cont in
        // FFI call with callback that yields bytes → Swift parses → cont.resume
    }
}
```

Standard template for every unary RPC. ~10 lines per method.

**TDD** covers resource method round-tripping (request → bytes → gRPC → response → bytes → typed Swift value).

**Deliverable**: All AIP resource methods (`ListConversations`, `GetConversation`, `CreateConversation`, `UpdateConversation`, `DeleteConversation`, `ListMessages`, `GetMessage`, `ListArtifacts`, `GetArtifact`, `DeleteArtifact`, `ListArtifactVersions`, `GetArtifactVersion`, `DeleteArtifactVersion`) available as typed async methods on `ChatClient`.

### Phase 4 — Storage session (~0.5 day)

Swift-side glue to call `StorageGateways.CreateStorageSession` once after Firebase login and make the resulting cookie available to `URLSession.shared` for asset fetches.

Implementation:

1. After `AuthService` reports a successful Firebase sign-in, call `ChatClient.createStorageSession()` (which routes through the shared C++ resources layer)
2. gRPC response includes the `Set-Cookie` header via response metadata
3. Parse the cookie and add it to `HTTPCookieStorage.shared` scoped to `.pivox.io` (or whatever Pivox uses)
4. Standard `URLSession.shared` requests (including `AsyncImage` loads) pick up the cookie automatically

This is existing Pivox infrastructure — `CreateStorageSession` RPC already exists, cookie flow already works for the web UI. Native app just needs to call it and install the cookie.

**Deliverable**: After login, assets hosted on storage gateways are loadable by native image controls without any custom code. `AsyncImage(url: storageURL)` just works.

### Phase 5 — Conversation list view (~1 day)

Depends on `AIElements` M0 foundations (theme, surface, disclosure style).

New files under `native/platform/macos/swift/AIChat/Views/`:

```
ConversationListView.swift       Sidebar-adjacent list of conversations
ConversationListViewModel.swift  Paginated list, loading state, error state
ConversationRow.swift            Single row in the list
NewConversationButton.swift      Button → creates a new conversation
```

**UI**:
- Scrollable list of conversation rows, grouped or sorted by `last_message_time`
- Each row shows title (or truncated first user message if no title), relative timestamp, archived/pinned state
- Pin / archive / delete via swipe-actions or a context menu
- "+ New conversation" at the top
- Empty state when the list is empty: centered copy + "Start your first conversation" button
- Loading state: shimmer rows via `AIElements` `Shimmer`
- Error state: retry button

**State management**:

```swift
@MainActor
final class ConversationListViewModel: ObservableObject {
    @Published var conversations: [Pivox_Ai_V1_Conversation] = []
    @Published var state: ListState = .idle
    private let client: ChatClient

    func load() async { ... }
    func create() async throws -> Pivox_Ai_V1_Conversation { ... }
    func delete(_ name: String) async throws { ... }
    func archive(_ name: String) async throws { ... }
}

enum ListState { case idle, loading, loaded, error(Error) }
```

Pagination: use cursor-based paging from the AIP `ListConversationsRequest.page_token`. Load the first page on mount, load more when scrolled near the bottom.

TDD: unit tests for the view model against a mock `ChatClient`. Snapshot tests for each UI state (idle/loading/loaded/error/empty).

**Deliverable**: Conversation list renders. User can create / delete / archive. Pagination works.

### Phase 6 — Conversation view (~2 days)

Depends on `AIElements` chat core components (Message, PromptInput, CodeBlock, Reasoning, Tool, Sources, Artifact framework).

New files:

```
ConversationView.swift           Main chat view for a specific conversation
ConversationViewModel.swift      Loads history, manages in-flight turn
MessageListView.swift            Scrollable message feed, auto-scroll behavior
MessageRowView.swift             Renders a Pivox_Ai_V1_Message via AIElements
StreamingCoordinator.swift       Bridges ChatClient AsyncSequence → view state
PromptInputView.swift            AIElements PromptInput wired to sendMessage
```

**`ConversationViewModel.swift`**:

```swift
@MainActor
final class ConversationViewModel: ObservableObject {
    @Published var messages: [Pivox_Ai_V1_Message] = []
    @Published var inFlight: InFlightTurn?
    @Published var state: ConversationState = .idle
    
    private let client: ChatClient
    private let conversationName: String
    private var streamTask: Task<Void, Never>?

    func loadHistory() async { ... }
    
    func send(userText: String) async {
        inFlight = InFlightTurn(kind: .user(text: userText))
        let event = Pivox_Ai_V1_ClientEvent.with {
            $0.event = .message(.with {
                $0.conversationName = self.conversationName
                $0.parts = [.with { $0.text = userText }]
            })
        }
        do {
            try client.send(event)
        } catch {
            state = .error(error)
            return
        }
        streamTask = Task { await pumpStream() }
    }
    
    private func pumpStream() async {
        do {
            for try await event in client.stream() {
                handle(event)
            }
        } catch {
            state = .error(error)
        }
    }
    
    private func handle(_ event: Pivox_Ai_V1_ServerEvent) {
        switch event.event {
        case .textStart: inFlight?.assistantText = ""
        case .textDelta(let d): inFlight?.assistantText?.append(d.delta)
        case .textEnd: commitInFlight()
        case .toolInputStart, .toolInputDelta, .toolInputAvailable: handleToolCall(event)
        case .artifactStart, .artifactDelta, .artifactEnd, .artifactError: handleArtifactEvent(event)
        case .finish: commitInFlight()
        case .error(let e): state = .error(ChatError.serverError(e))
        default: break
        }
    }
    
    func cancel() {
        streamTask?.cancel()
        inFlight = nil
    }
}

struct InFlightTurn {
    enum Kind { case user(text: String), assistant }
    var kind: Kind
    var assistantText: String?
    var activeToolCalls: [String: Pivox_Ai_V1_ToolCall] = [:]
}

enum ConversationState { case idle, streaming, error(Error) }
```

**Streaming rendering**: `MessageListView` observes `viewModel.messages` + `viewModel.inFlight`. In-flight assistant text renders as a "live" message at the bottom of the list, updating on every text-delta. When `textEnd` fires, the in-flight state is committed to `messages` and cleared. SwiftUI's view diffing handles the transition smoothly.

**Auto-scroll**: matches ai-elements `Conversation` behavior — sticks to bottom unless the user has scrolled up. Tracked via a `userScrolled` flag in the view model, toggled by a `ScrollView` position observer.

**Prompt input**: `AIElements.PromptInput` with `onSubmit` wired to `viewModel.send`. Disabled while `state == .streaming`. Cancel button visible during streaming.

TDD: unit tests for `ConversationViewModel.handle(_:)` against fixture `ServerEvent`s covering every oneof case. Snapshot tests for `MessageListView` in idle / streaming / error states. XCUITest for the full send-message → see-response flow against a mocked ChatClient.

**Deliverable**: A user can pick a conversation, see history, send a message, and watch the response stream in live.

### Phase 7 — Tool call UI (~1 day)

Depends on `AIElements.Tool` component (M1 of the AIElements plan).

New files:

```
ToolCallCard.swift               Renders an in-progress or completed tool call
ClientToolRegistry.swift         Swift-side registry for client-executable tools
ClientToolExecutor.swift         Dispatches tool calls to registered handlers
```

**`ClientToolRegistry.swift`** — same shape as the AIElements composition contract:

```swift
@MainActor
public final class ClientToolRegistry: ObservableObject {
    private var handlers: [String: (Data) async throws -> Data] = [:]
    
    public func register<Input: Decodable, Output: Encodable>(
        _ name: String,
        _ handler: @escaping (Input) async throws -> Output
    ) {
        handlers[name] = { inputBytes in
            let input = try JSONDecoder().decode(Input.self, from: inputBytes)
            let output = try await handler(input)
            return try JSONEncoder().encode(output)
        }
    }
    
    public func dispatch(name: String, input: Data) async throws -> Data {
        guard let handler = handlers[name] else {
            throw ChatError.unknownTool(name)
        }
        return try await handler(input)
    }
}
```

**Flow**:

1. Server streams `ToolInputAvailable` for a client tool
2. Stream closes cleanly
3. `ConversationViewModel` notices the tool call, asks `ClientToolRegistry` to execute
4. Registry looks up the handler, runs it (may be async — UI tool might show a dialog or pick a file)
5. Handler returns encoded output
6. View model builds `ClientEvent.toolOutput` and calls `client.send(...)`
7. New stream opens, model continues with the tool result in history

**v1 registry is empty** — no real tools. Test harness registers a `server_time` tool or similar trivial thing to validate the end-to-end loop.

Tool call rendering: `AIElements.Tool` with states mapped from `Pivox_Ai_V1_ToolCall.state`. User sees "Calling tool: X..." during execution, result expanded below on completion.

TDD: unit tests for `ClientToolRegistry` (register, dispatch, error paths). Integration test registers a test tool and validates the full turn → tool call → result → continuation loop.

**Deliverable**: Tool call UI renders. Test tool registered and exercised end-to-end.

### Phase 8 — Artifact rendering (inline only for v1) (~1 day)

Depends on `AIElements.Artifact` primitives (M1) and the renderer registry.

Files:

```
ArtifactPaneView.swift           HSplitView with chat on left, artifact on right
ArtifactStreamCoordinator.swift  Bridges ArtifactStart/Delta/End/Error events to ArtifactStore
ChatArtifactRegistry.swift       Pre-registers inline renderers for v1 types
```

**v1 artifact types** (all inline text):
- `code` → `AIElements.CodeArtifactView` (tree-sitter highlight)
- `markdown` → `AIElements.MarkdownArtifactView`
- `svg` → `AIElements.SvgArtifactView` (Skia via shared core)
- `html` → deferred (web content, M4 of AIElements plan)

**Flow**:
1. Stream emits `ArtifactStart` with type + title
2. Coordinator creates an `Artifact` in the local `ArtifactStore`
3. Stream emits `ArtifactDelta` events with text content
4. Store updates, artifact pane re-renders (debounced)
5. Stream emits `ArtifactEnd`, store commits the version
6. User can copy / download / iterate via chat

**Asset-backed artifacts** are not exercised in v1. The `AIElements` `ArtifactRegistry` has the dispatch path for them, and if an asset-backed version ever arrives, the registry handles it — but no v1 code path produces them.

Snapshot tests for each artifact type in various states.

**Deliverable**: User asks model for code, code streams into the artifact pane live.

### Phase 9 — Sidebar wiring + app integration (~0.5 day)

Final integration step:

1. Add "AI Assistant" (or "Chat") item to the existing sidebar navigation in `ContentView.swift`
2. Route to `ConversationListView` → `ConversationView`
3. Keyboard shortcut to open (e.g., `⌘⇧A`)
4. Empty-state handling when no conversations exist
5. Proper `@StateObject` / `@EnvironmentObject` wiring for `ChatClient` singleton scoped to the app's lifetime
6. Handle app-level auth state: if Firebase not signed in, chat UI hidden; on login, `CreateStorageSession` fires

TDD: XCUITest flows cover sign-in → navigate to chat → create conversation → send message → receive reply → open another conversation → delete one → sign out.

**Deliverable**: The feature is accessible from the main app. End-to-end flow works against a running Go BE + Ollama.

### Phase 10 — Error handling and polish (~1 day)

Specific edge cases that deserve explicit handling:

1. **Stream disconnect mid-turn**: show "Connection lost, retrying..." in the in-flight message area. Auto-retry with backoff. If retry fails, surface error with a "Retry" button.
2. **Model error** (`ServerEvent.error`): show the error message inline in the conversation, not as a modal. Consumer can retry the turn.
3. **Cancel mid-stream**: cancel button in the prompt input area during streaming. Cancellation drops the stream, shows the partial response as "canceled", allows the user to send a new message.
4. **Auth token expired**: Firebase auto-refreshes. If that fails, route user to sign-in.
5. **Network offline**: detect via `NWPathMonitor`, show an offline banner at the top of the chat view, queue the next send for when back online (optional — could also just show error).
6. **Empty model response** (turn finishes with no text): show "The model didn't respond" placeholder.
7. **Very long messages**: virtualized list rendering via SwiftUI `LazyVStack`. Already default behavior.

TDD: dedicated test cases for each error condition with mock `ChatClient`.

**Deliverable**: Feature is robust against realistic failure modes. No crashes, no frozen UI, no lost user input.

### Phase 11 — Manual Ollama smoke test + ship

End-to-end test against a real running Go BE + Ollama + qwen3-vl. Not in CI — manual before merging.

Scenarios:
1. Simple question → text response streams in
2. Multi-turn conversation → history persists, each turn loads correctly
3. Code generation → artifact pane opens with highlighted code
4. Tool registered → model calls the tool → result streams back
5. Cancel mid-stream → partial response preserved, can send another
6. Close the app and reopen → conversations persist, can resume

**Deliverable**: Ready to ship. All phases green, manual smoke test passes.

## Test strategy

Per `CLAUDE.md`: TDD everywhere. Tests before implementation.

| Layer | Framework | Coverage |
|---|---|---|
| Shared C++ `ChatClient` | gtest | Unit tests on lifecycle, reconnect, cancel, thread marshaling. Fake grpc server. |
| Swift `ChatClient` façade | XCTest + mock FFI | Unit tests on async method dispatch, event parsing, error propagation |
| View models | XCTest + mock `ChatClient` | Unit tests on state transitions, streaming, tool dispatch, error handling |
| SwiftUI views | swift-snapshot-testing | Snapshot tests for every view in every state (idle, loading, streaming, error, empty) |
| Feature flows | XCUITest | End-to-end sign-in → chat → send → receive → cancel → delete |
| Integration | Mock server harness | Full gRPC round-trip without needing a real Go BE |
| Manual smoke | Real Go BE + Ollama | Pre-merge validation |

Baseline snapshot tests land with each view. XCUITest flows land with Phase 9 (sidebar wiring). Feature test count target: ~150 tests across unit + snapshot + UI for the chat feature alone.

## Risks

1. **Swift-to-C FFI memory management** — `Unmanaged.passRetained` / `takeRetainedValue` patterns are easy to get wrong. Double-release or leak. Mitigation: dedicated unit tests on the FFI lifecycle with a leak-checking harness. Fail CI on leaks.
2. **grpc-cpp's `ClientBidiReactor` thread model** — the newer callback API has had subtle thread-reentrancy bugs in older versions. Mitigation: pin a known-good grpc-cpp version, add explicit thread-hop tests.
3. **SwiftUI streaming re-render cost** — updating `inFlight.assistantText` on every `textDelta` could cause excessive re-renders with long responses. Mitigation: batch updates via a `TimelineView` or debounce to ~10Hz. Measure before optimizing.
4. **Auth cookie propagation** — `HTTPCookieStorage.shared` does pick up cookies automatically, but cookie scoping must match the actual Pivox domain. Test against real DNS during Phase 4.
5. **PivoxModels regeneration on proto changes** — when the Go BE adds a field to a proto, `buf generate` must emit new Swift types before the macOS build can consume them. Build pipeline needs to run buf before CMake. Mitigation: add buf generate to the pre-build step.
6. **CMake + SPM integration for the first time** — the M0 spike from the AIElements plan validates this. If that spike hasn't been done yet, this entire plan blocks on it. Sequence carefully.

## Out of scope for v1

Deferred to follow-up phases (captured here so nothing is lost):

- **HTML / mermaid / jsx-preview artifact rendering** (requires WKWebView — M4 of the AIElements plan)
- **Image / pdf / chart / table artifact rendering** (requires asset-backed artifact paths + AIElements M4/M1)
- **Conversation sharing / multi-user** (server-side feature first)
- **Pivox-specific client tools** (`open_view`, `edit_asset`, `focus_channel`, etc. — land in phase 2 as concrete tool implementations registered against the v1 registry)
- **Search** across conversations (requires full-text search infrastructure)
- **Export** conversation to Markdown / PDF (AIElements has a `ConversationDownload` primitive; wiring it up is a small follow-up)
- **Voice input** (`SpeechInput` component from AIElements — M2)
- **Offline mode** / local conversation cache
- **Multi-window support** for side-by-side conversations
- **Inline chat in context** (e.g., inside the rundown editor) — v1 is a dedicated sidebar view only

## Execution order when you give go

1. Validate Phase 0 dependencies (Swift 6 spike done, buf.gen.yaml ready, CMake SPM path validated)
2. Phase 0 — `PivoxModels` package
3. Phase 1 — Shared C++ `ChatClient` + C FFI
4. Phase 2 — Swift `ChatClient` façade
5. Phase 3 — Resource RPC wrappers
6. Phase 4 — Storage session wiring
7. Phases 5–8 — UI (parallelizable with integration tests)
8. Phase 9 — Sidebar integration
9. Phase 10 — Error handling
10. Phase 11 — Manual smoke test, ship

Estimated total: **~10 working days** for a careful TDD pass. Could compress meaningfully if patterns fall into place quickly.

## Things to verify before starting

Same methodology as the Go BE plan — most unknowns resolve in minutes of implementation. None are blocking:

- Exact CMake wiring for `PivoxModels` in the Xcode project (spike it, see what works)
- Whether `grpc-cpp`'s `ClientBidiReactor` version in the Pivox vcpkg baseline works cleanly (pin if needed)
- The exact Firebase auth → cookie scope mapping (inspect during Phase 4)
- Whether `swift-snapshot-testing` is already available (Part of the AIElements M0 spike; confirm before Phase 5)
