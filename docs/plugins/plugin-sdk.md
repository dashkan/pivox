# Pivox Plugin SDK

## Overview

The Plugin SDK defines how rendering engines integrate with Pivox's compositor. Every source in Pivox — whether built-in or third-party — implements the same `PivoxPlugin` trait to deliver frames, receive commands, and report capabilities.

**Critical design principle: Pivox's own built-in engines (CEF, FFmpeg, and Rive) are built on this same Plugin SDK.** The compositor treats every source identically regardless of origin.

**References:** `docs/protocols.md` (protobuf definitions), individual plugin docs in `docs/plugins/`.

## Two Plugin Modes

Plugins run in one of two modes depending on whether they can be loaded in-process:

### In-Process Plugins (Built-In, Day One)

Built-in plugins (CEF, FFmpeg, Rive) run **inside the channel process** as loaded modules. Each channel process loads only the plugins it needs.

```
┌──────────────────────────────────────────────────────────┐
│  Channel Process (one per channel)                        │
│                                                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐               │
│  │ CEF      │  │ FFmpeg   │  │ Rive     │  ← loaded     │
│  │ plugin   │  │ plugin   │  │ plugin   │    on demand   │
│  │          │  │          │  │          │    as shared   │
│  │          │  │          │  │          │    libraries   │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘               │
│       │ RGBA        │ RGBA        │ RGBA                 │
│       │ (direct     │ (direct     │ (direct              │
│       │  pointer)   │  pointer)   │  pointer)            │
│       ▼             ▼             ▼                      │
│  ┌──────────────────────────────────────────────────┐    │
│  │  Compositor (treats all sources identically)      │    │
│  └──────────────────────┬───────────────────────────┘    │
│                          ▼                                │
│  Audio Mixer → Frame Pipeline → Output                    │
└──────────────────────────────────────────────────────────┘
```

**Advantages:**
- Frame delivery via direct buffer pointer — zero copy, zero overhead
- No shared memory setup, no gRPC for frame signaling
- Fewer OS processes — 5 total for 4 channels (1 supervisor + 4 channel processes)
- Plugins load/unload on demand — a channel with no video layers doesn't load FFmpeg

**Crash behavior:** If a plugin crashes (e.g., CEF segfault from a badly behaved template), the entire channel process goes down. The supervisor restarts the channel process (~2-5 seconds to recover). In practice, plugin crashes are rare — CEF sandboxes JS errors, FFmpeg is deterministic decode, Rive is a deterministic state machine.

### Out-of-Process Plugins (Third-Party, Phase 6)

Third-party plugins that can't run in-process (Unreal, Godot, custom engines) connect as **separate OS processes** via gRPC + shared memory.

```
┌──────────────────────────────────────────────────────────┐
│  Channel Process                                          │
│                                                           │
│  ┌──────────────────────────────────────────────────┐    │
│  │  Plugin Receiver (accepts external connections)   │    │
│  └──────────┬───────────────────────────────────────┘    │
│             │▲ gRPC + shared memory                      │
│  In-process ││                                           │
│  plugins    ││  ┌──────────────────┐                     │
│  (CEF,      ││  │ Unreal plugin    │ ← separate process  │
│   FFmpeg,   ││  │ (out-of-process) │                     │
│   Rive)     ││  └──────────────────┘                     │
│       │     │▼                                           │
│       ▼     ▼                                            │
│  ┌──────────────────────────────────────────────────┐    │
│  │  Compositor (treats all sources identically)      │    │
│  └──────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────┘
```

**Advantages:**
- Crash isolation — Unreal crashing doesn't affect CEF/FFmpeg/Rive
- Can run on different machines (Unreal on Windows, engine on Linux)
- No in-process constraints (Unreal's threading model, GPU context, etc.)

### Process Count

| Configuration | Processes |
|---|---|
| 4 channels, built-in plugins only | 5 (1 supervisor + 4 channel processes) |
| 4 channels + 1 Unreal on 1 channel | 6 (+1 Unreal process) |
| Redundancy (A + B), built-in only | 10 |

Built-in plugins add zero extra processes. Each out-of-process plugin adds one process per channel it serves.

### The SDK Abstraction

The `PivoxPlugin` trait is the same for both modes. The difference is transport:

| | In-Process | Out-of-Process |
|---|---|---|
| Frame delivery | Direct buffer pointer | Shared memory + gRPC signal |
| Audio delivery | Direct buffer pointer | Shared memory |
| Command reception | Direct function call | gRPC stream |
| Status reporting | Direct callback | gRPC stream |
| Plugin SDK handles | Memory management, lifecycle | gRPC, shared memory setup, double buffering |

The compositor receives an RGBA buffer either way — it doesn't know or care how the buffer arrived.

## PivoxPlugin Trait

Every plugin — in-process or out-of-process — implements this trait:

```rust
pub trait PivoxPlugin {
    /// Return plugin capabilities (called once at registration).
    fn capabilities(&self) -> PluginCapabilities;

    /// Load content. `project` is engine-specific (file path, URL, scene name).
    /// `data` is JSON with initial view model state.
    fn on_load(&mut self, project: &str, data: &[u8]);

    /// Start rendering / trigger in-animation.
    fn on_play(&mut self);

    /// Stop rendering / trigger out-animation.
    fn on_stop(&mut self);

    /// Update data while on-air. `data` is JSON with field patches.
    fn on_update(&mut self, data: &[u8]);

    /// Advance multi-step graphic to next page/state.
    fn on_next(&mut self) {}

    /// Immediately remove all content.
    fn on_clear(&mut self) {}

    /// Render one frame. Write RGBA pixels into `buffer`.
    /// Buffer is pre-allocated to `width * height * 4` bytes.
    fn render_frame(&mut self, buffer: &mut [u8], width: u32, height: u32);

    /// Optional: render audio samples for this frame.
    fn render_audio(&mut self, _audio_buffer: &mut [f32], _sample_rate: u32, _channels: u32) {}
}
```

### In-process usage

```rust
// Built-in plugin — loaded as a Rust crate inside channel process
let mut cef = CefPlugin::new(config);
let caps = cef.capabilities();  // register capabilities

// Per frame:
cef.render_frame(&mut buffer, 1920, 1080);  // direct pointer
cef.render_audio(&mut audio_buf, 48000, 2);
compositor.composite(layer_id, &buffer);
```

### Out-of-process usage

```rust
// Third-party plugin — runs in its own process
fn main() {
    let plugin = UnrealPlugin::new();
    pivox_plugin_sdk::run(plugin);
    // SDK handles: gRPC connection, shared memory setup,
    // frame delivery signaling, status reporting
}
```

The SDK's `run()` function manages the gRPC connection and shared memory transport. The plugin author implements the same trait — only the transport layer differs.

## Plugin Capabilities

Plugins declare capabilities at registration. The control plane uses these to show the right UI controls, route content, and validate rundown items.

```protobuf
message PluginCapabilities {
  string name = 1;
  string version = 2;
  PluginType type = 3;            // GRAPHICS, VIDEO, AUDIO, HYBRID

  // Supported commands
  bool supports_load = 10;
  bool supports_play = 11;
  bool supports_stop = 12;
  bool supports_update = 13;
  bool supports_next = 14;
  bool supports_seek = 15;
  bool supports_variable_speed = 16;
  bool supports_loop = 17;

  // Output capabilities
  bool outputs_video = 20;
  bool outputs_audio = 21;
  bool outputs_alpha = 22;
  bool outputs_captions = 23;

  // Content capabilities
  repeated string supported_formats = 30;
  repeated string supported_codecs = 31;
  map<string, string> custom_metadata = 40;
}
```

### Capability matrix — built-in plugins

| Capability | CEF | FFmpeg | Rive |
|---|---|---|---|
| `supports_load` | Yes | Yes | Yes |
| `supports_play` | Yes | Yes | Yes |
| `supports_stop` | Yes | Yes | Yes |
| `supports_update` | Yes | No | Yes |
| `supports_next` | Yes | No | Yes |
| `supports_seek` | No | Yes | No |
| `supports_variable_speed` | No | Yes | No |
| `supports_loop` | No | Yes | Yes |
| `outputs_video` | Yes | Yes | Yes |
| `outputs_audio` | Yes | Yes | No |
| `outputs_alpha` | Yes | No | Yes |
| `outputs_captions` | No | Yes | No |

## Plugin Loading and Unloading

Channel processes load plugins on demand:

1. Channel starts with no plugins loaded
2. Control plane sends `LoadCommand` for a layer with a `.html` template → channel loads CEF plugin
3. Control plane sends `VideoLoadCommand` for a layer with a `.mxf` clip → channel loads FFmpeg plugin
4. Control plane sends `LoadCommand` for a layer with a `.riv` file → channel loads Rive plugin
5. Control plane sends `ClearCommand` for a layer → channel unloads the plugin for that layer (frees resources)

Multiple instances of the same plugin type can be loaded (e.g., CEF for layers 1, 2, and 3). Each instance is independent.

## Out-of-Process Plugin Protocol

For plugins that run as separate processes (third-party engines), the full gRPC protocol applies.

See `docs/protocols.md` for canonical protobuf definitions.

**Services:**

```protobuf
service PivoxPluginHost {
  rpc Configure (PluginConfig) returns (PluginConfigAck);
  rpc Execute (stream PluginCommand) returns (stream PluginCommandAck);
}

service PivoxPluginClient {
  rpc DeliverFrames (stream PluginFrame) returns (stream FrameAck);
  rpc ReportStatus (stream PluginStatus) returns (Empty);
}
```

**Frame delivery:** Shared memory with double buffering. gRPC `PluginFrame` message signals "frame N is ready" — actual pixel data is in the shared memory region. See `docs/protocols.md` for message definitions.

**Audio delivery:** Separate shared memory region, PCM at 48kHz, synced to video frames.

## Built-In Plugins

| Plugin | Engine | Type | Mode | Documentation |
|---|---|---|---|---|
| **CEF** | Chromium Embedded Framework | GRAPHICS | In-process | [plugin-cef.md](plugin-cef.md) |
| **FFmpeg** | FFmpeg libav* | VIDEO | In-process | [plugin-ffmpeg.md](plugin-ffmpeg.md) |
| **Rive** | Rive C/C++ runtime | GRAPHICS | In-process | [plugin-rive.md](plugin-rive.md) |

## Third-Party Plugin Development

**What Pivox publishes (not open source — published spec + SDK):**

1. **Pivox Plugin Protocol spec** — protobuf definitions with documented behavior and timing requirements
2. **Pivox Plugin SDK** — library (C, Rust, C++ bindings, ~500-1000 lines) handling gRPC, shared memory, frame delivery, genlock sync
3. **Reference plugin** — minimal OpenGL renderer (spinning cube) demonstrating the full integration

**Plugins can be written in any language** that supports gRPC + shared memory:

| Language | gRPC | Shared Memory | Realistic |
|---|---|---|---|
| Rust | Yes (tonic) | Yes | Yes — first-class SDK |
| C/C++ | Yes (grpc++) | Yes | Yes — most game engines |
| C# | Yes (Grpc.Net) | Yes | Yes — Unity |
| Go | Yes (grpc-go) | Yes | Yes |
| Python | Yes (grpcio) | Yes | Prototyping only — too slow for 60fps |
