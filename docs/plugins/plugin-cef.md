# Pivox CEF Plugin — HTML/JS Graphics Engine

## Overview

CEF (Chromium Embedded Framework) is Pivox's primary graphics rendering engine. It is a built-in plugin — it ships with Pivox and is built on the same Plugin SDK as all plugins (built-in and third-party alike).

CEF runs as an **in-process plugin** inside each channel process. It is loaded on demand when a channel needs HTML/JS graphics layers. If CEF crashes, the entire channel process restarts — the supervisor detects this and recovers automatically. In practice, CEF crashes are rare — V8 sandboxes JavaScript errors and most template bugs don't cause segfaults.

CEF renders HTML/CSS/JS templates in **Off-Screen Rendering (OSR) mode** — no window is created. It delivers RGBA frames via direct buffer pointer to the compositor (no shared memory or IPC — same process), which alpha-blends them over video layers and routes the result to broadcast output.

This is a full Chromium browser. WebGPU, WebGL, WebRTC, Web Audio, and all standard web APIs work inside templates. Template developers author graphics using the same tools and APIs they use for the web.

**Related documents:**
- `docs/engine.md` — playout engine architecture, rendering pipeline, compositing, hardware output
- `docs/sdk.md` — JavaScript SDK reference (injected into every template)
- `docs/data-plane.md` — shared memory architecture, feed connectors, operator controls

## Plugin Capabilities

The CEF plugin registers the following `PluginCapabilities` when it connects to the supervisor:

```protobuf
PluginCapabilities {
  name = "CEF HTML Renderer"
  type = GRAPHICS

  // Supported commands
  supports_load = true           // pre-load template into background slot
  supports_play = true           // animate in (calls onPlay)
  supports_stop = true           // animate out (calls onStop)
  supports_update = true         // live data patching via view model
  supports_next = true           // multi-step graphics (calls onNext)
  supports_seek = false          // not a timeline-based source
  supports_variable_speed = false // not applicable
  supports_loop = false          // not applicable

  // Output capabilities
  outputs_video = true           // delivers RGBA frames (with alpha)
  outputs_audio = true           // delivers PCM audio via CefAudioHandler
  outputs_alpha = true           // alpha channel is meaningful (DSK keying)
  outputs_captions = false       // not a video source — no embedded captions

  // Content capabilities
  supported_formats = [".html"]  // template directories with index.html
}
```

## CEF Configuration

CEF runs in Off-Screen Rendering (OSR) mode with the following flags:

| Flag | Purpose |
|---|---|
| `--off-screen-rendering-enabled` | No window created — headless rendering |
| `--use-gl=egl` | GPU-accelerated rendering without X11/Wayland (Linux production) |
| `--enable-unsafe-webgpu` | WebGPU support via Dawn (auto-selects platform GPU API) |
| `--disable-gpu-vsync` | Frame timing controlled by engine, not display vsync |
| `--auto-grant-permissions` | Auto-grant camera/mic for WebRTC-based templates (video calls) |

Frame rate is not controlled by CEF. The engine ticks CEF's `DoMessageLoopWork()` at exactly the house frame rate (e.g., 59.94 Hz). Every tick produces one frame. Every frame is captured. No dropped frames.

## C++ / Rust Boundary

C++ is used only because CEF's API is C++. The C++ layer is a thin pass-through (~500-1000 lines total). It receives CEF callbacks and immediately forwards them to Rust via FFI. All engine logic lives in Rust.

```
┌─────────────────────────────────────────────────────┐
│  CEF Host Process                                    │
│                                                      │
│  C++ (thin, ~500-1000 lines):                        │
│    - CefApp, CefClient, CefRenderHandler setup       │
│    - OnPaint() → passes buffer pointer to Rust       │
│    - CefV8Handler → forwards calls to Rust via FFI   │
│    - CefAudioHandler → forwards PCM to Rust          │
│    - CEF message loop tick (called by Rust)           │
│                                                      │
│  Rust (all engine logic):                            │
│    - Frame pipeline, compositor                      │
│    - Native SDK function implementations             │
│    - Timing, hardware queries, audio mixing          │
│    - Shared memory frame delivery                    │
│    - Output adapters (AJA, NDI, MJPEG)              │
│                                                      │
│  FFI boundary (narrow, well-defined):                │
│    C++ → Rust: on_paint(buffer_ptr, width, height)   │
│    C++ → Rust: on_audio(samples_ptr, frames, channels)│
│    C++ → Rust: native_call(fn_id, args) → result     │
│    Rust → C++: execute_javascript(code)              │
│    Rust → C++: send_mouse_event(x, y, button)        │
│    Rust → C++: send_key_event(key_code, modifiers)   │
│    Rust → C++: tick_message_loop()                   │
└─────────────────────────────────────────────────────┘
```

**Do not** try to wrap CEF's full C++ API in Rust FFI bindings. CEF's class hierarchy, ref-counting (`CefRefPtr`), and callback patterns make this impractical. Keep the C++ surface minimal and forward everything to Rust immediately.

## Frame Delivery

CEF's `CefRenderHandler::OnPaint()` fires once per frame with a BGRA pixel buffer. The plugin writes this buffer to a shared memory region and signals the compositor.

CEF GPU-renders everything — even basic HTML/CSS is GPU composited internally by Chromium's compositor. The GPU does the heavy lifting. Better GPU = smoother CEF rendering = more channels per machine.

The flow per frame:

1. Engine ticks `DoMessageLoopWork()` on the genlock edge
2. CEF renders all active graphics layers
3. `OnPaint()` fires with the BGRA pixel buffer
4. C++ `OnPaint` handler passes the buffer pointer to Rust via FFI
5. Rust writes RGBA to shared memory, signals the compositor
6. Compositor alpha-blends graphics over video layers
7. Frame pipeline converts colorspace, splits fill+key, routes to outputs

## Audio Capture

CEF's `CefAudioHandler::OnAudioStreamPacket()` delivers raw PCM samples per audio stream within a graphics layer. Audio sources include:

- WebRTC calls (voice, video conferencing)
- HTML5 `<audio>` and `<video>` elements
- Web Audio API sound effects and synthesis

Audio is written to the shared memory audio region at 48kHz (CEF outputs 48kHz natively). The engine's audio mixer combines CEF audio with video layer audio, applies per-layer volume controls, and embeds the mixed output in the SDI/NDI streams.

Audio is synchronized to video frames — the engine correlates audio packets with the frame in which they were produced.

## JavaScript SDK Injection

The Pivox SDK is injected into every template page by CEF before the template loads. Template authors do not import or include it — it is always available.

Injection is implemented via CEF's V8 extension mechanism or `ExecuteJavaScript()` at page load time. The SDK defines the contract between the playout engine and template code.

**SDK namespaces:**

| Namespace | Purpose |
|---|---|
| `pivox.model` | Reactive view model — declarative data bindings, watchers |
| `pivox.feeds` | Shared memory data feed subscriptions with read throttle |
| `pivox.native` | Hardware queries, frame timing, audio metering (Rust via V8 FFI) |
| `pivox.system` | System data sources (time, timecode, channel info) — always available |
| `pivox.assets` | Asset resolution and preloading |
| `pivox.timing` | Genlock-synced frame callbacks |
| `pivox.channel` | Channel info and safe areas |
| `pivox.log` | Structured logging back to engine |

See `docs/sdk.md` for the full API reference, lifecycle hooks, binding patterns, and template examples.

## Native V8 Bindings (pivox.native)

Some operations are impossible or unacceptably slow in JavaScript. The SDK exposes native functions implemented in Rust, callable directly from JS through CEF's V8 binding mechanism.

**Call path:** JS to V8 to C++ handler (one-line pass-through) to Rust via FFI. Sub-microsecond overhead — no serialization, no IPC.

A `CefV8Handler` is registered for the `pivox.native` namespace. Each C++ handler function is a one-line pass-through that calls the corresponding Rust function via FFI and returns the result.

**Available bindings:**

| Category | Functions |
|---|---|
| Frame timing | `getFrameNumber()`, `getTimecode()`, `getGenlockPhase()`, `getFrameTimestamp()` |
| Hardware state | `getOutputStatus()`, `getChannelConfig()` |
| Audio analysis | `getAudioLevels()`, `getAudioLevels({ layer: N })`, `getAudioLevels({ layers: [...] })` |
| GPU operations | `gpuBlur(imageData, radius)`, `gpuColorTransform(imageData, matrix)` — async, returns Promise |

## Shared Memory Feed Reader

The SDK's `pivox.feeds` namespace reads from the Data Plane's shared memory feeds. This path bypasses gRPC entirely — sub-microsecond latency.

Under the hood:
1. A Rust native binding memory-maps the feed region into the CEF process
2. The SDK (JavaScript) checks for new data per frame via the native binding
3. When data has changed and the read throttle allows, the SDK fires subscription callbacks

Templates subscribe with `pivox.feeds.subscribe()` and specify a `maxUpdatesPerSec` read throttle. See `docs/data-plane.md` for the shared memory architecture and two-layer throttling model.

## Remote Input Interaction

CEF's OSR mode exposes APIs to inject input events programmatically: `SendMouseClickEvent`, `SendKeyEvent`, `SendTouchEvent`. This enables remote interaction for preview, template editing, and debugging — without a local display on the engine machine.

**Event flow:**

```
Browser/Electron (operator UI)
  │
  │  User clicks at (450, 320) in preview window
  │
  ▼
Go API server (WebSocket)
  │
  │  Translate coordinates from preview resolution
  │  to output resolution (e.g., 720p preview → 1080p output)
  │
  ▼
Rust supervisor (gRPC SendInput stream)
  │
  │  Route to correct channel process
  │
  ▼
CEF host process (C++)
  │
  │  browser->GetHost()->SendMouseClickEvent(...)
  │  browser->GetHost()->SendKeyEvent(...)
  │
  ▼
CEF renders updated frame → MJPEG stream updates in operator's browser
```

**Coordinate mapping:** The operator's preview is typically scaled down (e.g., 720p preview of 1080p output). The frontend translates coordinates to output resolution before sending. The engine always receives coordinates in native output resolution.

**Input gating by channel mode:**

| Channel Mode | Input Allowed | Use Case |
|---|---|---|
| **On-Air** | No — events dropped with warning | Live broadcast — graphics on air |
| **Preview** | Yes | Operator previewing before taking on-air |
| **Edit** | Yes | WYSIWYG template design — interactive editing |
| **Debug** | Yes | Developer testing — DevTools access enabled |

The Rust supervisor enforces mode rules. If a channel is on-air and input events arrive, they are dropped and an `InputAck` with `accepted=false` is returned.

**Latency:** Input round-trip (click → render → MJPEG update in browser) is ~150-250ms. Acceptable for preview and editing.

## Template Loading

When the supervisor sends a `PluginLoadCommand` to the CEF plugin:

1. `LoadCommand` includes `template_uri` → resolved to a local file path
2. CEF loads `index.html` from the template directory
3. The SDK is injected before the page loads
4. CEF calls `onLoad(model)` with the initial data snapshot
5. The template sets up bindings and watchers

**Background/foreground slots:**
- **Background slot:** Template is loaded but invisible (CSS hidden). Data bindings are live, but the template is not on-air. Used for pre-loading the next graphic.
- **Foreground slot:** Template is visible and on-air. Transitioned via `onPlay()`.

## WebGPU Support

WebGPU is enabled via the `--enable-unsafe-webgpu` flag. CEF uses the Dawn backend, which auto-selects the platform GPU API (Vulkan on Linux, Metal on macOS, D3D12 on Windows).

Templates use WebGPU for:
- 3D graphics and scenes
- Particle effects and simulations
- Shader-based animations (WGSL compute and fragment shaders)
- Real-time data visualizations with GPU acceleration

Three.js, Babylon.js, custom WGSL shaders, and any WebGPU-based library work inside templates. This is the primary path for 3D broadcast graphics in Pivox.

## Rive WASM Support

Rive's JavaScript/WASM runtime can run inside CEF templates. This is an option for simpler Rive animations where the overhead of a separate native plugin process is not justified.

For complex Rive animations or high channel counts, the native Rive plugin (which uses Rive's C++ runtime directly) is preferred. See `docs/plugins/plugin-rive.md` for the native Rive plugin.

## Remote JavaScript Debugging

CEF supports Chrome DevTools Protocol for remote debugging. Enabled per channel in Debug mode:

```
--remote-debugging-port=9222
```

When enabled, any Chrome browser on the network can connect to `http://engine-machine:9222` and get full DevTools:

- DOM inspector — inspect template HTML structure
- Console — view `pivox.log` output, JS errors, SDK state
- Network — monitor asset loading, feed connections (for WASM Rive or direct fetch templates)
- Performance profiler — profile animation performance, find frame budget issues
- JavaScript debugger — set breakpoints in template code, inspect view model state
- Memory — detect leaks in long-running templates

**Security:** Remote debugging is only enabled in Debug channel mode. The supervisor does not enable it for On-Air, Preview, or Edit modes. The port is configurable per channel to avoid conflicts when debugging multiple channels simultaneously.

## Crash Behavior

CEF runs in-process — a CEF crash takes down the entire channel process (all plugins on that channel).

- **If CEF crashes:** All layers on the channel go black (video, graphics, everything). Other channels are unaffected (separate processes).
- **Detection:** The supervisor monitors channel processes. A crash is detected immediately.
- **Recovery:** The supervisor restarts the channel process and all its plugins automatically.
- **State restoration:** On restart, templates are reloaded and view model state is re-applied from the last known snapshot. The recovery is visible (~2-5 seconds) but does not require operator intervention.

In practice, CEF crashes are rare — V8 sandboxes JavaScript errors, and most template bugs result in visual glitches rather than segfaults.
