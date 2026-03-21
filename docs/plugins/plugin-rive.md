# Pivox Rive Plugin — 2D Motion Graphics Engine

## Overview

Rive is Pivox's visual animation engine for designer-created 2D motion graphics. It provides a browser-based visual design tool (rive.app) and a high-performance C/C++ runtime that renders animations at broadcast frame rates.

Rive fills the gap between "developer writes JavaScript animations" and "we build a WYSIWYG editor" — designers get a professional animation tool on day one.

**Built-in plugin** — ships with Pivox, built on the same Plugin SDK as CEF and FFmpeg.

**Related docs:**
- `docs/plugins/plugin-sdk.md` — Plugin SDK, protocol, shared memory
- `docs/tooling.md` — designer workflow, template scaffolding
- `docs/sdk.md` — JavaScript SDK (for CEF/WASM alternative path)

## Why Rive

| Need | Without Rive | With Rive |
|---|---|---|
| Complex animation (stingers, branded transitions) | Developer writes JS/CSS by hand | Designer creates visually in rive.app |
| Motion graphics (animated logos, show opens) | Developer codes in Three.js or GSAP | Designer animates in rive.app |
| Data-driven animation (score reveals, vote counters) | Developer writes custom JS | Designer builds state machine, data binds to inputs |
| Template iteration | Change code → rebuild → test | Change animation → export → test |

## Plugin Capabilities

```
PluginCapabilities:
  name: "Rive C++ Runtime"
  version: <rive-runtime version>
  type: GRAPHICS

  supports_load: true
  supports_play: true       # triggers in-animation state
  supports_stop: true       # triggers out-animation state
  supports_update: true     # sets state machine inputs (data binding)
  supports_next: true       # can advance state machine
  supports_seek: false
  supports_variable_speed: false
  supports_loop: true       # state machine can loop

  outputs_video: true       # RGBA frames with alpha
  outputs_audio: false      # Rive doesn't produce audio
  outputs_alpha: true       # full alpha channel support
  outputs_captions: false

  supported_formats: [".riv"]
```

## Runtime — C/C++ Native (Day One)

The Rive plugin uses the [rive-runtime](https://github.com/rive-app/rive-runtime) C/C++ library. This is the same core runtime used by Rive's iOS, Android, and desktop products.

**Why native C++ over WASM:**
- Rive's value is complex animations — exactly the workload where WASM overhead matters
- Native runtime renders directly to GPU texture (Metal, Vulkan, OpenGL)
- No browser compositing overhead — frame goes straight from Rive renderer to shared memory
- Simple animations don't need Rive (CSS/JS handles them) — Rive exists for the hard stuff

**GPU renderer backends:**

| Platform | Backend | Notes |
|---|---|---|
| macOS (dev) | Metal | Native, optimal |
| Linux (prod) | Vulkan or OpenGL | Vulkan preferred if available |
| Windows (prod/dev) | D3D11/D3D12 or Vulkan | Multiple options |

**Integration architecture:**

```
┌──────────────────────────────────────────────────────┐
│  Channel Process                                      │
│                                                       │
│  ┌────────────────────────────────────────────────┐  │
│  │  Rive Plugin (in-process, C/C++ runtime)        │  │
│  │                                                  │  │
│  │  1. Load .riv file → Artboard + StateMachine    │  │
│  │  2. Receive commands from channel process:      │  │
│  │     LoadCommand → load .riv, init state machine │  │
│  │     PlayCommand → trigger "in_animation" input  │  │
│  │     UpdateCommand → set state machine inputs     │  │
│  │     StopCommand → trigger "out_animation" input │  │
│  │  3. Each frame:                                  │  │
│  │     state_machine.advance(dt)                    │  │
│  │     renderer.render(artboard) → GPU texture      │  │
│  │     GPU readback → RGBA buffer (direct pointer)  │  │
│  └──────────────────────┬───────────────────────────┘  │
│                          │ RGBA buffer (zero copy)      │
│                          ▼                              │
│  Compositor → Audio Mixer → Frame Pipeline → Output    │
└──────────────────────────────────────────────────────┘
```

## WASM Alternative (CEF)

For simpler Rive animations, the WASM runtime can run inside a CEF template — no separate plugin process needed. This is a template-level decision, not an engine-level one.

| | Native C++ Plugin | WASM in CEF Template |
|---|---|---|
| Performance | Native GPU rendering, minimal overhead | WASM + Canvas/WebGL + CEF compositing |
| Best for | Complex animations, full-screen motion graphics | Simple animations mixed with HTML layout |
| SDK access | Plugin SDK only (play/stop/update) | Full Pivox JS SDK (model, feeds, native) |
| Process | Separate plugin process | Inside CEF plugin process |
| Integration effort | Plugin SDK adapter | Zero — just a JS library in a template |

Both paths can coexist — some layers use the native Rive plugin, others use Rive WASM inside CEF templates.

## Command Mapping

Pivox commands map to Rive state machine operations:

| Pivox Command | Rive Action |
|---|---|
| `LoadCommand(project, data)` | Load `.riv` file, instantiate artboard + state machine, set initial inputs from `data` |
| `PlayCommand` | Fire the trigger input named in manifest's `triggers.play` (e.g., "in_animation") |
| `StopCommand` | Fire the trigger input named in manifest's `triggers.stop` (e.g., "out_animation") |
| `UpdateCommand(data)` | Set state machine inputs from `data` JSON — numbers, strings, booleans map to Rive inputs |
| `NextCommand` | Fire the trigger input named in manifest's `triggers.next` (if defined) |

## Data Binding — State Machine Inputs

Rive state machines have typed inputs: numbers, booleans, and triggers. Pivox's UpdateCommand maps JSON fields to these inputs:

```
UpdateCommand data:
  { "score_home": 3, "score_away": 2, "is_overtime": true }

Maps to Rive inputs:
  state_machine.set_number("score_home", 3)
  state_machine.set_number("score_away", 2)
  state_machine.set_boolean("is_overtime", true)
```

The Rive designer creates inputs in the state machine editor. The template manifest maps Pivox field names to Rive input names. The operator sees the same field controls (auto/gated/manual) as any other template — the Data Plane handles routing identically.

## Template Manifest

```json
{
  "name": "branded-lower-third",
  "version": "1.0.0",
  "engine": "rive",
  "entry": "lower-third.riv",
  "artboard": "MainArtboard",
  "state_machine": "MainStateMachine",

  "triggers": {
    "play": "in_animation",
    "stop": "out_animation",
    "next": "next_page"
  },

  "fields": {
    "name": {
      "type": "string",
      "rive_input": "name_text",
      "label": "Name",
      "default_update_mode": "manual"
    },
    "title": {
      "type": "string",
      "rive_input": "title_text",
      "label": "Title",
      "default_update_mode": "manual"
    },
    "team_color": {
      "type": "color",
      "rive_input": "accent_color",
      "label": "Team Color",
      "default_update_mode": "manual"
    }
  },

  "default_transition": {
    "type": "CUT",
    "duration_ms": 0
  }
}
```

The `engine: "rive"` field tells the control plane to route this template to the Rive plugin instead of CEF.

## Designer Workflow

```
1. Designer opens rive.app (browser-based editor)
2. Creates animation:
   - Artboard at broadcast resolution (1920x1080)
   - State machine with named states:
     - "idle" (initial, invisible)
     - "in_animation" (animate in)
     - "visible" (on-air, holding)
     - "out_animation" (animate out)
   - Inputs for data binding (text, numbers, booleans)
   - Triggers for play/stop/next
3. Tests in rive.app preview
4. Exports .riv file
5. Creates manifest.json (maps Pivox fields to Rive inputs)
6. Uploads to Pivox template registry
7. Operator loads and plays like any other template
```

## Process Model

- Rive runs **in-process** inside the channel process as a loaded module
- Loaded on demand — only when a channel has Rive templates on a layer
- Unloaded when the layer is cleared (frees runtime resources)
- Frame delivery via direct buffer pointer — zero copy, zero overhead
- If Rive crashes: the entire channel process goes down (all plugins on that channel). Supervisor restarts the channel process, reloads all plugins, re-applies state. In practice, Rive crashes are very rare — deterministic state machine execution with no user code.

## Risks and Considerations

**Linux support:** The rive-runtime repo notes macOS as the primary dev platform with "community work" for Windows/Linux. The Vulkan/OpenGL renderer path on Linux needs early validation during Phase 1.

**Rive company viability:** Rive is a smaller company. If Rive discontinues the product, the C/C++ runtime is MIT-licensed — Pivox can fork and maintain it. The .riv file format is the risk — designer tooling would be lost if rive.app shuts down. Monitor adoption and financial health.

**Rive text rendering:** Rive's text capabilities are more limited than HTML/CSS. For text-heavy templates (tickers, detailed scoreboards), CEF is the better choice. Rive is best for motion graphics where animation quality matters more than text flexibility.
