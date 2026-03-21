# Pivox Tooling & Developer Ecosystem

## Overview

This document covers the tools, SDKs, and development workflows available to Pivox users — template developers, designers, and integrators.

**Related docs:**
- `docs/sdk.md` — full Pivox JavaScript SDK API reference
- `docs/templates.md` — template authoring guide, manifest spec, lifecycle
- `docs/data-plane.md` — live data feeds, schemas, shared memory
- `docs/engine.md` — engine internals, plugin protocol

## Design & Authoring Tools

### Rive (Visual Animation Design)

Rive is a **day-one** design tool for creating complex broadcast animations without writing JavaScript. Designers work in Rive's visual editor, export `.riv` files, and load them as templates in Pivox.

**What Rive provides:**
- Browser-based visual animation editor (rive.app)
- Timeline-based keyframe animation
- Interactive state machines (trigger animations from data changes)
- Bone/skeletal animation
- Vector graphics with mesh deformation
- Export to compact `.riv` binary format

**Why Rive day one:**
- Fills the gap between "developer writes JS" and "we build a WYSIWYG editor"
- Designers get a professional animation tool immediately — no Pivox WYSIWYG editor needed
- `.riv` files are compact (KBs, not MBs) — fast to load, cache, and transfer
- State machines map naturally to Pivox commands (play → trigger in-animation state, stop → trigger out-animation state, update → set input values)

**Two integration paths:**

| | Rive in CEF (WASM) — Simple Animations | Rive Native Plugin (C/C++) — Day One |
|---|---|---|
| Integration effort | Near zero — Rive JS/WASM library in a CEF template | Medium — Plugin SDK adapter, C/C++ FFI |
| Performance | WASM in Chromium — good, improving constantly | Native C/C++, direct buffer render — fastest possible |
| Template authoring | Same as any CEF template (HTML + Rive WASM) | Separate manifest, different from HTML templates |
| SDK access | Full — `pivox.model`, `pivox.feeds`, `pivox.native` | Limited to Plugin SDK commands |
| Dev workflow | Browser mock works — preview in Chrome | Needs engine running |

**Day-one approach: Native C/C++ Rive plugin** for complex animations (the reason Rive exists in the stack). Rive WASM in CEF is available as a simpler alternative for animations mixed with HTML layout. See `docs/plugins/plugin-rive.md` for the native plugin architecture.

**CEF/WASM integration — a Rive template is just an HTML page:**

```html
<canvas id="rive-canvas"></canvas>
<script src="@rive-app/canvas"></script>
<script>
  class RiveLowerThird {
    onLoad(model) {
      this.rive = new RiveCanvas({
        src: pivox.assets.resolve('lower-third.riv'),
        canvas: document.getElementById('rive-canvas'),
        stateMachines: 'MainStateMachine',
        autoplay: false
      });
      // Bind view model fields to Rive state machine inputs
      pivox.model.watch('name', (val) => this.rive.setTextInput('name_text', val));
      pivox.model.watch('title', (val) => this.rive.setTextInput('title_text', val));
    }
    onPlay() {
      this.rive.play('in_animation');
      // Rive state machine fires completion event
      this.rive.onStateChange((event) => {
        if (event === 'in_complete') pivox.ready();
      });
    }
    onStop() {
      this.rive.play('out_animation');
      this.rive.onStateChange((event) => {
        if (event === 'out_complete') pivox.done();
      });
    }
  }
</script>
```

**Native C/C++ plugin path (fallback if WASM perf insufficient):**

```
Rive Plugin (C/C++ core — separate process, via Plugin SDK)
  │
  │  Receives commands from supervisor:
  │    LoadCommand → rive_runtime.load("lower-third.riv")
  │    PlayCommand → rive_runtime.trigger("in_animation")
  │    UpdateCommand → rive_runtime.set_input("name", "John Smith")
  │    StopCommand → rive_runtime.trigger("out_animation")
  │
  │  Renders every frame:
  │    rive_runtime.advance(dt)
  │    rive_runtime.render_to_buffer(rgba_buffer)
  │
  │  Output: RGBA buffer → compositor
  │
  └── Plugin capabilities:
        outputs_video: true, outputs_alpha: true
        supports_load/play/stop/update: true
        supported_formats: [".riv"]
```

**Designer workflow:**

```
1. Designer opens rive.app (browser-based editor)
2. Creates animation with:
   - In-animation state (triggered by PlayCommand)
   - Out-animation state (triggered by StopCommand)
   - Text/data inputs (bound to Pivox view model fields)
   - Interactive state machine with named triggers
3. Exports .riv file
4. Uploads to Pivox template registry (with manifest)
5. Operator loads and plays like any other template
```

**Rive manifest example:**

```json
{
  "name": "lower-third-animated",
  "version": "1.0.0",
  "engine": "rive",
  "entry": "lower-third.riv",
  "artboard": "MainArtboard",

  "state_machine": "MainStateMachine",
  "triggers": {
    "play": "in_animation",
    "stop": "out_animation"
  },

  "fields": {
    "name": {
      "type": "string",
      "rive_input": "name_text",
      "label": "Name"
    },
    "title": {
      "type": "string",
      "rive_input": "title_text",
      "label": "Title"
    }
  },

  "default_transition": {
    "type": "CUT",
    "duration_ms": 0
  }
}
```

**Rive vs CEF templates:**

| | Rive Templates | CEF Templates (HTML/JS) |
|---|---|---|
| Authoring | Visual editor (rive.app) | Code editor (VS Code, etc.) |
| Who creates | Motion designers, animators | Web developers |
| Animation capability | Advanced — bones, mesh deform, state machines | CSS/JS animations, WebGPU |
| Data binding | Rive state machine inputs | Pivox SDK view model + feeds |
| File size | Compact (.riv, KBs) | HTML/CSS/JS bundle (KBs-MBs) |
| Runtime | C/C++ core (separate plugin process) | CEF/Chromium (separate browser process) |
| 3D capability | No (2D only) | Yes (WebGPU, Three.js, Babylon.js) |
| Data visualization | Limited | Full (D3, Chart.js, etc.) |
| Live data feeds | Via Pivox view model only | View model + shared memory feeds |
| Ecosystem | Growing, designer-focused | Massive (web platform) |

**When to use which:**
- **Rive** — designer-created animations: branded lower thirds, stingers, transitions, motion graphics, anything where visual animation quality matters and a designer (not developer) creates the content
- **CEF** — developer-created templates: data visualizations, 3D graphics, complex layouts, anything that needs full web platform capabilities, live data feed subscriptions, or custom JavaScript logic

Both can be used simultaneously on different layers of the same channel.

### HTML/CSS/JS Templates (CEF)

The primary template platform. Any web developer can create broadcast graphics.

**Tools:**
- Any code editor (VS Code, Cursor, WebStorm)
- Browser DevTools for debugging
- `pivox-sdk-mock.js` for browser-based development (see `docs/sdk.md`)
- Standard web build tools (Vite, webpack, esbuild)

**Libraries commonly used in broadcast templates:**
- **GSAP** — professional animation library, smooth tweens and timelines
- **Three.js / Babylon.js** — 3D graphics via WebGPU/WebGL
- **D3.js** — data-driven visualizations (charts, maps, diagrams)
- **Chart.js** — simple charting
- **Lottie** — After Effects animation playback (alternative to Rive for AE workflows)
- **anime.js** — lightweight animation
- **PixiJS** — 2D WebGL rendering

### WYSIWYG Template Editor (Future)

A browser-based visual editor for creating simple templates without code. Targeted at producers and operators who need to customize templates quickly.

**Scope (TBD):**
- Drag-and-drop element placement
- Text styling (font, size, color, alignment)
- Image/logo placement
- Simple animation presets (fade, slide, scale)
- Data field binding (connect text elements to view model fields)
- Live preview

**Not in scope (use Rive or code for these):**
- Complex keyframe animation
- 3D graphics
- Custom JavaScript logic
- State machine design

To be designed separately.

## Developer SDKs & Libraries

### Pivox JavaScript SDK

Injected into CEF templates automatically. See `docs/sdk.md` for full API reference.

**Namespaces:** `pivox.model`, `pivox.feeds`, `pivox.native`, `pivox.system`, `pivox.assets`, `pivox.timing`, `pivox.channel`, `pivox.log`

### Pivox SDK Mock (npm)

Browser-based mock for template development without the engine.

```bash
npm install @pivox/sdk-mock
```

```html
<script src="node_modules/@pivox/sdk-mock/dist/pivox-sdk-mock.js"></script>
```

Simulates all SDK namespaces in a standard browser. See `docs/sdk.md` — Browser Mock section.

### Pivox Plugin SDK

For third-party engine integrations (Unreal, Godot, custom renderers). Published as a library in C, Rust, and C++.

See `docs/plugins/plugin-sdk.md` for the full Plugin Protocol and SDK. Built-in plugins (CEF, FFmpeg, Rive) use the SDK from Phase 1a. Third-party engine integration is Phase 6.

**The Plugin SDK handles:**
- gRPC connection to the Pivox channel supervisor
- Shared memory frame delivery setup
- Command reception and dispatch
- Status reporting
- Capability declaration

**Plugin authors implement:**
- `on_load(project, data)` — load content
- `on_play()` — start rendering
- `on_stop()` — stop rendering
- `on_update(data)` — update data
- `render_frame(buffer, width, height)` — render one frame

### Pivox Go Client SDK

For external system integrations — trigger graphics from custom automation, build alternative operator UIs, integrate with non-standard NRCS.

```go
import "github.com/pivox/pivox-go-sdk"

client := pivox.NewClient("localhost:50051")
client.Play(pivox.PlayCommand{
    Channel: 1,
    Layer: 1,
    TemplateURI: "template://lower-third/v1",
    Data: map[string]any{"name": "John Smith", "title": "CEO"},
})
```

## CLI Tools

### pivox-engine

The engine binary. Runs the Rust/C++ playout engine.

```bash
# Start engine with config
pivox-engine --config /etc/pivox/engine.yaml

# Start single channel for development
pivox-engine --dev --channels 1 --ndi-output
```

### pivox-server

The Go control plane binary.

```bash
# Start control plane (cloud mode)
pivox-server --mode cloud --config /etc/pivox/server.yaml

# Start control plane (local mode, syncs with cloud)
pivox-server --mode local --cloud-endpoint wss://cloud.pivox.io --token <token>

# Start control plane (standalone on-prem, no cloud)
pivox-server --mode standalone --config /etc/pivox/server.yaml
```

### pivox-ctl

CLI tool for controlling the engine and control plane from the terminal.

```bash
# Channel control
pivox-ctl play --channel 1 --layer 1 --template lower-third --data '{"name":"John"}'
pivox-ctl stop --channel 1 --layer 1
pivox-ctl update --channel 1 --layer 1 --data '{"name":"Jane"}'
pivox-ctl status --channel 1

# Video control
pivox-ctl video load --channel 1 --layer 0 --uri /media/clip.mxf --paused
pivox-ctl video play --channel 1 --layer 0 --speed 1.0
pivox-ctl video seek --channel 1 --layer 0 --timecode 00:01:30:00

# Feed control
pivox-ctl feeds list
pivox-ctl feeds status scores
pivox-ctl feeds pause scores
pivox-ctl feeds resume scores

# Template management
pivox-ctl templates list
pivox-ctl templates upload ./my-template/
pivox-ctl templates publish lower-third --version 1.2.0

# System
pivox-ctl health
pivox-ctl channels
```

## Development Environment Setup

### macOS (Primary Dev Platform)

```bash
# Prerequisites
brew install rust go node ffmpeg

# Clone and build
git clone <pivox-repo>
cd pivox

# Build engine (Rust + C++)
make build-engine

# Build control plane (Go)
make build-server

# Download CEF binary distribution
make download-cef

# Start dev environment (engine + control plane + Electron)
make dev
```

### Windows (Secondary Dev Platform)

```powershell
# Prerequisites: Rust, Go, Node.js, FFmpeg (via choco or manual install)
choco install rust go nodejs ffmpeg

# Build and run (same make targets, or use provided scripts)
.\scripts\dev-setup.ps1
make dev
```

### Template Development Only (No Engine Needed)

```bash
# Just need Node.js
npm install @pivox/sdk-mock

# Create template project
npx create-pivox-template my-lower-third

# Start dev server with hot reload
cd my-lower-third
npm run dev
# Opens browser with mock SDK, live reloading
```

## Template Project Scaffolding

`create-pivox-template` generates a starter template project:

```
my-template/
├── manifest.json          # template manifest
├── index.html             # entry point
├── style.css              # styles
├── main.js                # template class + SDK bindings
├── package.json           # npm dependencies
├── dev.html               # development wrapper (includes SDK mock)
├── .pivoxrc               # local dev config (engine endpoint for tier 2 dev)
└── assets/
    └── (template-specific assets)
```

Includes:
- Pre-configured `pivox-sdk-mock` for browser development
- Hot module reload via Vite
- TypeScript definitions for the SDK (`@pivox/sdk-types`)
- Example lifecycle implementation
- Example manifest with field declarations
