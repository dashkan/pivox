# AIElements — Implementation Plan

> **Auth note (2026): Firebase removed — Keycloak-only.** This is a
> Native App (macOS/Windows) plan; the Native App is now a
> legacy/reference target. Any Firebase reference below (e.g. the
> "Firebase-emulator-style" native test setup) reflects the abandoned
> native auth — the cloud is Keycloak-only (`internal/oidc`). See
> `AGENTS.md` for the current auth model.

## Engineering principles — non-negotiable

**No hacky code. Ever.** Applies to every line of this implementation and every PR.

No `// TODO: fix properly later`. No `// HACK:`. No silent fallbacks or swallowed errors. No magic constants tuned until tests pass. No duplicated logic because the clean path seemed hard. No retry loops masking races. No platform branches that hide design flaws. Local refactors and reverting unmerged work proceed freely. **Sweeping changes — touching files outside the current feature area, changing public interfaces, modifying merged work, crossing package boundaries, or rewriting >200 lines of existing working code — require user consultation before starting.**

**Complex engineering issues get consulted with Gemini before implementation.** When implementation hits a problem where the obvious path leads somewhere hacky, stop, understand the root cause, prompt Gemini for a sounding-board on the clean solution, capture the outcome in an adjacent design note, then implement cleanly. Applies to cross-language interop edge cases, race conditions, build system issues, API design tradeoffs, performance cliffs, library limitations. Standard well-trodden patterns don't need consultation.

## TL;DR

Build a native `AIElements` component library that gives Pivox parity with Vercel's ai-elements, interpreted natively per platform.

- **Primary deliverable**: macOS implementation (SwiftUI), fully built and tested.
- **Secondary deliverable**: docs + per-component prompts for the WinUI 3 (C++/WinRT) port, produced at the end of the macOS work.
- **Visual direction**: compact density, platform-appropriate shape language, native typography, SF Symbols on macOS / Fluent System Icons on WinUI. Decomposition follows the ai-elements Figma; visual look is reinterpreted natively — not shadcn on desktop.
- **Architectural pattern**: shared C++ core for pure logic (models, parsing, rendering of SVG and markdown AST), native Swift / C++/WinRT views on top. WKWebView/WebView2 introduced in a dedicated later phase for inherently-web content types only (HTML artifacts, KaTeX math, Mermaid, WebPreview, JSX Preview); everything else stays fully native. No CEF involvement — that boundary stays with the template preview / design canvas subsystem.
- **Testing**: TDD end-to-end. gtest for shared core, `swift-snapshot-testing` + XCUITest for SwiftUI. Write tests first.
- **Scope**: **all** ai-elements components. Nothing dropped — phased instead. The three most deferrable (Persona, Voice Selector, JSX Preview) land in the last phase and can be cut if time presses without affecting the rest of the library.
- **Estimated effort**: ~14 focused weeks for macOS + handoff docs. Windows port is separate, agent-driven work.

## Scope

All Vercel ai-elements components are in, phased across milestones. Nothing is dropped except workflow-editor components (Canvas, Node, Edge) — no use case in Pivox.

### In

**Chat core** (M1): Conversation, Message, PromptInput, CodeBlock, Reasoning, Tool, Sources, Suggestion, Task, Sandbox, Agent, StackTrace, Artifact framework.

**Input & media** (M2): Attachments, Terminal, FileTree, Image, ModelSelector, MicSelector, SpeechInput, Transcription, AudioPlayer.

**Layout & status** (M3): Snippet, Shimmer, Plan, Queue, ChainOfThought, Context, Checkpoint, Commit, Confirmation, EnvironmentVariables, OpenIn, PackageInfo, InlineCitation, TestResults, SchemaDisplay, SuggestionInput. (Panel, Connection, Controls, Toolbar removed — React Flow-only.)

**WebView-dependent content** (M4): HTML artifact type, KaTeX math in markdown, Mermaid in markdown and as artifact type, WebPreview. These all share the WKWebView/WebView2 infrastructure introduced in M4.

**Deferred trio — last phase** (M5): Persona (Rive via C++ runtime + Skia backend), Voice Selector (AVSpeechSynthesisVoice / Windows.Media.SpeechSynthesis), JSX Preview (offline esbuild + React in WKWebView, built on M4 infrastructure).

**Artifact types**: code, markdown, image, svg, pdf, table, chart, json-tree (native, M1) + html, jsx-preview, mermaid (WebView-backed, M4/M5).

### Out

- **Canvas / Node / Edge** — React Flow-style workflow editor. No Pivox use case. Reintroduce later if a concrete need emerges.
- **Panel / Connection / Controls / Toolbar** — ai-elements ships these as React Flow helpers (panel positioned on a canvas, bezier connection line, zoom/fit controls, `NodeToolbar` attached to graph nodes). They have no meaning without the Canvas. Dropped alongside Canvas/Node/Edge. General-purpose equivalents (splitter, action-button row) are built as Foundation primitives if/when needed, not as ai-elements components.
- **3D / video Artifact types** — out of scope for chat artifacts.
- **Sandbox code execution** — UI shell lands in M1; actual code execution is a separate product.

### Dropping policy

Composition contract is intentionally strict: we ship every ai-elements sub-component with the same name and slot role. Drops happen only when:

1. **The component has no standalone meaning** (React Flow helpers without the Canvas). These drop as a group.
2. **A sub-component genuinely can't be expressed natively on the target platform.** None found in the audit — every sub-component across the library maps cleanly to SwiftUI primitives (`NSPopover` for hover cards, `TabView(.page)` for carousels, `@ViewBuilder` generics for polymorphic wrappers, searchable `List` in `Popover` for command palettes, `AVCaptureDevice.DiscoverySession` for device pickers, etc.).

Any future drop must have a documented reason of one of those two types. Platform convenience is not a valid drop reason — if SwiftUI can express it, we build it.

### Native-primitive exception to the composition contract

The composition contract has two axes, and they're scoped differently:

| Axis | Scope | Decision |
|---|---|---|
| **What components exist** | Cross-platform (shared) | Product decision. Both platforms ship the same set |
| **How each component is built** | Per-platform (independent) | Implementation decision. Each platform picks composition vs. native primitive based on what its OS offers |

Drops of entire components (like Canvas/Node/Edge) are shared — the product doesn't change per platform. Decomposition collapsing (like AudioPlayer → `AVPlayerView`) is per-platform, because it depends on whether the OS provides a suitable native control. macOS and WinUI may arrive at different answers for the same component, and that is fine. Consumer-facing API shapes may diverge per platform — the only shared contract is the data model (Artifact, UIMessage, streaming events, markdown block list, SVG rendering) and the set of components that exist.

**Default**: follow the composition contract on a given platform — same sub-components as ai-elements, same names.

**Exception**: when a component has a **full native OS counterpart on that platform** — one native control that provides the *entire* component's look and behavior, not just a piece of it — that platform collapses to a thin wrapper around the native control and drops the sub-component decomposition.

All three criteria must hold on a given platform to invoke the exception:

1. A real native control exists on the target platform
2. It provides the full component behavior users recognize (transport bar + scrubber + time + volume + AirPlay for AudioPlayer; selectable searchable PDF for PDF; sortable resizable columns for Table; interactive charts for Chart)
3. Reimplementing the ai-elements decomposition wouldn't produce anything better — it would just give us a worse-looking version of what the OS already ships

Goal: native look + behavior on each platform. The composition contract serves that goal 95% of the time. When it would fight native affordances instead (the AudioPlayer case on macOS), the exception applies — per platform.

**macOS choices — components that qualify for the native-primitive exception on macOS** (listed explicitly — anything not listed follows the default on macOS):

| Component | Native primitive on macOS | Collapse |
|---|---|---|
| **AudioPlayer** | `AVKit.AVPlayerView` | 11 sub-components → 1 wrapper. Ships with full transport, scrubber, time, volume, AirPlay, Now Playing integration |
| **Image** | SwiftUI `AsyncImage` | Already 1 component |
| **PdfArtifactView** | `PDFKit.PDFView` | Already 1 wrapper. Free text selection, search, thumbnail sidebar, annotations |
| **TableArtifactView** | SwiftUI `Table` | Already 1 wrapper. Native sort + column resize |
| **ChartArtifactView** | SwiftUI `Charts` | Already 1 wrapper. Interactive, accessible, animated |

**WinUI choices — to be determined at porting time.** The Windows implementer evaluates each of the components above (plus any others) against criteria 1–3 using Windows-native primitives (`MediaPlayerElement`, `Windows.Data.Pdf`, `DataGrid`, etc.) and independently decides whether to collapse or compose. The handoff doc for each component records the macOS decision and explicitly leaves the Windows decision open — the Windows implementer is not required to match macOS on this axis.

Likely Windows outcomes based on what each OS offers:

| Component | Likely Windows outcome | Reason |
|---|---|---|
| **AudioPlayer** | Collapse to `MediaPlayerElement` wrapper | WinUI ships a full-featured media player control with transport, scrubber, volume. Equivalent to AVPlayerView |
| **Image** | Collapse to `Image` / `BitmapImage` wrapper | Native primitive, trivial |
| **PdfArtifactView** | Probably compose — `Windows.Data.Pdf` only renders pages to bitmaps; no text selection, no search, no thumbnails | Windows implementer may build chrome around page renders, or use a third-party PDF viewer |
| **TableArtifactView** | Collapse to `DataGrid` or `ItemsRepeater` wrapper | `CommunityToolkit.WinUI.UI.Controls.DataGrid` provides sort + resize |
| **ChartArtifactView** | **Compose.** No native WinUI chart control. Implementer picks: custom Direct2D, Win2D, OxyPlot, ScottPlot, or a CommunityToolkit Labs chart. Guided by the shared `ChartData` contract + macOS reference screenshots |
| **Terminal** | Likely **compose**. No native terminal view on WinUI. Microsoft.Terminal.Core exists but is heavy/coupled. Implementer may build on top of `RichTextBlock` with shared-core ANSI parser, or vendor a lighter library |

**Hybrid native-primitive-in-content pattern** (macOS decisions — WinUI may differ):

Some components have a native control for their *content* but not their *chrome*. On these, macOS keeps the ai-elements decomposition and collapses only the content sub-component to a native wrapper:

| Component | Decomposition | macOS native primitive inside |
|---|---|---|
| **Terminal** | Terminal, TerminalHeader, TerminalTitle, TerminalStatus, TerminalActions, TerminalCopyButton, TerminalClearButton, **TerminalContent** | `TerminalContent` internally wraps `SwiftTerm` (already in Pivox stack) instead of hand-rolling ANSI rendering |
| **WebPreview** | WebPreview, WebPreviewNavigation, WebPreviewNavigationButton, WebPreviewUrl, **WebPreviewBody**, WebPreviewConsole | `WebPreviewBody` internally wraps `WKWebView` |

WinUI may pick different primitives for these content slots — e.g., `WebPreviewBody` on WinUI wraps `WebView2` instead of WKWebView; `TerminalContent` on WinUI may use a different library or compose from primitives.

## Composition contract (load-bearing design principle)

**For every ai-elements component, `AIElements` ships the same sub-component decomposition, with the same names, the same slot roles, and the same nesting structure.** Implemented in SwiftUI on macOS and C++/WinRT XAML on Windows.

Only two things change from the React original:

1. **Visual rendering** — native primitives, native materials, native icons, compact density, platform-appropriate shape
2. **Idiom** — `@ViewBuilder` closures on SwiftUI, `<Children>` + inherited `DataContext` on XAML

Component counts stay the same. API shape stays the same. Slot names stay the same. Per-sub-component props stay the same.

### Authoritative reference

`.claude/skills/ai-elements/scripts/*.tsx` is the source of truth for each component's sub-component tree. When porting, read the JSX, write the equivalent SwiftUI / XAML tree. If the JSX has `<MessageBranchPrevious>`, SwiftUI gets a `MessageBranchPrevious` view with the same props; the XAML equivalent follows from the SwiftUI implementation.

### Example — same tree, three dialects

React / ai-elements:

```tsx
<Tool>
  <ToolHeader type="tool-weather" state={part.state} />
  <ToolContent>
    <ToolInput input={part.input} />
    <ToolOutput output={<MessageResponse>{text}</MessageResponse>}
                errorText={part.errorText} />
  </ToolContent>
</Tool>
```

SwiftUI:

```swift
Tool {
    ToolHeader(type: "tool-weather", state: part.state)
    ToolContent {
        ToolInput(input: part.input)
        ToolOutput(errorText: part.errorText) {
            MessageResponse(text)
        }
    }
}
```

C++/WinRT XAML:

```xml
<ai:Tool>
    <ai:ToolHeader Type="tool-weather" State="{x:Bind Part.State}" />
    <ai:ToolContent>
        <ai:ToolInput Input="{x:Bind Part.Input}" />
        <ai:ToolOutput ErrorText="{x:Bind Part.ErrorText}">
            <ai:MessageResponse Text="{x:Bind Text}" />
        </ai:ToolOutput>
    </ai:ToolContent>
</ai:Tool>
```

### Shared state between parents and children

When a parent (e.g. `PromptInput`) manages state that children (e.g. `PromptInputSubmit`, `PromptInputTextarea`, `PromptInputAttachmentsDisplay`) need to read or mutate, each platform uses the native ambient-context mechanism:

- **SwiftUI**: parent provides a state object via `.environment(\.promptInput, context)`, children read via `@Environment(\.promptInput)`. Direct map of React Context.
- **C++/WinRT XAML**: parent sets a typed view-model as `DataContext`; children inherit it through the XAML tree and bind to its properties. More verbose but same semantics.

No props drilling, no globals. Same mental model as React Context in both cases.

### Implications

- **If ai-elements has N sub-components for a component, `AIElements` has N sub-components** with the same names and responsibilities. Not collapsed into a monolith, not fragmented, not renamed.
- **Consumers compose with the same mental model they'd use in React.** If you know ai-elements, you know `AIElements`.
- **The Windows port has zero API design work.** The WinUI implementer reads `.claude/skills/ai-elements/scripts/*.tsx`, reads the corresponding SwiftUI file under `platform/macos/swift/AIElements/Components/`, writes the XAML equivalent. No decisions to re-make about decomposition, slot names, or props.
- **Tests verify the contract.** Every component's snapshot tests mirror the JSX examples from `.claude/skills/ai-elements/scripts/`, ensuring the trees compose the same way.

## Architecture

```
┌────────────────────────────────────────────────────────────────┐
│  SwiftUI views (platform/macos/swift/AIElements/)              │
│  - Components, Foundation, Bridge                               │
└────────────────────────────────────────────────────────────────┘
                            │
            Obj-C++ shim (platform/macos/objcpp/AIElementsBridge)
                            │
┌────────────────────────────────────────────────────────────────┐
│  Shared C++ core (native/core/ai_elements/)                     │
│  - Data models (UIMessage, MessagePart, ToolUIPart, Artifact)   │
│  - Markdown: cmark-gfm → typed block list + streaming fixer     │
│  - Highlighter: tree-sitter → token span list                   │
│  - SVG renderer: Skia + SkSVGDOM → SkPicture → replay           │
│  - Helpers: ANSI parser, autoscroll policy, transcription math, │
│    stack-trace parser, chart data types                         │
└────────────────────────────────────────────────────────────────┘
```

- **Shared C++ core** holds everything that is pure logic or rendering of portable formats. Written once, tested once in gtest, linked on both platforms.
- **Platform layer** (macOS for this work) holds the SwiftUI components, the theme, native surfaces (Liquid Glass), native device integrations (AVFoundation, Speech.framework, PDFKit), and the Obj-C++ bridge to the shared core.
- **WinUI layer** (future, handoff) mirrors the platform layer in C++/WinRT, re-using the shared core as-is. Visual parity is the responsibility of the Windows implementer against a documented reference.

### Why Skia for SVG

Skia's `SkSVGDOM` covers ~95% of real-world SVGs (it's what Chrome uses to rasterize SVG). Direct2D's built-in SVG support is a subset — fine for simple shapes, breaks on filters, complex gradients, most Illustrator/Figma exports. SwiftDraw on the Mac side has similar coverage gaps.

Using Skia on both sides gives us:
- Guaranteed parity — one parser, one rasterizer, identical output
- Shared snapshot tests in C++ gtest, trusted across platforms
- Futureproofing — anything Skia supports, we support

Costs:
- ~15–20 MB added to each platform build
- vcpkg integration work for Skia (Skia uses `gn` natively; vcpkg has a port, finicky but works)
- Resize-aware replay plumbing on both platforms:
  - Mac: `NSViewRepresentable` hosting `CAMetalLayer` → Skia Metal backend → `SkPicture` replay on size change
  - Win: `SwapChainPanel` + Skia D3D backend → `SkPicture` replay on size change

Non-SVG visual output (chat bubbles, code, markdown, charts) does NOT go through Skia — those use native primitives (SwiftUI views, SwiftUI Charts, AttributedString, etc.).

## Design language

No existing Figma or tokens. Drafting ours during foundations, using ai-elements' Figma (https://www.figma.com/community/file/1555856339129556457) as a decomposition reference only — not a visual reference.

### Principles

- **Compact density**. Pro-tool spacing, not consumer chat spacing. Row heights ~28–32pt, inline spacing 4–8pt.
- **Native typography**. SF on macOS via `.system(.body, .callout, .footnote, .caption)`. No custom web fonts.
- **Native materials**. Liquid Glass on macOS 26+, `.ultraThinMaterial` fallback. Apply to floating surfaces only — prompt input, popovers, conversation background. Not inline message bubbles.
- **Native focus rings, native hover states, native selection colors.** No custom focus rings.
- **Flat rows for disclosures**. Custom `DisclosureGroupStyle` removes the Mac-y chevron chrome.
- **Rounded but restrained**. 8pt default corner radius, 12pt for elevated surfaces, 4pt for inline chips.
- **SF Symbols for iconography**. Maintain a small icon mapping file so component code writes `Icon(.sparkles)` and each platform picks its native glyph. No custom SVG icon set.

### Tokens (initial proposal, to iterate)

```
Colors (semantic, light/dark aware):
  background, surface, surfaceRaised, surfaceFloating
  border, borderStrong
  contentPrimary, contentSecondary, contentMuted
  accent, accentMuted
  success, warning, danger
  selection, focusRing

Spacing scale: 2, 4, 6, 8, 12, 16, 24, 32 pt
Radius scale:  2, 4, 6, 8, 12, 16 pt
Typography:    title, headline, body, callout, subheadline, footnote, caption
Motion:        instant (0ms), fast (120ms), base (200ms), slow (320ms)
```

Implemented as SwiftUI `EnvironmentValues` with a `@Environment(\.aiElementsTheme)` accessor. WinUI port will implement the same token shape as a XAML resource dictionary.

## Dependencies

### Shared C++ core (via vcpkg)

Add to `native/vcpkg.json` — introduced across phases as needed:

- `skia` (M0) — 2D graphics + SVG rendering (SkSVGDOM) + Rive C++ runtime backend in M5
- `cmark-gfm` (M0) — markdown parser (CommonMark + GFM)
- `tree-sitter` + `tree-sitter-highlight` (M0) — syntax highlighter

Already present:
- `gtest` — test framework

**M5 note**: Rive's C++ runtime renders on top of Skia, so M5's Persona work reuses the M0 Skia integration rather than adding a separate renderer dep. Rive runtime source itself gets added as a submodule or vcpkg overlay port (no upstream vcpkg port exists — small overlay to write).

### macOS (via SPM)

First SPM additions to the project. SPM integration into the CMake-generated Xcode project is the first foundation spike — verify it works before committing to more deps.

- `swift-snapshot-testing` (M0, PointFreeCo) — snapshot tests for SwiftUI components

No Swift-side dep on Skia directly. Skia sits behind the Obj-C++ bridge.

### Native frameworks used

**M0–M3**: `SwiftUI`, `AppKit` (via `NSViewRepresentable`), `AVFoundation`, `Speech`, `PDFKit`, `Charts`, `MetalKit`, `QuartzCore`.

**M4 adds**: `WebKit` (`WKWebView`) for HTML artifact, math rendering via offline KaTeX, Mermaid rendering, and WebPreview. Configured air-gapped by default via `WKWebViewConfiguration` + strict CSP injection for inline HTML.

**M5 adds**: `AVFoundation` (`AVSpeechSynthesisVoice.speechVoices()`) for Voice Selector (already linked); `WKWebView` reused for JSX Preview with offline `esbuild-wasm` + React runtime bundled as app resources.

All first-party, all OS-provided — no additional runtime deps shipped with Pivox.

## Foundations phase (~1.5 weeks)

Build before any component. Every visible component depends on at least one foundation piece.

### FN-1: Design tokens + theme (1 day)
- `Theme.swift` with all tokens above as an `ObservableObject` or `EnvironmentKey`
- Light/dark adaptive via `Color(nsColor:)` semantic tokens
- `.environment(\.aiElementsTheme, .default)` at the root
- **Tests**: unit tests asserting token resolution in light/dark

### FN-2: Glass / surface primitive (0.5 day)
- `Surface.swift` with `.surface(.base|.raised|.elevated|.floating)` modifier
- Elevated/floating = Liquid Glass on macOS 26+, `.ultraThinMaterial` fallback
- Base/raised = solid background from tokens
- Reuses the existing `GlassCard` pattern

### FN-3: Tooltip primitive (0.5 day)
- `Tooltip.swift` — NSPopover-backed tooltip with content + shortcut chip
- `.aiTooltip(content:shortcut:side:)` view modifier
- Handles hover delay, position, dismissal

### FN-4: Disclosure style (0.5 day)
- `FlatDisclosureStyle.swift` — custom `DisclosureGroupStyle` matching ai-elements' flat row + chevron
- Used by Reasoning, Tool, Task, Sandbox, Agent, Sources, StackTrace, FileTree, etc.

### FN-5: Markdown pipeline (3 days)
- **Shared core** (`native/core/ai_elements/markdown/`):
  - `cmark_gfm_wrapper.cc` — parse to typed block list (Paragraph, Heading, List, CodeBlock, Table, Quote, Image, HorizontalRule)
  - `incomplete_markdown_fixer.cc` — Streamdown-style stream-repair (close unclosed fences, lists, emphasis)
  - Typed block list: `std::variant<Paragraph, Heading, ...>` emitted across the bridge
  - **Tests (gtest, written first)**: CommonMark spec compliance, GFM tables/task lists, streaming repair
- **macOS renderer** (`Foundation/Markdown/MarkdownView.swift`):
  - `MarkdownView` SwiftUI view that consumes the block list from the bridge
  - Inline runs → `AttributedString` with theme styling
  - Block items (code, table, image) → sibling SwiftUI views
  - **Tests (snapshot)**: fixtures covering every block type in light/dark

### FN-6: Syntax highlighter (2 days)
- **Shared core** (`native/core/ai_elements/highlight/`):
  - `tree_sitter_highlighter.cc` — wraps `tree-sitter-highlight` for a chosen grammar set: swift, typescript, javascript, python, rust, go, c, cpp, sh, json, yaml, toml, sql, html, css, markdown, xml
  - Output: typed token span list (`{start, length, kind}`) across the bridge
  - Theme map: token kind → theme color token
  - **Tests (gtest, written first)**: per-language fixture tests asserting expected token spans
- **macOS consumption**: `CodeBlockView` consumes spans and builds an `AttributedString`

### FN-7: Skia integration + SVG renderer (2 days)
- Wire Skia via vcpkg. First verify the vcpkg port builds cleanly in Pivox's CMake setup
- `native/core/ai_elements/svg/skia_svg_renderer.cc`:
  - `parse(svg_string) -> SkPicture` (parse-once, replay-many)
  - `bounds() -> SkRect`
- **macOS bridge**:
  - `SkiaSVGLayer` — `NSViewRepresentable` hosting `CAMetalLayer` with Skia's Metal backend
  - `SkPicture` replay on size change via `NSView.viewDidChangeFrame`
  - Accepts either an SVG string or a parsed `SkPicture` handle
- **Tests**:
  - gtest: parse fixtures, assert bounds, snapshot-test rendered pixel output against reference PNGs
  - Swift snapshot: `SkiaSVGLayer` rendering a fixture SVG at multiple sizes

### FN-8: SPM wiring spike (0.5 day)
- Add `swift-snapshot-testing` as the first SPM dep to prove SPM-in-CMake-Xcode works
- If the CMake-generated project can't consume SPM cleanly, resolve before proceeding

### FN-9: Snapshot test infrastructure (0.5 day)
- `make test-native-aielements` target orchestrates Firebase-emulator-style setup
- Register snapshot test baselines under `native/tests/macos/AIElements/Snapshot/__Snapshots__/`
- Run under `xcodebuild test` (required for AppKit view snapshotting)

### FN-10: Shared helpers in core (1 day)
- `ansi_parser.cc` — for Terminal
- `autoscroll_policy.cc` — "follow bottom?" logic for Conversation
- `transcription_segments.cc` — active/past/future math
- `stack_trace_parser.cc` — common stack trace formats
- `ui_message.h` — `UIMessage`, `MessagePart` variant, `ToolUIPart` states matching the AI SDK shape
- `artifact.h` — Artifact type/version/content data model
- All tests written first in gtest

**Foundations total: ~11 days.**

## Component waves

Components land in dependency order. Each wave depends on the previous. TDD throughout: snapshot tests for every component before implementation, gtest for any non-trivial logic in shared core.

### Wave 1 — Chat core (~2.5 weeks)

Priority order within the wave:

1. **Message** (+ Content, Actions, Response, Branch, Toolbar) — depends on FN-5 markdown. First component; sets the visual tone, gets the most review.
2. **Conversation** (+ Content, EmptyState, ScrollButton, Download) — depends on Message + FN-10 autoscroll.
3. **PromptInput** (+ Textarea, Submit, ActionMenu, Select, Button, Body, Header, Footer, Tools, AttachmentsDisplay, HoverCard, Command) — the big one.
   - `AutosizingTextView`: `NSViewRepresentable` wrapping `NSTextView` with autosize, key handling (`Enter` submit, `Shift+Enter` newline, `⌘+Enter` alt), paste image, drop target
   - Global drop via `NSWindow.contentView.registerForDraggedTypes`
   - Slash-menu as a popover anchored to the text view
4. **CodeBlock** (+ Header, Title, Filename, Actions, CopyButton, LanguageSelector) — depends on FN-6 highlighter.
5. **Reasoning** (+ Trigger, Content) — depends on FN-4 disclosure + FN-5 markdown + streaming state.
6. **Tool** (+ Header, Content, Input, Output) — depends on FN-4 disclosure + `ToolUIPart` model + `CodeBlock` for JSON input display.
7. **Sources** — depends on FN-4 disclosure.
8. **Suggestion / Suggestions** — trivial, chip buttons.
9. **Task** (+ Trigger, Content, Item, ItemFile) — FN-4 disclosure + SF Symbols.
10. **Sandbox** (+ Header, Content, Tabs, TabBar, TabsList, TabsTrigger, TabContent) — FN-4 disclosure + `CodeBlock`.
11. **Agent** (+ Header, Content, Instructions, Tools, Tool, Output) — FN-5 markdown + `CodeBlock`.
12. **StackTrace** (+ Frame) — depends on `stack_trace_parser` in core.
13. **Artifact framework** (+ Pane, Header, Renderer, per-type renderers — see Artifacts section).

### Wave 2 — Input & media (~1.5 weeks)

14. **Attachments** (+ Attachment, Preview, Info, Remove) — drag/drop, NSItemProvider, image thumbnails.
15. **Terminal** (+ Header, Title, Status, Actions, ClearButton, CopyButton, Content) — depends on `ansi_parser`.
16. **FileTree** — `OutlineGroup` + custom `DisclosureGroupStyle`.
17. **Image** — `AsyncImage` + skeleton + retry.
18. **ModelSelector** — searchable popover (Menu is not searchable on macOS).
19. **MicSelector** — `AVCaptureDevice.DiscoverySession`. Requires `NSMicrophoneUsageDescription` + sandbox entitlement (check `Pivox.entitlements`).
20. **SpeechInput** — `SFSpeechRecognizer` + `AVAudioEngine`. Requires `NSSpeechRecognitionUsageDescription`. Auto-restart loop for on-device 1-minute sessions.
21. **Transcription** (+ Segment) — controlled state + active highlight math from `transcription_segments`.
22. **AudioPlayer** — native-primitive exception. Single wrapper around `AVKit.AVPlayerView` via `NSViewRepresentable`. Ships with full transport bar, scrubber, time, volume, AirPlay, Now Playing integration for free. The ai-elements decomposition (Element, ControlBar, PlayButton, SeekBackward/Forward, TimeDisplay, TimeRange, DurationDisplay, MuteButton, VolumeRange) is explicitly dropped per the native-primitive exception — rebuilding it would produce a worse version of what AVKit already provides.

### Wave 3 — Layout & status (~1.5 weeks)

Note: Panel, Connection, Controls, Toolbar were removed in the audit — all four were React Flow-only helpers (`@xyflow/react` `Panel`, custom connection lines, React Flow `Controls`, React Flow `NodeToolbar`). Without the Canvas they have no purpose.

23. **Snippet** — mono text + copy button, built on an input-group primitive (Snippet, SnippetAddon, SnippetText, SnippetInput, SnippetCopyButton).
24. **Shimmer** — `LinearGradient` mask + repeating animation. Generic `@ViewBuilder` wrapper replaces React's polymorphic `as` prop.
25. **Plan** — collapsible card with streaming shimmer (Plan, PlanHeader, PlanTitle, PlanDescription, PlanTrigger, PlanContent, PlanFooter, PlanAction).
26. **Queue** — message/todo list with collapsible sections (Queue, QueueSection, QueueSectionTrigger, QueueSectionLabel, QueueSectionContent, QueueList, QueueItem, QueueItemIndicator, QueueItemContent, QueueItemDescription, QueueItemActions, QueueItemAction, QueueItemAttachment, QueueItemImage, QueueItemFile).
27. **ChainOfThought** — stepped reasoning with search-result badges and images (ChainOfThought, ChainOfThoughtHeader, ChainOfThoughtStep, ChainOfThoughtSearchResults, ChainOfThoughtSearchResult, ChainOfThoughtContent, ChainOfThoughtImage).
28. **Context** — token-usage gauge with hover-card breakdown (Context, ContextTrigger, ContextContent, ContextContentHeader, ContextContentBody, ContextContentFooter, ContextInputUsage, ContextOutputUsage, ContextReasoningUsage, ContextCacheUsage).
29. **Checkpoint** — conversation restore marker (Checkpoint, CheckpointIcon, CheckpointTrigger).
30. **Commit** — git commit card with expandable file changes. 22 sub-components including CommitHeader, CommitAuthor, CommitAuthorAvatar, CommitInfo, CommitMessage, CommitMetadata, CommitHash, CommitSeparator, CommitTimestamp, CommitActions, CommitCopyButton, CommitContent, CommitFiles, CommitFile, CommitFileInfo, CommitFileStatus, CommitFileIcon, CommitFilePath, CommitFileChanges, CommitFileAdditions, CommitFileDeletions.
31. **Confirmation** — tool approval flow (Confirmation, ConfirmationTitle, ConfirmationRequest, ConfirmationAccepted, ConfirmationRejected, ConfirmationActions, ConfirmationAction).
32. **EnvironmentVariables** — masked key/value editor with visibility toggle (EnvironmentVariables, EnvironmentVariablesHeader, EnvironmentVariablesTitle, EnvironmentVariablesToggle, EnvironmentVariablesContent, EnvironmentVariable, EnvironmentVariableGroup, EnvironmentVariableName, EnvironmentVariableValue, EnvironmentVariableCopyButton, EnvironmentVariableRequired).
33. **OpenIn** — dropdown menu to open a query in external AI tools (OpenIn, OpenInTrigger, OpenInContent, OpenInChatGPT, OpenInClaude, OpenInT3, OpenInScira, OpenInv0, OpenInCursor, OpenInItem, OpenInLabel, OpenInSeparator). Native name is `OpenIn`, not `OpenInChat`.
34. **PackageInfo** — version change card (PackageInfo, PackageInfoHeader, PackageInfoName, PackageInfoChangeType, PackageInfoVersion, PackageInfoDescription, PackageInfoContent, PackageInfoDependencies, PackageInfoDependency).
35. **InlineCitation** — hover-card citation with carousel. 14 sub-components (InlineCitation, InlineCitationText, InlineCitationCard, InlineCitationCardTrigger, InlineCitationCardBody, InlineCitationCarousel, InlineCitationCarouselContent, InlineCitationCarouselItem, InlineCitationCarouselHeader, InlineCitationCarouselIndex, InlineCitationCarouselPrev, InlineCitationCarouselNext, InlineCitationSource, InlineCitationQuote). Carousel via SwiftUI `TabView(.page)`.
36. **TestResults** — test suite tree with per-test status and error details. 17 sub-components including TestResults, TestResultsHeader, TestResultsSummary, TestResultsDuration, TestResultsProgress, TestResultsContent, TestSuite, TestSuiteName, TestSuiteStats, TestSuiteContent, Test, TestStatus, TestName, TestDuration, TestError, TestErrorMessage, TestErrorStack.
37. **SchemaDisplay** — REST API endpoint doc. 12 sub-components including SchemaDisplay, SchemaDisplayHeader, SchemaDisplayMethod, SchemaDisplayPath, SchemaDisplayDescription, SchemaDisplayContent, SchemaDisplayParameters, SchemaDisplayParameter, SchemaDisplayRequest, SchemaDisplayResponse, SchemaDisplayProperty, SchemaDisplayExample.
38. **SuggestionInput** — typeahead; wrap `NSComboBox` or build a custom popover-on-text-field.

**Wave totals: ~5.5 weeks for all components.**

## Native Artifacts

### Architecture

```
Shared core (native/core/ai_elements/artifacts/)
  artifact.h            Artifact{type, id, title, language, payload, version}
  artifact_store.{h,cc} Versioned in-memory store, patches from streaming deltas
  artifact_patcher.cc   Applies artifact-delta messages (line-by-line streaming)
  chart_data.{h,cc}     ChartData{type, series, labels, theme tokens}

macOS (platform/macos/swift/AIElements/Components/Artifact/)
  Artifact.swift            Composable primitive: Artifact, ArtifactHeader,
                            ArtifactContent, ArtifactActions slots.
                            Same composition shape as ai-elements.
  ArtifactPane.swift        Optional layout convenience: HSplitView wrapper
                            that mounts an Artifact + chat alongside.
  ArtifactRegistry.swift    Type string → view factory dispatch
  Renderers/
    CodeArtifactView        → CodeBlock
    MarkdownArtifactView    → MarkdownView
    ImageArtifactView       → AsyncImage + zoom gesture
    SvgArtifactView         → SkiaSVGLayer (from FN-7)
    PdfArtifactView         → PDFKit PDFView
    TableArtifactView       → SwiftUI Table (sort/resize native)
    ChartArtifactView       → SwiftUI Charts (BarMark/LineMark/AreaMark/PointMark/SectorMark)
    JsonTreeArtifactView    → SchemaDisplay
```

### UI surface — full ai-elements decomposition

Per the composition contract, every ai-elements sub-component ships with the same name and slot role. Eight public views for the Artifact primitive:

| View | Role |
|---|---|
| `Artifact` | Root container |
| `ArtifactHeader` | Holds title, description, actions, close |
| `ArtifactTitle` | Primary title text |
| `ArtifactDescription` | Secondary descriptive text (subtitle, timestamp, version label) |
| `ArtifactActions` | Container for action buttons |
| `ArtifactAction` | Individual action button (icon + optional label, tooltip, handler) |
| `ArtifactClose` | Close / dismiss button |
| `ArtifactContent` | Body; dispatches to the registered renderer via `ArtifactRegistry` |

No toolbar concept, no separate surfaces — one composable tree. Whether an `ArtifactAction` runs a local function or dispatches a prompt back to the chat is a handler concern, not a structural one:

```swift
Artifact {
    ArtifactHeader {
        ArtifactTitle(artifact.title)
        ArtifactDescription("v\(store.currentVersion(artifact.id)) of \(store.history(artifact.id).count)")
        ArtifactActions {
            // Local — immediate, no AI involved
            ArtifactAction(icon: .copy, tooltip: "Copy") {
                pasteboard.setString(artifact.content)
            }
            ArtifactAction(icon: .download, tooltip: "Download") {
                savePanel.show(artifact)
            }
            VersionSelector(store: store, id: artifact.id)

            // AI-directed — same slot, handler sends a prompt back
            ArtifactAction(icon: .sparkles, tooltip: "Make concise") {
                chat.sendMessage("Please make this more concise")
            }
            ArtifactAction(icon: .wand, tooltip: "Add comments") {
                chat.sendMessage("Add comments explaining each function")
            }
        }
        ArtifactClose { store.hide(artifact.id) }
    }
    ArtifactContent {
        ArtifactRegistry.default.render(artifact)
    }
}
```

Signatures match ai-elements — `Artifact`, `ArtifactHeader`, `ArtifactTitle`, `ArtifactDescription`, `ArtifactActions`, `ArtifactAction`, `ArtifactClose`, `ArtifactContent`. Same names, same nesting, same composition. Nothing is dropped because of platform — SwiftUI handles all of these cleanly as plain `View` structs with `@ViewBuilder` content slots.

### Streaming

First-class, driven by SSE markers from the chat transport above `AIElements`. Not all artifact types stream — some buffer until complete. That distinction is declared per-renderer at registration time, not hardcoded by type.

#### SSE marker contract

The streaming layer speaks four events; shared-core `ArtifactStore` maps 1:1. The transport above `AIElements` is responsible for parsing the SSE stream and calling the right store method.

| SSE event | Payload | `ArtifactStore` call | `ChangeKind` fired |
|---|---|---|---|
| `artifact_start` | `{id, type, title, language?, metadata?}` | `create(Artifact{...})` | `Created` |
| `artifact_delta` | `{id, content}` or `{id, op, offset, length, content}` | `append(id, chunk)` or `apply_delta(id, delta)` | `Appended` or `Patched` |
| `artifact_end` | `{id}` | `commit(id)` | `Committed` |
| `artifact_error` | `{id, error}` | `fail(id, error)` | `Failed` |

Two payload shapes are supported and may be mixed within a single stream:

1. **Append-only**: each delta's `content` is raw bytes appended to the payload. Claude's typical pattern. Model "types" the artifact.
2. **Delta / patch**: each delta is a structured edit (`{op: insert|delete|replace, offset, length, content}`). Used when the model edits an existing artifact mid-stream.

Renderers don't care which was used — they just react to `ChangeKind`.

#### Shared C++ core API

```cpp
// native/core/ai_elements/artifacts/artifact_store.h
namespace pivox::ai_elements {

enum class ChangeKind { Created, Appended, Patched, Committed };

struct ArtifactDelta {
    enum class Op { Insert, Delete, Replace };
    Op op;
    size_t offset;
    size_t length;
    std::vector<uint8_t> content;
};

class ArtifactStore {
public:
    void create(Artifact draft);
    void append(std::string id, std::span<uint8_t const> chunk);
    void apply_delta(std::string id, ArtifactDelta delta);
    void commit(std::string id);       // locks current state as a new version

    std::optional<Artifact> current(std::string id) const;
    std::vector<Artifact> history(std::string id) const;

    using Listener = std::function<void(Artifact const&, ChangeKind)>;
    using Token = uint64_t;
    Token subscribe(Listener);
    void unsubscribe(Token);
};

}
```

Listeners fire synchronously on whatever thread called `append/apply_delta/commit`. Platform layers marshal to UI thread (`@MainActor` on Swift, `DispatcherQueue.TryEnqueue` on C++/WinRT).

#### Registry — minimal API

One registration shape. Type string, view factory, one boolean for whether the type can render partial payloads.

```swift
public final class ArtifactRegistry {
    public func register<V: View>(
        type: String,
        supportsStreaming: Bool = true,
        @ViewBuilder renderer: @escaping (Artifact) -> V
    )
}
```

That's the whole API. Renderers that need to throttle expensive re-parses do it internally with `.onChange(of:)` or `.task(id:)` — SwiftUI's diffing makes this cheap and per-type handling is an implementation detail of each renderer, not a library concern.

`supportsStreaming: false` flips one behavior: while the artifact is in the store's streaming state, `ArtifactContent` shows a generic shimmer placeholder instead of invoking the renderer. The renderer is only called once `commit` fires.

#### Built-in type streaming support

| Type | `supportsStreaming` | Notes |
|---|---|---|
| **code** | `true` | Renderer re-highlights on each update via `.onChange(of: artifact.payload)`; tree-sitter runs on the main actor debounced by SwiftUI's natural diff rate |
| **markdown** | `true` | `IncompleteMarkdownFixer` runs in the renderer before cmark-gfm |
| **table** | `true` | Renderer appends rows from each update directly into SwiftUI `Table` |
| **chart** | `true` | Streaming JSON parser in the renderer; re-renders when a complete object is parseable |
| **json-tree** | `true` | Streaming JSON parser in the renderer |
| **html** (M4) | `true` | `IncompleteHtmlFixer` in the renderer before `navigateToString` |
| **jsx-preview** (M5) | `true` | esbuild + React re-render in hidden WKWebView |
| **svg** | `false` | SVG is XML — partial XML is a parse error |
| **image** | `false` | Binary blob |
| **pdf** | `false` | Binary blob |
| **mermaid** (M4) | `false` | Layout requires full input |

#### What a non-streamable artifact looks like during a stream

```
SSE: artifact_start {id:"a1", type:"svg", title:"Flowchart"}
     → create → Created → UI shows shimmer placeholder
                          ("Generating SVG…", shimmer bar, optional byte count)

SSE: artifact_delta {id:"a1", content:"<svg..."}
     → append → Appended → UI stays on placeholder (supportsStreaming: false)

SSE: artifact_delta {id:"a1", content:"</svg>"}
     → append → Appended → still placeholder

SSE: artifact_end {id:"a1"}
     → commit → Committed → swap placeholder for SvgArtifactView →
                             Skia parses → SkPicture cached → drawn at target size
```

For a streamable type:

```
SSE: artifact_start → Created → empty CodeBlock
SSE: artifact_delta → Appended → renderer re-highlights → "typing" effect
SSE: artifact_delta → Appended → renderer re-highlights
...
SSE: artifact_end → Committed → final highlight → version stamped
```

#### Placeholder UX for non-streamable types

`ArtifactContent` shows a generic streaming placeholder when `supportsStreaming: false` and the store reports the artifact is streaming:

- Type-appropriate icon (from the type's registered icon glyph)
- Title from `artifact_start`
- Animated shimmer bar
- Optional current payload size ("124 KB so far")

Consumers can opt out of the default placeholder by wrapping their renderer:

```swift
r.register(type: "my-type", supportsStreaming: false) { artifact in
    MyCustomView(artifact: artifact)
        .overlay {
            if ArtifactContext.isStreaming {
                MyCustomStreamingPlaceholder(artifact: artifact)
            }
        }
}
```

Most consumers just use the default.

#### Version history

When `commit()` is called, the current payload is frozen as a new version entry with timestamp + metadata. The store keeps an immutable LRU history (default 50, configurable).

`ArtifactHeader` exposes a version selector (prev/next + "v3 of 5") that lets users step through committed versions. The currently-displayed version is independent of the "live" (still-streaming) version, which is marked separately.

Mirrors Claude's artifact UX:
- Watch code being written live
- Model signals done → becomes `v1`
- Model edits → new stream of chunks → new `commit` → `v2`
- User steps between versions via the header selector

#### SwiftUI integration

```swift
struct ArtifactPane: View {
    @ObservedObject var store: ArtifactStoreObservable
    let artifactId: String

    var body: some View {
        let artifact = store.current(artifactId)
        VStack(spacing: 0) {
            ArtifactHeader {
                Text(artifact.title)
                Spacer()
                if store.isStreaming(artifactId) {
                    Shimmer().frame(width: 48)
                }
                VersionSelector(
                    versions: store.history(artifactId),
                    current: store.currentVersion(artifactId)
                ) { store.showVersion(artifactId, $0) }
                ArtifactActions { /* copy, download */ }
            }
            ArtifactContent {
                ArtifactRegistry.default.render(artifact)
            }
        }
    }
}
```

`ArtifactStoreObservable` is a Swift wrapper around the C++ `ArtifactStore`, firing `objectWillChange` on the main queue when the core listener fires. Per-type renderers (e.g. `CodeArtifactView`) internally debounce expensive re-parsing.

#### Cross-platform parity

Shared core is identical — streaming logic, delta application, version management all live in C++. The macOS and WinUI platform layers each observe via their native async-dispatch primitives and update their per-type renderers. `streaming-contracts.md` in the M6 handoff documents the exact debounce intervals, repair strategies, and marshaling patterns for the Windows implementer to match.

### Chart Artifact

Uses **SwiftUI Charts directly** on macOS — no intermediate format. Shared core only holds `ChartData` (the cross-platform data contract). Windows implementer picks their own rendering path (OxyPlot, ScottPlot, custom Direct2D, or CommunityToolkit Labs) guided by the data contract and Mac reference screenshots in the handoff doc.

### SVG Artifact

Uses **Skia SVG renderer** from FN-7. Parse once → `SkPicture` → replay at target size on resize. Both platforms use the same renderer via shared core; output is byte-identical. This is the primary justification for putting Skia in the stack.

## Testing strategy (TDD throughout)

Per `CLAUDE.md`: all new code uses TDD. Tests first, then implementation. No exceptions.

### Shared C++ core (gtest)

Runs under `ctest` as part of the existing Pivox test infrastructure.

- **Markdown**: CommonMark spec subset + GFM tables/task lists + streaming repair edge cases. Fixture-driven.
- **Highlighter**: per-language fixtures asserting token spans at known offsets.
- **SVG renderer**: parse fixtures → pixel snapshot against reference PNG. Fails on any visual regression.
- **Helpers**: ANSI parser, autoscroll policy, transcription math, stack-trace parser — each with unit tests.
- **Artifact store**: create/delta/version-step/restore flows.
- **Chart layout**: data → normalized chart data (no rendering, just math).

### SwiftUI components (swift-snapshot-testing)

One snapshot test file per component. Each file covers:
- Default state
- All meaningful variants (states, content types, densities)
- Light and dark
- Narrow and wide

Baselines live in `native/tests/macos/AIElements/Snapshot/__Snapshots__/`. Changes require visual review.

Runs under `xcodebuild test` (AppKit views require a windowed host — `swift test` alone is insufficient).

### Behavior (XCUITest)

Reserved for components with real interaction: PromptInput (key handling, paste, drop, slash menu), Conversation (autoscroll following, scroll button), Artifact (version stepping), SpeechInput (permissions flow).

### CI integration

Add `make test-aielements` target chaining `ctest --label-regex ai_elements` + `xcodebuild test -scheme PivoxAIElementsSnapshotTests`. Wire into the existing warnings-as-errors posture (`-Wall -Wextra -Werror`, `SWIFT_TREAT_WARNINGS_AS_ERRORS`).

## Build system

### CMake

Add to `native/CMakeLists.txt`:
- `add_subdirectory(core/ai_elements)` — new library target `pivox_ai_elements_core`
- Link Skia, cmark-gfm, tree-sitter from vcpkg
- Export headers for the Obj-C++ bridge

Add to `native/core/ai_elements/CMakeLists.txt`:
- Source globs per subdirectory (markdown, highlight, svg, artifacts, helpers)
- gtest targets with `add_test(NAME ai_elements_... COMMAND ...)` entries
- Public include dir for the bridge header

### Xcode / SwiftPM

The existing macOS target adds:
- `AIElements` folder under `platform/macos/swift/` (recursive glob via the existing CMake Swift source pattern — verify whether globbing is in place first; if not, explicit file listing)
- New SPM package dep: `swift-snapshot-testing` (test-only)
- New Obj-C++ bridge files under `platform/macos/objcpp/AIElementsBridge.{h,mm}`

### vcpkg

Add to `native/vcpkg.json`:
```json
{
  "dependencies": [
    "gtest",
    "skia",
    "cmark-gfm",
    "tree-sitter",
    "tree-sitter-highlight"
  ]
}
```

Skia via vcpkg is known to be finicky. **First build will be the foundation-phase spike** — if it blows up, the fallback is to pre-build Skia outside vcpkg and link it manually.

## File layout

```
native/
├── core/
│   └── ai_elements/
│       ├── CMakeLists.txt
│       ├── models/
│       │   ├── ui_message.{h,cc}
│       │   ├── tool_ui_part.{h,cc}
│       │   └── artifact.{h,cc}
│       ├── markdown/
│       │   ├── cmark_gfm_wrapper.{h,cc}
│       │   ├── block_list.{h,cc}
│       │   └── incomplete_markdown_fixer.{h,cc}
│       ├── highlight/
│       │   ├── tree_sitter_highlighter.{h,cc}
│       │   └── token_span.{h,cc}
│       ├── svg/
│       │   └── skia_svg_renderer.{h,cc}
│       ├── artifacts/
│       │   ├── artifact_store.{h,cc}
│       │   ├── artifact_patcher.{h,cc}
│       │   └── chart_data.{h,cc}
│       └── helpers/
│           ├── ansi_parser.{h,cc}
│           ├── autoscroll_policy.{h,cc}
│           ├── transcription_segments.{h,cc}
│           └── stack_trace_parser.{h,cc}
│
├── platform/macos/
│   ├── swift/AIElements/
│   │   ├── Foundation/
│   │   │   ├── Theme.swift
│   │   │   ├── Surface.swift
│   │   │   ├── Tooltip.swift
│   │   │   ├── FlatDisclosureStyle.swift
│   │   │   ├── Icon.swift
│   │   │   ├── Markdown/
│   │   │   │   └── MarkdownView.swift
│   │   │   ├── Highlight/
│   │   │   │   └── CodeHighlighter.swift
│   │   │   └── SVG/
│   │   │       └── SkiaSVGLayer.swift
│   │   ├── Models/
│   │   │   └── Bridge.swift       Swift mirrors of shared-core types
│   │   └── Components/
│   │       ├── Conversation/
│   │       ├── Message/
│   │       ├── PromptInput/
│   │       ├── CodeBlock/
│   │       ├── Reasoning/
│   │       ├── Tool/
│   │       ├── Task/
│   │       ├── Sandbox/
│   │       ├── Agent/
│   │       ├── StackTrace/
│   │       ├── Artifact/
│   │       │   └── Renderers/
│   │       ├── Terminal/
│   │       ├── FileTree/
│   │       ├── PromptInput/
│   │       └── ...one folder per component
│   └── objcpp/
│       ├── AIElementsBridge.h
│       └── AIElementsBridge.mm
│
└── tests/
    ├── core/
    │   └── ai_elements/
    │       ├── markdown_tests.cc
    │       ├── highlight_tests.cc
    │       ├── svg_tests.cc
    │       ├── artifact_store_tests.cc
    │       └── helpers_tests.cc
    └── macos/
        └── AIElements/
            ├── Snapshot/
            │   ├── __Snapshots__/
            │   ├── MessageSnapshotTests.swift
            │   ├── ConversationSnapshotTests.swift
            │   └── ...one per component
            └── UI/
                ├── PromptInputUITests.swift
                ├── ConversationUITests.swift
                └── ArtifactUITests.swift
```

## Milestones

Each milestone closes a coherent slice. Foundations (M0) is non-negotiable — everything downstream depends on it working. M5 is structured to be cuttable if time presses: everything shippable lands by M4.

| # | Milestone | Scope | Duration |
|---|---|---|---|
| **M0** | **Foundations** | FN-1 through FN-10: theme, surface, tooltip, disclosure style, markdown pipeline, highlighter, Skia SVG, SPM wiring, snapshot infra, core helpers, Artifact framework + ArtifactStore + registry. All gtest tests passing, first snapshot test landing on a stub component | ~1.5 weeks |
| **M1** | **Chat core + native artifacts** | Wave 1: Message, Conversation, PromptInput, CodeBlock, Reasoning, Tool, Sources, Suggestion, Task, Sandbox, Agent, StackTrace. Artifact renderers: code, markdown, image, svg (via FN-7 Skia), pdf (PDFKit), table (SwiftUI Table), chart (SwiftUI Charts), json-tree. Streaming + version selector working end-to-end | ~2.5 weeks |
| **M2** | **Input & media** | Wave 2: Attachments, Terminal, FileTree, Image, ModelSelector, MicSelector, SpeechInput, Transcription, AudioPlayer | ~1.5 weeks |
| **M3** | **Layout & status** | Wave 3: Snippet, Shimmer, Plan, Queue, ChainOfThought, Context, Checkpoint, Commit, Confirmation, EnvironmentVariables, OpenIn, PackageInfo, InlineCitation, TestResults, SchemaDisplay, SuggestionInput | ~1.5 weeks |
| **M4** | **WebView-dependent content** | Introduces the WKWebView stack (air-gapped by default). Lands: HTML artifact type, KaTeX math in markdown (offline KaTeX → image), Mermaid in markdown and as artifact type (offline mermaid.js → image), WebPreview (URL loader with browser chrome). All share one WKWebView foundation | ~2 weeks |
| **M5** | **Deferred trio (last phase)** | Persona (Rive C++ runtime + Skia backend, reuses M0 Skia), Voice Selector (AVSpeechSynthesisVoice), JSX Preview (offline esbuild + React in WKWebView, extends M4) | ~2 weeks |
| **M6** | **Handoff docs + prompts** | Generate `docs/ai/components/*.md`, `windows-prompts/*.md`, `windows-implementation-guide.md`, `design-tokens.md`, `shared-core.md`, `behavior-contracts.md`, `svg-renderer-notes.md`, `chart-spec.md`, `webview-integration.md`, `streaming-contracts.md` | ~1 week |

**Total: ~12 weeks.** Honest estimate. M5 is structured to be cuttable without affecting anything earlier — a complete, shippable chat UI exists at end-of-M4.

## Handoff deliverables (M5)

At M5, produce the full Windows handoff package under `docs/ai/`:

```
docs/ai/
├── plan.md                          (this file)
├── README.md                        overview + how to use this directory
├── design-tokens.md                 all tokens + macOS → WinUI semantic mapping
├── shared-core.md                   inventory of native/core/ai_elements/ —
│                                    public API surface, test coverage, build
├── behavior-contracts.md            cross-platform invariants every component
│                                    must satisfy (keyboard, focus, a11y, streaming)
├── svg-renderer-notes.md            Skia integration parity between Mac and Win
│                                    (Metal backend vs D3D backend, SwapChainPanel)
├── chart-spec.md                    chart types, ChartData schema, visual reference
│                                    screenshots from macOS implementation
├── windows-implementation-guide.md  XAML primitive mapping, RichTextBlock quirks,
│                                    SplitView patterns, vcpkg/build system,
│                                    gotchas discovered during macOS work
├── components/
│   ├── conversation.md              per-component: shape, props, slots, states,
│   ├── message.md                   behavior, keyboard/focus, edge cases, pointer
│   ├── prompt-input.md              to macOS implementation, and an explicit
│   │                                "macOS choice / Windows decision open" note for
│   │                                the composition-vs-native axis. Windows
│   │                                implementer picks their own path.
│   └── ...one per component
└── windows-prompts/
    ├── 00-foundations.md            self-contained prompt for each piece.
    ├── 01-conversation.md           each prompt references the relevant
    ├── 02-message.md                components/*.md + shared-core.md.
    │                                Prompts never dictate composition vs native —
    │                                they evaluate the criteria against Windows
    │                                primitives and let the implementer decide.
    └── ...
```

Per-component docs written as each component lands (not retroactively) so real gotchas get captured while they're fresh. Windows prompts written at M5, sized to be self-contained (each prompt can be handed to a fresh agent without prior context).

## Risks

1. **Skia via vcpkg** — known to be finicky. Spiked during FN-7. Fallback: pre-build Skia outside vcpkg, link manually, document the process for the Windows side to replicate.
2. **SPM-in-CMake-Xcode** — Pivox's macOS target is CMake-generated, not a hand-authored Xcode project. SPM integration is the first foundation spike (FN-8). Fallback: vendor `swift-snapshot-testing` source, skip SPM entirely.
3. **Markdown streaming repair** — the incomplete-markdown fixer is non-trivial. Edge cases around nested lists and code fences mid-stream. Written with aggressive test coverage first.
4. **AppKit view snapshotting** — requires `xcodebuild test` with a windowed test host. If the existing test setup uses `swift test` only, need to add a snapshot-test xctest target. Addressed in FN-9.
5. **Skia Metal backend integration with SwiftUI** — `CAMetalLayer` inside `NSViewRepresentable` with size observation and replay is standard but not trivial. Spiked in FN-7 with the first SVG rendering test.
6. **PromptInput text view** — the most complex single component. `NSTextView` autosize, key handling, paste image, drop, slash menu. Allot extra time within M1.
7. **Warnings-as-errors** — `-Wall -Wextra -Werror` and `SWIFT_TREAT_WARNINGS_AS_ERRORS` are on. Third-party vcpkg sources may generate warnings. Isolate them behind `SYSTEM` includes in CMake.
8. **Scope creep** — ai-elements has ~40 components. Per `CLAUDE.md`: don't add features beyond what's asked. Stick to the wave list above. Anything new goes in a follow-up.
9. **WKWebView sandbox hardening (M4)** — HTML artifact rendering inline model-generated HTML opens a data exfiltration surface if not locked down. Mitigations: air-gapped configuration (`WKWebViewConfiguration.websiteDataStore = .nonPersistent()`), content blockers on all network types, strict CSP injection (`default-src 'none'; style-src 'unsafe-inline'`), `allowsContentJavaScript = false` unless required, no `WKScriptMessageHandler` exposed from the inline-HTML webview. Documented in detail for the Windows side — WebView2 has equivalents.
10. **Offline KaTeX / Mermaid bundling (M4)** — KaTeX and Mermaid need to be bundled as app resources so the WKWebView can load them without network. KaTeX is ~300 KB, Mermaid is ~2 MB. Bundled into `Resources/WebContent/` and loaded via `loadHTMLString(_, baseURL:)` pointing at the bundle.
11. **Rive source availability (M5)** — Persona needs `.riv` files for the 6 variants (obsidian, mana, opal, halo, glint, command). Vercel's are on GitHub; license check required before shipping. Fallback: build one custom SwiftUI/Canvas variant per state, ship a single Pivox-branded persona instead of 6 ai-elements variants. Scope decision in M5.
12. **Rive C++ runtime integration** — no upstream vcpkg port. Small overlay port to write, or submodule the Rive runtime and build it via our CMake. Spike at the start of M5 before committing to the Persona work.
13. **esbuild-wasm in WKWebView (M5)** — JSX Preview needs a JSX→JS compiler running in the webview. esbuild-wasm is ~8 MB bundled. Live compile latency on typical components is 50–200 ms, debounce to 200 ms. Alt: pre-compile JSX via a native Swift transformer using swift-syntax-based JSX parser (fork territory, not recommended).

## Open decisions (small, non-blocking)

These don't block the start of foundations. Resolve as they come up.

1. **Initial token values** — I'll propose concrete colors, spacing, fonts during FN-1. You steer.
2. **Chart types in phase 1** — proposing bar, line, area, scatter, pie. Others (candlestick, heatmap, box plot) added on demand.
3. **Tree-sitter grammar set** — proposing swift, ts, js, py, rust, go, c, cpp, sh, json, yaml, toml, sql, html, css, md, xml. Add more if users need them.
4. **Artifact version history limit** — proposing 50 versions in-memory, then LRU eviction. Configurable.
5. **Snapshot test variance tolerance** — starting with strict (0% tolerance). May need to loosen if font metrics vary across macOS minor versions.

## Locked decisions (reference)

From the planning conversation:

- No WebKit, no WebView, no CEF involvement in AIElements
- Skia + SkSVGDOM for SVG rendering on **both** platforms (when Windows lands)
- cmark-gfm for markdown parsing in shared core
- tree-sitter for syntax highlighting in shared core
- SwiftUI Charts on macOS for chart artifacts (not Skia, not SVG)
- SF Symbols on macOS (hybrid with Fluent on Windows via a shared icon mapping file)
- SwiftDraw is NOT used (superseded by Skia-on-both decision)
- Native Artifacts support: code, markdown, image, svg, pdf, table, chart, json-tree
- Drop list (see Scope > Out)
- macOS 15 deployment target (confirmed — SwiftUI Charts available)
- Module name: `AIElements`
- Compact density
- Windows implementation deferred to handoff docs + prompts at M5
- TDD throughout, per `CLAUDE.md`
- Warnings-as-errors enforced
