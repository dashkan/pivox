# Pivox Playout Engine — Architecture & Design

## Overview

Pivox's playout engine is a broadcast-quality media playout system. It uses a plugin architecture where rendering engines connect via the Pivox Plugin Protocol. Three built-in plugins ship day one — CEF (HTML/JS graphics), FFmpeg (video/audio/stills), and Rive (2D motion graphics). Third-party engines (Unreal, Godot, etc.) can connect as additional sources using the same protocol. All plugins are composited together and output via AJA SDI/ST 2110 and NDI.

**Day-one media support:**
- **Graphics** — HTML/JS/CSS templates with WebGPU, reactive data binding, animations (via CEF plugin)
- **Video** — clip playback, replay, variable speed, jog/shuttle, frame-accurate seeking (via FFmpeg plugin)
- **Audio** — audio-only playback with visual waveform/VU meter templates (via FFmpeg + CEF plugins)
- **Stills** — PNG, JPEG, TGA, TIFF as full-frame layers (via FFmpeg plugin)
- **2D animation** — designer-created motion graphics via visual editor (via Rive plugin)

The goal is a single system that replaces the combination of separate graphics (Vizrt/CasparCG), clip/replay (EVS), and playout tools that broadcast facilities currently operate as independent systems.

This document covers the **playout engine core** — the compositor, frame pipeline, output routing, and process model. Individual plugin internals are documented separately:

- `docs/plugins/plugin-sdk.md` — Plugin Protocol, SDK, capability system
- `docs/plugins/plugin-cef.md` — CEF/HTML/JS graphics engine
- `docs/plugins/plugin-ffmpeg.md` — FFmpeg video/audio/stills engine
- `docs/plugins/plugin-rive.md` — Rive 2D animation engine
- `docs/plugins/plugin-unreal.md` — Unreal Engine integration (future)

The upper layer (NRCS, rundowns, template management, asset management, API gateway) is a separate Go application documented in `docs/control-plane.md`.

## Technology Stack

### Lower Layer (Engine) — Rust + C/C++

Performance-critical rendering and hardware output:

| Component | Language | Rationale |
|---|---|---|
| CEF plugin | C++ (thin) + Rust | Built-in plugin using the Pivox Plugin SDK. C++ is a thin pass-through for CEF callbacks → Rust via FFI |
| Native SDK functions | Rust (called from C++ V8 handlers) | Frame timing, hardware queries, GPU ops. C++ V8Handler is one-line pass-through per function |
| FFmpeg plugin | Rust + C (FFmpeg) | Built-in plugin using the Pivox Plugin SDK. Clip playback, replay, variable speed via libav* |
| Compositor | Rust (CPU SIMD, GPU if needed) | Merge video layers + CEF graphics layers. Alpha blending is CPU-feasible for typical layer counts. GPU path available if profiling demands it |
| Frame pipeline | Rust | Buffer management, colorspace conversion, fill+key split. Memory safety matters at 60fps |
| AJA NTV2 output adapter | C++ wrapper + Rust driver | NTV2 SDK is C++. Thin C++ shim exposes frames to Rust via FFI |
| NDI output | C++ FFI | NDI SDK is C/C++. Minimal wrapper |
| Channel supervisor | Rust | Process manager for channel processes. Health monitoring, restart, IPC |
| MJPEG preview output | Rust | Encode frames for remote browser/Electron preview |
| Recording adapter | Rust (NVENC) | Compliance recording — encode output to H.264/HEVC, write to local disk |
| GPI handler | Rust + C++ (AJA) | Physical button triggers and tally lights via AJA card GPI or IP-based panels |
| Caption/VANC handler | Rust + C++ (AJA) | Embed closed captions (CEA-608/708) in SDI VANC and ST 2110-40 metadata |
| Colorspace conversion | Rust (CPU SIMD, GPU if needed) | sRGB → Rec.709 is near-trivial. CPU SIMD for day one, native GPU only if profiling shows CPU bottleneck |

### C++ / Rust Boundary Principle

C++ is used **only** where a third-party SDK requires it (CEF, AJA NTV2, NDI). The C++ layer is as thin as possible — it receives callbacks from the SDK and immediately forwards to Rust via FFI. All engine logic lives in Rust.

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
│    - Video engine (FFmpeg)                           │
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

**Do not** try to wrap CEF's full C++ API in Rust FFI bindings — CEF's class hierarchy, ref-counting (CefRefPtr), and callback patterns make this impractical. Instead, keep CEF's C++ surface minimal and forward everything to Rust immediately.

### Upper Layer (Control Plane) — Go

Everything above playout: NRCS, rundowns, template registry, asset management, REST/gRPC API gateway, operator UI backend, MOS/VDCP gateways, redundancy coordination, monitoring.

### Boundary — gRPC over Unix Domain Sockets

All communication between Go control plane and Rust engine uses gRPC with protobuf over Unix domain sockets. See [IPC Design](#ipc-design) for details.

## Core Concepts

### Downstream Key (DSK) Model

Pivox outputs **two separate SDI signals per channel**:

- **Fill**: The rendered output (RGB) — video, graphics, or composited video+graphics
- **Key**: A grayscale mask (the alpha channel) — white = fully opaque, black = fully transparent

When a channel contains only graphics layers (no video), the vision mixer uses its downstream keyer (DSK) to composite the fill+key over live camera feeds. When a channel contains video layers with graphics composited on top, the output is a full-frame signal that can be used as a direct source on the mixer.

This dual-use model (DSK for graphics-only, full-frame for video+graphics) is standard in broadcast. The vision mixer operator selects the appropriate input mode.

### Channel / Layer Model

```
Channel = one SDI output pair (fill + key)
  └── Layer Stack (composited bottom-up, higher layer number = on top)
        Layer 0: Video playback (FFmpeg)    ← clip, replay, live ingest
        Layer 1: Lower third (CEF)          ← HTML/JS graphic
        Layer 2: Bug / DOG (CEF)            ← persistent logo
        Layer 3: Ticker / crawl (CEF)       ← HTML/JS graphic
        Layer 4: Alert banner (CEF)         ← HTML/JS graphic
        ...
```

Each channel runs as a **separate OS process** containing one CEF instance and one FFmpeg instance. A channel contains two source types:

- **Video layers** — decoded by FFmpeg. Clip playback, replay, variable speed, jog/shuttle. Typically layer 0 (background) but can be any layer.
- **Graphics layers** — rendered by CEF. HTML/JS templates. Composited on top of video via alpha blending.

The **compositor** (Rust, GPU-accelerated) merges all layers into a single RGBA output per frame. Graphics layers with transparency alpha-blend over video layers. The result is split into fill (RGB) + key (alpha) for SDI output.

### Foreground / Background Slots

Each layer has two slots: **foreground** (on-air / visible) and **background** (cued / warm / invisible). This allows pre-loading the next item while the current item is still on-air, without requiring a second CEF or FFmpeg instance.

```
Channel 1 (single CEF instance, single FFmpeg instance)
│
├── Layer 0 (Video)
│   ├── FOREGROUND: replay_goal_43.mxf     [PLAYING at 0.5x]
│   └── BACKGROUND: highlights_reel.mxf    [LOADED, paused on first frame]
│
├── Layer 1 (Graphics)
│   ├── FOREGROUND: lower-third "John Smith"  [PLAYING / on-air]
│   └── BACKGROUND: lower-third "Jane Doe"   [LOADED / warm, invisible]
│
├── Layer 2 (Graphics)
│   ├── FOREGROUND: ticker                    [PLAYING]
│   └── BACKGROUND: (empty)
│
└── Layer 3 (Graphics)
    ├── FOREGROUND: bug/DOG                   [PLAYING]
    └── BACKGROUND: (empty)
```

**How transitions work between foreground and background:**

1. `LoadCommand` / `VideoLoadCommand` → loads content into the **background** slot (warm, invisible, ready for instant play)
2. `PlayCommand` / `VideoPlayCommand` → if background has content, transitions background to foreground using the specified transition type and duration
3. The outgoing foreground item receives `onStop()` (graphics) or stops playback (video) and is discarded
4. The incoming background item moves to foreground, receives `onPlay()` (graphics) or starts playback (video)
5. Background slot is now empty, ready for the next cue

If background is empty when Play is called, content loads directly into foreground (no transition).

**Why not two CEF/FFmpeg instances per channel:**

| Approach | Memory | GPU | Complexity |
|---|---|---|---|
| Two full instances per channel | ~1-2GB extra per channel | Double GPU contexts, double VRAM | 8 processes for 4 channels |
| Single instance, FG/BG slots | ~50-200MB for hidden content | Same GPU context, minimal extra VRAM | Simple DOM/buffer management |

CEF manages both foreground and background graphics as DOM elements in a single page — background items are fully rendered (JS executed, `onLoad()` called) but invisible (CSS hidden). FFmpeg manages multiple open decode contexts within a single process — background clips have their first frame decoded and buffered, decoder idle until play.

### Compositor and Transition Engine

The compositor merges all layers per frame. During a transition between foreground and background within a layer, the compositor temporarily blends both:

**Normal state (no transition):**
```
Layer output = foreground buffer
```

**During transition (e.g., 30-frame dissolve):**
```
Frame 0:  FG × 1.0 + BG × 0.0    (100% outgoing)
Frame 15: FG × 0.5 + BG × 0.5    (mid-transition)
Frame 30: FG × 0.0 + BG × 1.0    (100% incoming)
→ BG becomes new FG, old FG discarded
```

This is the same alpha-blend math used for compositing layers on top of each other — applied within a layer instead of between layers. Negligible additional GPU cost.

**Built-in transition types:**

| Transition | Description | GPU Cost |
|---|---|---|
| Cut | Instant swap — no blending | None |
| Mix / Dissolve | Alpha crossfade over duration | Trivial |
| Push (L/R/U/D) | Slide incoming in, outgoing out | Trivial — UV offset |
| Wipe (edge) | Hard edge sweep across frame | Trivial — threshold |
| Wipe (box) | Box grows from center or corner | Trivial — rectangle test |
| Wipe (circle) | Circle grows from center | Trivial — distance test |
| DVE | Squeeze, zoom, spin (transform matrix) | Low |
| Custom shader | User-provided GPU shader | Varies |

**Custom transition shaders** are a differentiator — motion designers can create branded transitions as GPU shaders (GLSL or WGSL). These are loaded as assets from the template/asset system and appear in the operator UI's transition library alongside built-ins. No competing system offers this level of customization without proprietary tools.

**Transition selection by operators and automation:**

Transitions are controlled at multiple levels:

1. **Template default** — the template designer can specify a preferred transition in the template manifest (e.g., "this lower-third always uses a push-up"). Used when no override is specified.
2. **Rundown item** — the producer assigns a transition per item when building the rundown. Overrides the template default.
3. **Operator override** — the operator can change the transition at play time via the UI. Overrides the rundown setting.
4. **Automation (MOS/VDCP)** — automation triggers can specify a transition per command. Overrides all defaults.

The operator UI presents a transition library:
- Built-in transitions (cut, mix, push, wipe variants)
- Custom shader transitions loaded from the asset system
- Duration control (frames or milliseconds)
- Direction control (for push/wipe types)
- Preview of the transition effect before committing

### What the Compositor Does vs. the Vision Mixer

Important distinction — Pivox's compositor and the facility's vision mixer serve different roles:

**Vision mixer (external hardware — not part of Pivox):**
- Switches between **full video sources** (cameras, VTRs, graphics channels)
- "Cut from Camera 1 to Camera 2" or "dissolve to Pivox CH1"
- Pivox is one of the **sources** feeding the mixer
- Hardware: Grass Valley Kayenne/Karrera, Sony XVS, Ross Carbonite, Blackmagic ATEM
- Operator: technical director (TD) at the mixer panel

**Pivox compositor (inside the engine):**
- Composites **layers within a single channel** (video + graphics + transitions)
- "Dissolve from lower-third A to lower-third B on layer 1"
- The vision mixer never sees these internal transitions — it just receives Pivox's composited fill+key output
- Operator: graphics operator at the Pivox UI, or automation

The vision mixer handles source-level switching. Pivox handles element-level compositing and transitions within its own output. They complement each other — Pivox does not replace the vision mixer.

### Templates

Templates are the content that the engine renders. Two template types are supported day one:

**CEF templates (HTML/CSS/JS):**
- Web applications that implement the Pivox JavaScript SDK lifecycle hooks
- Full web platform — WebGPU, WebGL, Canvas, any JS library
- Authored by web developers in any code editor
- Can also embed Rive WASM runtime for animations within HTML layouts
- See `docs/sdk.md` for the SDK API, `docs/templates.md` for the authoring guide

**Rive templates (.riv files):**
- 2D motion graphics created in Rive's visual editor (rive.app)
- Authored by motion designers — no code required
- State machines map to Pivox commands (play/stop/update)
- Rendered by the native Rive C/C++ plugin (or WASM in CEF for simpler animations)
- See `docs/plugins/plugin-rive.md` for integration details, `docs/tooling.md` for designer workflow

**Future:**
- **WYSIWYG editor** — browser-based visual editor for simple templates without code
- **3D engine templates** — Unreal, Bevy, Godot via Plugin SDK (Phase 6)

## System Architecture

```
┌─────────────────────────────────────────────────────────┐
│  GO CONTROL PLANE                                        │
│                                                          │
│  NRCS / Rundowns ──► Playout Controller ──► gRPC API    │
│  Template Registry    State Machine          │           │
│  Asset Cache Manager  (preload + LRU cache)  │           │
│  Data Plane (live feeds, routing, gating, throttling)  │           │
│  MOS/VDCP Gateways                           │           │
│  Operator Web UI                             │           │
│  Redundancy Coordinator                      │           │
├──────────────────────────────────────────────┼───────────┤
│  gRPC over Unix Domain Socket                │           │
├──────────────────────────────────────────────┼───────────┤
│  RUST/C++ ENGINE                             ▼           │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │           Channel Supervisor (Rust)               │   │
│  │  - spawns/monitors channel processes              │   │
│  │  - routes commands from gRPC to channels          │   │
│  │  - aggregates health/status back to Go            │   │
│  │  - handles process crashes and restarts           │   │
│  └──┬────────────┬────────────┬────────────┬────────┘   │
│     │            │            │            │             │
│     ▼            ▼            ▼            ▼             │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐  ┌──────────┐   │
│  │  CH 1    │ │  CH 2    │ │  CH 3    │..│  CH N    │   │
│  │          │ │          │ │          │  │          │   │
│  │ CEF*     │ │ CEF*     │ │ CEF*     │  │ CEF*     │   │
│  │ FFmpeg*  │ │ FFmpeg*  │ │ FFmpeg*  │  │ FFmpeg*  │   │
│  │ Rive*    │ │ Rive*    │ │ Rive*    │  │ Rive*    │   │
│  │ Comp.    │ │ Comp.    │ │ Comp.    │  │ Comp.    │   │
│  │ Audio    │ │ Audio    │ │ Audio    │  │ Audio    │   │
│  │ FrmPipe  │ │ FrmPipe  │ │ FrmPipe  │  │ FrmPipe  │   │
│  └──┬───────┘ └──┬───────┘ └──┬───────┘  └──┬───────┘   │
│     │ Fill+Key   │            │             │            │
│     ▼          ▼          ▼             ▼               │
│  ┌──────────────────────────────────────────────────┐   │
│  │              Output Router (Rust)                 │   │
│  │  - maps channels → physical outputs              │   │
│  │  - genlock timing synchronization                 │   │
│  └──┬──────────────────┬───────────────────┬────────┘   │
│     ▼                  ▼                   ▼             │
│  ┌──────────┐   ┌──────────┐        ┌──────────┐       │
│  │ AJA NTV2 │   │ NDI Send │        │ MJPEG    │       │
│  │ Output   │   │ Output   │        │ Preview  │       │
│  │(C++/Rust)│   │ (C++)    │        │ (Rust)   │       │
│  └────┬─────┘   └──────────┘        └──────────┘       │
│       │              │                                   │
│       │         Standard ethernet                        │
│       │         (no special hardware)                    │
│       ▼                                                  │
│  SDI or ST 2110                                          │
│  (per-port config)                                       │
└─────────────────────────────────────────────────────────┘
```

**Notes:**
- `*` = loaded on demand (plugins load/unload as layers need them)
- CEF, FFmpeg, and Rive are **in-process plugins** inside each channel process — direct buffer pointers, no IPC
- Third-party plugins (Unreal, etc.) connect as **out-of-process** via gRPC + shared memory
- 5 total processes for 4 channels (1 supervisor + 4 channel processes)
- See `docs/plugins/plugin-sdk.md` for the dual plugin mode architecture
- AJA outputs SDI or ST 2110 per port (configured at startup, not runtime)
- NDI runs independently over standard ethernet — no AJA card required
- NDI and AJA output run simultaneously in production
- For development on macOS, NDI is the only output (no AJA card needed)
- MJPEG preview is always available for browser/Electron operator UI

## Channel Process Architecture

Each channel is an isolated OS process. The supervisor spawns and monitors these. A crash in one channel does not affect others.

```
┌──────────────────────────────────────────────────────────┐
│              Channel Process (one per channel)            │
│                                                           │
│  ┌───────────────────────┐  ┌──────────────────────────┐ │
│  │   CEF Runtime (C++)    │  │   Video Engine (Rust)     │ │
│  │                        │  │                           │ │
│  │  - Off-Screen Render   │  │  - FFmpeg decode          │ │
│  │    (OSR) mode          │  │    (libavformat/codec)    │ │
│  │  - No visible window   │  │  - Hardware decode        │ │
│  │  - GPU-accelerated     │  │    (NVDEC/VAAPI)         │ │
│  │    via EGL (Linux)     │  │  - Frame-accurate seek    │ │
│  │  - WebGPU enabled      │  │  - Variable speed         │ │
│  │                        │  │  - Jog / shuttle          │ │
│  │  OnPaint() callback ───┤  │  - Loop, in/out points   │ │
│  │  (per graphics layer)  │  │                           │ │
│  └────────────┬───────────┘  └────────────┬──────────────┘ │
│               │ RGBA (per layer)          │ YUV/RGBA       │
│               ▼                           ▼                │
│  ┌────────────────────────────────────────────────────┐   │
│  │              Compositor (Rust)                      │   │
│  │                                                     │   │
│  │  1. Video layer(s) as base                         │   │
│  │  2. CEF graphics layers alpha-blended on top       │   │
│  │  3. Final RGBA output per frame                    │   │
│  └─────────────────────┬──────────────────────────────┘   │
│                         ▼                                  │
│  ┌────────────────────────────────────────────────────┐   │
│  │              Frame Pipeline (Rust)                  │   │
│  │                                                     │   │
│  │  1. Colorspace: sRGB → Rec.709                     │   │
│  │  2. Fill buffer: RGBA → RGB                        │   │
│  │  3. Key buffer: extract alpha channel              │   │
│  │  4. Genlock sync: hold frame until edge            │   │
│  │  5. Route to output adapters                       │   │
│  └────────────────────────────────────────────────────┘   │
│                                                           │
│  IPC to supervisor: Unix domain socket                    │
│  - receives commands (template + video playback)          │
│  - sends status (on-air state, frame count, health)       │
└──────────────────────────────────────────────────────────┘
```

### CEF Configuration

CEF runs in Off-Screen Rendering (OSR) mode:

- `--off-screen-rendering-enabled` — no window created
- `--use-gl=egl` — GPU-accelerated rendering without X11/Wayland (Linux production)
- `--enable-unsafe-webgpu` — WebGPU support via Dawn (platform GPU backend selected automatically)
- `--disable-gpu-vsync` — frame timing controlled by engine, not display vsync
- `--auto-grant-permissions` — auto-grant camera/mic for WebRTC-based templates (video calls)
- Frame rate controlled by engine ticking CEF's message loop at the house frame rate (e.g., 59.94fps)

### Audio Pipeline

Both CEF and the video engine produce audio. The engine captures and routes audio through a pipeline parallel to the video path.

**Capture sources:**
- CEF: `CefAudioHandler::OnAudioStreamPacket()` delivers raw PCM samples per graphics layer (WebRTC calls, HTML5 audio/video, sound effects)
- FFmpeg: decoded audio packets from video clips, synchronized to video frames

The engine mixes all audio sources (graphics layers + video layers) into a single stereo or multichannel output per channel.

**Routing:**

```
CEF audio streams (per graphics layer)
  │
  ├──────────────────────────────┐
  │                              │
  ▼                              ▼
FFmpeg audio (per video layer)   │
  │                              │
  └──────────┬───────────────────┘
             ▼
  Audio mixer (Rust)
    │  - per-channel mix of all layer audio (graphics + video)
    │  - sample rate conversion if needed
    │  - level control per layer
    │
    ├──► AJA SDI audio embedder (NTV2 SDK)
    │    → embedded audio in SDI output, routed to facility audio mixer
    │
    └──► NDI audio (embedded in NDI stream)
         → monitoring and integration
```

AJA's NTV2 SDK supports embedding up to 16 channels of audio in each SDI output. The engine writes PCM samples to the card's audio buffer alongside video frames.

**Audio capabilities:**

| Feature | Description |
|---|---|
| Per-layer volume/mute | Operator controls volume and mute per layer at runtime via gRPC |
| Audio follow video (AFV) | During transitions, audio crossfades in sync with video — cut video = cut audio, dissolve video = crossfade audio |
| Audio channel mapping | Route layer audio to specific SDI output channel pairs (e.g., CH1-2 = program, CH3-4 = clean feed) |
| Audio delay compensation | Configurable delay (typically 1-3 frames) to maintain lip sync — video processing adds latency, audio must be delayed to match |
| Silence generation | When no layers produce audio, output valid silence (zero samples). AJA cards require continuous audio. |
| Sample rate conversion | All sources resampled to 48kHz (broadcast standard). CEF outputs 48kHz natively. FFmpeg clips may be 44.1kHz or other rates |

### Frame Timing and Genlock

CEF does not know about broadcast timing. The engine controls frame cadence:

1. Engine receives genlock reference signal via AJA card
2. On each genlock edge, engine ticks CEF's `DoMessageLoopWork()`
3. CEF renders and fires `OnPaint()` with the pixel buffer
4. Engine captures the buffer and routes to compositor → frame pipeline → outputs
5. AJA card's `AutoCirculate` schedules the frame for the next output field

This ensures every rendered frame aligns with house sync. No dropped frames, no judder.

**Frame rate depends on output format:**
- 1080p59.94: tick at 59.94fps (59.94 full frames per second)
- 1080i59.94: tick at 29.97fps (29.97 progressive frames, AJA card segments each into two fields via PsF)
- 1080p50: tick at 50fps

The engine always renders progressive frames. For interlaced output formats (1080i), the AJA card handles Progressive Segmented Frame (PsF) conversion — splitting each progressive frame into two fields. No interlacing logic is needed in the engine.

For the MJPEG preview path, every 2nd-4th frame is JPEG-encoded at 720p and served over HTTP. Preview latency of 100-200ms is acceptable.

## Video Engine (FFmpeg)

The video engine handles clip playback, replay, and variable-speed playout using FFmpeg's libraries (libavformat, libavcodec, libavutil, libswscale). It runs within each channel process alongside CEF, providing video layers that the compositor merges with graphics layers.

### Why FFmpeg

- Every broadcast codec: ProRes, DNxHR, XDCAM, AVC-Intra, HEVC, H.264, MPEG-2, MXF/MOV/MP4 containers
- Frame-accurate seeking by timecode or frame number
- Hardware-accelerated decode: NVDEC (NVIDIA GPU), VAAPI (Linux), VideoToolbox (macOS)
- Battle-tested in broadcast — used by CasparCG, FFastTrans, and most playout systems
- LGPL/GPL licensed (LGPL if you avoid GPL-only codecs and link dynamically)

### Capabilities

| Feature | Description |
|---|---|
| Frame-accurate playback | Seek to any frame by timecode or frame number |
| Variable speed | 0.1x to 4x+ forward, negative for reverse playback |
| Jog | Step forward/backward one frame at a time |
| Shuttle | Variable speed scrub through clip |
| In/out points | Mark in and mark out for partial clip playback |
| Loop | Continuous loop between in/out points (or full clip) |
| Playlist/sequence | Gapless playback of multiple clips in sequence |
| Hardware decode | NVDEC on NVIDIA GPUs, VAAPI on Linux, VideoToolbox on macOS |
| Audio-only playback | Play audio files (WAV, MP3, FLAC, AAC) with no video — PCM routed to audio mixer. Used for music beds, sound effects, phone interviews |
| Still images | Load PNG/JPEG/TGA/TIFF as single-frame video — held on screen indefinitely |

### Decode Pipeline

```
Clip file (MXF, MOV, MP4, etc.)
  │
  ▼
libavformat — demux container, extract video/audio streams
  │
  ├── Video stream
  │   ▼
  │   libavcodec — decode (NVDEC hardware or CPU fallback)
  │   │
  │   ▼
  │   Decoded frame (YUV 4:2:2 or 4:2:0)
  │   │
  │   ▼
  │   libswscale or GPU shader — convert to RGBA
  │   │
  │   ▼
  │   Compositor (merge with CEF graphics layers)
  │
  └── Audio stream
      ▼
      libavcodec — decode to PCM
      │
      ▼
      Audio mixer (merge with CEF audio, route to AJA/NDI)
```

### Frame Timing for Variable Speed

At normal speed (1x), the video engine delivers one decoded frame per genlock tick. For variable speed:

- **Slow motion (e.g., 0.5x)**: Each decoded frame is held for 2 genlock ticks. The engine tracks fractional frame position.
- **Fast forward (e.g., 2x)**: Skip every other frame, or decode at double rate and drop.
- **Reverse**: Decode GOP in reverse order. For long-GOP codecs (H.264), this requires decoding the full GOP and buffering, then outputting frames in reverse. Intra-frame codecs (ProRes, DNxHR) reverse trivially.
- **Jog**: On each jog command, seek to the next/previous frame, decode, hold on screen.

### Supported Formats (Day One)

Priority broadcast codecs and containers:

| Container | Codecs |
|---|---|
| MXF (OP1a, OPAtom) | XDCAM HD, AVC-Intra, DNxHR, MPEG-2 |
| QuickTime (.mov) | ProRes (422, 4444), DNxHR, H.264 |
| MP4 | H.264, H.265/HEVC |
| MPEG-TS | MPEG-2, H.264 |

All of these are supported by FFmpeg out of the box.

## Output Adapters

### AJA NTV2 (Primary — SDI Output)

Primary output path to broadcast infrastructure.

**Target hardware:**
- Corvid 88: 8x SDI outputs — supports 4 channels with fill+key
- Corvid 44 12G: 4x 12G-SDI — supports 2 channels with fill+key (pair two cards for 4 channels)
- Kona 5: 4x 12G-SDI — development with Thunderbolt on macOS

**Implementation:**
- C++ thin wrapper around NTV2 SDK (`CNTV2Card`, `AutoCirculate` API)
- Rust driver manages frame scheduling and crosspoint routing
- Output routing configured programmatically: "framebuffer 0 → SDI out 1 (fill), SDI out 2 (key)"
- NTV2 SDK is open-source: https://github.com/aja-video/ntv2

**Genlock:** AJA cards accept external reference input (blackburst or tri-level sync). The card locks its output timing to this reference. The engine reads the card's frame clock to synchronize CEF rendering.

**ST 2110 (IP output):** AJA Corvid 44 12G and newer cards support ST 2110 natively alongside SDI. The same card can output both — it's a configuration difference, not a separate output path. See [ST 2110 Output](#st-2110-smpte-ip-output) for details.

### ST 2110 (SMPTE IP Output)

SMPTE ST 2110 is the broadcast industry standard for professional video over IP. Major facilities (ESPN, BBC, Sky, Discovery) are migrating from SDI routers to all-IP plants based on ST 2110.

**How it differs from SDI:** SDI sends video, audio, and metadata as one signal on one cable. ST 2110 separates them into independently routable IP streams:

- **ST 2110-20**: Uncompressed video (raw pixels over RTP)
- **ST 2110-30**: Audio (PCM over RTP)
- **ST 2110-40**: Metadata / ancillary data
- **ST 2110-22**: Compressed video (JPEG XS — lower bandwidth variant)

Each stream is independently routable on standard 25/100GbE network switches — no proprietary SDI routers required.

**How it differs from NDI:** NDI is a convenience protocol for LAN production (compressed, auto-discovery, easy to use). ST 2110 is the professional infrastructure standard (uncompressed, PTP-timed, facility-scale). They serve different purposes and Pivox supports both.

**Timing — PTP instead of genlock:** ST 2110 facilities use PTP (Precision Time Protocol, IEEE 1588) for synchronization instead of physical blackburst/tri-level reference signals. A PTP grandmaster clock synchronizes all devices on the network to sub-microsecond accuracy. AJA cards handle PTP synchronization at the hardware level — the engine's frame scheduling works the same way regardless of whether timing comes from a physical genlock input or PTP.

**Bandwidth:**

| Format | Uncompressed (ST 2110-20) | JPEG XS (ST 2110-22) |
|---|---|---|
| 1080p59.94 | ~3 Gbps per stream | ~100-300 Mbps per stream |
| 2160p59.94 | ~12 Gbps per stream | ~400 Mbps-1 Gbps per stream |

Four channels with fill+key = 8 streams. Uncompressed 1080p requires ~24 Gbps total — needs 25GbE or 100GbE infrastructure. JPEG XS brings this down to ~1-2 Gbps, workable on 10GbE.

**Implementation:** Since AJA cards handle ST 2110 natively, the engine's AJA output adapter covers both SDI and ST 2110. Configuration determines which output mode is active. No separate output adapter needed.

### NDI (Network Video Output)

NDI (Network Device Interface) sends video over standard IP networks.

**Purpose:**
- Development preview on macOS without AJA hardware
- Network-based monitoring (any machine on the subnet can view output)
- Integration with NDI-capable mixers and software (vMix, TriCaster, OBS)
- Redundancy monitoring (view standby engine output remotely)

**Characteristics:**
- Discovery: mDNS (automatic — receivers see sources appear on the network)
- Codec: SpeedHQ (~100-150 Mbps per 1080p60 stream, visually lossless)
- Latency: ~1-3 frames
- Bandwidth: 4 channels × fill+key = 8 streams ≈ 1.2 Gbps (requires 10GbE)
- SDK: Free to use (binary library from Vizrt), C/C++ headers, Linux/macOS/Windows

**Implementation:**
- Thin C++ wrapper around NDI SDK
- Each channel announces two NDI sources: "Pivox CH1 Fill", "Pivox CH1 Key"
- Frame data passed directly from the frame pipeline — minimal copy

### MJPEG Preview (Remote Browser/Electron)

Low-bandwidth preview for operator UI.

- Rust encodes every Nth frame as JPEG at reduced resolution (720p)
- Served as HTTP multipart stream (`Content-Type: multipart/x-mixed-replace`)
- Consumed by `<img>` tag in browser or Electron
- Go API server proxies and authenticates the stream
- Target: 15fps, 100-200ms latency — sufficient for operator monitoring

### Output Recording (Compliance + Asset Ingest)

Broadcasters are legally required to keep a recording of everything that went to air. Pivox records the output and ingests it directly into the Pivox asset management system, indexed for semantic search while capturing.

```
Frame Pipeline
  ├──► AJA (on-air)
  ├──► NDI (monitoring)
  ├──► MJPEG (preview)
  └──► Recording adapter (Rust)
       │
       │  Encode via NVENC (H.264/HEVC, configurable bitrate)
       │  Mux into MP4 or MXF container
       │  Embed timecode + metadata (channel, rundown item, template)
       │  Record continuously while channel is on-air
       │
       ▼
  Local SSD (real-time write)
       │
       ▼
  Go control plane (async, parallel to recording)
       │
       ├──► Pivox Asset Manager (ingest as new asset)
       │    ├── Segment into clips (per rundown item or intervals)
       │    ├── Generate thumbnails and proxy
       │    ├── Index for semantic search (near real-time, during capture)
       │    │   - Template name + data fields on-air at each timecode
       │    │   - Rundown item metadata
       │    │   - AI content analysis (scene detection, OCR, speech-to-text)
       │    └── Searchable within seconds of going to air
       │
       ├──► Nearline storage (NAS/SAN)
       └──► Cloud storage (S3, GCS, Azure Blob)
```

**Key design: indexing happens during capture, not after.** As frames are recorded, the Go control plane processes them in parallel — generating thumbnails, extracting metadata from the playout state (which template, which data, which rundown item was on-air at each timecode), and feeding content to the search index. An operator can search for "show me every time we displayed the election board" moments after it aired.

The engine writes to local SSD in real-time — recording cannot depend on network availability. The Go control plane handles ingest, indexing, and transfer to nearline/cloud asynchronously. Retention policy (e.g., 30 days local, 7 years archived) is a Go control plane concern.

## Closed Captioning

Closed captioning is a regulatory requirement in most broadcast markets (FCC in the US, Ofcom in the UK, EU accessibility directives).

### How Captions Work in Broadcast

Captions are a **data sideband** embedded in the video signal, not burned-in text. They are carried in the SDI signal's **VANC (Vertical Ancillary Data)** space in CEA-608 (analog legacy) or CEA-708 (digital) format. For ST 2110, captions travel as a separate **ST 2110-40** metadata stream.

### Live Captioning (Stenographer / AI)

Live captions are handled by a **dedicated caption encoder** (EEG, Softel, Verbit) that sits **downstream** of Pivox in the SDI chain. Pivox does not handle live caption timing — the caption encoder receives text from the stenographer/AI service, encodes it into CEA-608/708 format, synchronizes it to the audio, and inserts it into the SDI signal's VANC.

```
Pivox AJA output (SDI — no live captions)
  │
  ▼
Caption encoder (EEG, Softel, etc.)  ← text from stenographer/AI
  │                                    ← encodes CEA-608/708
  │                                    ← syncs to audio
  │                                    ← inserts into VANC
  ▼
SDI with captions → transmission chain
```

Pivox does not need to handle live captioning — it's a separate specialized system in the signal chain.

### Pre-Produced Clip Captions (Pass-Through)

Video clips (MXF/MOV) may contain embedded caption data tracks. When Pivox plays these clips, FFmpeg extracts the caption data and the engine passes it through to the AJA card's VANC output, frame-synchronized with the video. This is automatic — the caption data is already timed to the clip's video frames.

```
MXF clip with embedded CEA-708
  │
  ▼
FFmpeg demuxes caption data stream (alongside video + audio)
  │
  ▼
Engine writes caption data to AJA VANC per frame (frame-synced)
  │
  ▼
SDI output includes captions
```

AJA's NTV2 SDK supports VANC insertion via `CNTV2Card::SetAncInsertMode()` and related APIs.

### Caption Detection and Alerting

When a video clip is loaded, FFmpeg immediately reports whether a caption track exists. The engine exposes this in the `SlotState` status stream (`has_captions`, `caption_format`). The Go control plane surfaces this in the operator UI:

- **CC detected:** Green indicator with format (e.g., "CEA-708")
- **No CC detected:** Warning indicator — operator sees the alert before and during playout

Whether missing CC blocks playout is a **configurable policy** in the Go control plane — rundown items can be marked as "CC required" and playout blocked if the clip lacks captions. This is an editorial/compliance decision, not an engine decision. The engine just reports `has_captions: true/false`.

## GPI (General Purpose Interface)

GPI provides physical button triggers and tally lights via the AJA card's built-in GPIO pins. Heavily used in broadcast facilities for critical operations. Supported day one.

Pivox targets AJA cards exclusively for GPI — no third-party USB or IP GPI devices. This keeps the hardware stack unified and reduces integration complexity.

### GPI Inputs (Physical Buttons → Engine Commands)

```
Operator panel / GPI button box
  │
  │  Button press → contact closure → AJA card GPI input pin
  │
  ▼
Rust supervisor detects GPI edge via NTV2 SDK
  │
  ▼
Maps to configured command:
  GPI 1 rising → PlayCommand on CH1 Layer 1
  GPI 2 rising → StopCommand on CH1 Layer 1
  GPI 3 rising → NextCommand on CH1 Layer 1
  GPI 4 rising → PlayCommand on CH2 Layer 1
  ...
```

### GPI Outputs (Engine State → Tally Lights)

```
Channel 1 transitions to on-air mode
  │
  ▼
Rust supervisor sets AJA card GPI output pin high
  │
  ▼
Tally light on operator panel illuminates
```

### Configuration

GPI mapping (which pin triggers which command, which state drives which output) is configuration managed by the Go control plane and pushed to the Rust supervisor at startup. The Rust supervisor handles hardware-level GPIO reading/writing via the AJA NTV2 SDK's GPI APIs.

AJA Corvid and Kona cards provide multiple GPI input/output pins. The exact count varies by card model.

## HDR (Future Capability)

HDR output is not required for day one but is acknowledged as a future capability. The frame pipeline is designed with a pluggable colorspace conversion stage to support HDR without rearchitecting. This section documents the full HDR pipeline so the architecture decisions made now don't prevent HDR support later.

### SDR Pipeline (Day One)

The current pipeline handles Standard Dynamic Range output:

```
CEF renders sRGB (8-bit, gamma 2.2, Rec.709 gamut)
  │
  ▼
Frame pipeline colorspace conversion (GPU shader):
  sRGB → Rec.709 (minimal — sRGB and Rec.709 share the same primaries,
                   only the transfer function differs slightly)
  │
  ▼
AJA card outputs Rec.709 SDI (8-bit or 10-bit 4:2:2)
```

This is straightforward — sRGB and Rec.709 are nearly identical color spaces.

### HDR Pipeline (Future)

HDR requires a fundamentally different colorspace conversion:

```
CEF renders sRGB (8-bit, gamma 2.2, Rec.709 gamut)
  │
  ▼
Frame pipeline HDR conversion (GPU shader):
  │
  │  Step 1: Linearize
  │  Remove sRGB gamma → linear light values
  │
  │  Step 2: Gamut mapping
  │  Rec.709 primaries → Rec.2020 primaries (wider color space)
  │  This is a 3x3 matrix transform on linear RGB values
  │
  │  Step 3: Tone mapping (SDR graphics → HDR range)
  │  Map the 0-100 nit SDR range into the HDR range
  │  - For HLG: map into 0-1000 nit range using HLG OETF
  │  - For PQ: map into 0-1000+ nit range using PQ EOTF
  │  This determines how bright/vivid the graphics appear
  │  against HDR video content
  │
  │  Step 4: Apply HDR transfer function
  │  - HLG (Hybrid Log-Gamma): ARIB STD-B67 OETF
  │  - PQ (Perceptual Quantizer): SMPTE ST 2084 EOTF
  │
  │  Step 5: Quantize to 10-bit
  │  8-bit sRGB → 10-bit HDR (required for both HLG and PQ)
  │
  ▼
AJA card outputs Rec.2020 HDR SDI (10-bit 4:2:2)
```

### The Compositing Problem

When mixing SDR graphics (CEF) with HDR video clips (FFmpeg), the compositor must handle mismatched color spaces:

```
HDR video clip (Rec.2020, PQ, 10-bit)     CEF graphic (sRGB, 8-bit)
  │                                          │
  ▼                                          ▼
  ┌──────────────────────────────────────────────┐
  │  Compositor (must operate in a single        │
  │  color space — choose one, convert the other)│
  │                                              │
  │  Option A: Composite in HDR space            │
  │  - Convert CEF sRGB → Rec.2020/PQ            │
  │  - Alpha-blend in HDR space                  │
  │  - Output HDR                                │
  │  ✓ Correct — HDR video is untouched          │
  │                                              │
  │  Option B: Composite in SDR space            │
  │  - Tone-map HDR video → SDR                  │
  │  - Alpha-blend in SDR space                  │
  │  - Output SDR                                │
  │  ✗ Loses HDR quality — defeats the purpose   │
  └──────────────────────────────────────────────┘
```

**Option A is correct.** The graphics are converted to HDR space, the video stays in HDR, and compositing happens in the HDR color space. The tone-mapping of SDR graphics into HDR must be carefully tuned so graphics look natural — not washed out (too dim) or eye-searing (too bright).

### HDR Standards in Broadcast

| Standard | Transfer Function | Use Case | Region |
|---|---|---|---|
| HLG (Hybrid Log-Gamma) | ARIB STD-B67 | Live broadcast — backward-compatible with SDR displays | BBC, NHK, common in Europe/Asia |
| PQ (Perceptual Quantizer) | SMPTE ST 2084 | Mastered content, streaming — not backward-compatible | Netflix, Dolby Vision, Disney+ |

**For broadcast, HLG is the more likely target** — it's designed for live production and is backward-compatible (an SDR display can show HLG content, just without the HDR benefit). PQ is primarily used for pre-mastered content.

### What the AJA Card Does and Doesn't Do

**AJA cards do NOT perform HDR conversion.** They output whatever pixel data the engine feeds them. If you feed Rec.709 data and the downstream chain expects Rec.2020/HLG, the graphics will look wrong (washed out, wrong colors, incorrect brightness).

**AJA cards DO support:**
- 10-bit and 12-bit output (required for HDR)
- Rec.2020 color space metadata signaling in SDI
- ST 2110 with HDR metadata (ST 2110-40)

### Architectural Impact

The frame pipeline's colorspace conversion is a pluggable stage. For SDR, it does sRGB → Rec.709 (CPU SIMD). For HDR, it does sRGB → Rec.2020 + PQ/HLG — this is more compute-intensive and may warrant GPU acceleration via `wgpu` (Rust WebGPU implementation) when the time comes. Switching between SDR and HDR is a conversion function swap + buffer format change (8-bit → 10-bit), not an architectural change.

**Day-one design decisions that enable future HDR:**
1. Frame pipeline uses pluggable colorspace conversion — swap the conversion function without rearchitecting
2. Compositor designed to operate on linear-light internally — enables correct blending in any color space
3. Buffer management supports 10-bit formats — size buffers for 10-bit from the start, even if day-one output is 8-bit
4. FFmpeg decodes HDR metadata — `av_frame_get_side_data()` provides mastering display info, content light level. Store and pass through even if not used yet.

## IPC Design

See `docs/protocols.md` for the canonical protobuf definitions (all message types, service definitions, enums).

### Go Control Plane ↔ Engine Supervisor

**Protocol:** gRPC over TCP (facility LAN). UDS only for single-machine deployments.

**Why gRPC:** Command latency (~0.3-1ms over LAN) is negligible vs. frame period (16.68ms at 59.94fps). Bidirectional streaming lets the engine push status continuously. Protobuf schemas are shared between Go and Rust — type-safe, versioned.

**Key RPCs:**
- `Execute` — streaming command channel (play, stop, update, video commands)
- `WatchStatus` — continuous status stream (channel health, layer state, frame counts)
- `SendInput` — remote mouse/keyboard/touch injection for preview/edit mode

### Engine Supervisor ↔ Channel Processes

**Protocol:** Raw length-prefixed protobuf over Unix domain sockets (local, no gRPC overhead). Same machine — supervisor and channel processes are co-located on the engine machine.

### Engine Supervisor ↔ Plugin Processes

**Protocol:** Plugin Protocol (gRPC + shared memory). See `docs/plugins/plugin-sdk.md`.

## Asset Preloading and Caching

Preloading is a **hybrid responsibility** split between the Go control plane (file transfer and caching) and the engine (content warm-up).

### The Problem

A rundown item may reference a template, images, fonts, and video clips stored in the Pivox nearline asset management system. Before the item can go on-air, all assets must be:
1. On local disk (downloaded from MAM)
2. Loaded into CEF or FFmpeg (warm, ready for instant play)

### Responsibility Split

**Go control plane — Asset Cache Manager:**
- Watches rundown state — knows what items are coming up
- Resolves asset references to local cache paths
- Pulls templates, images, fonts, and clips from nearline MAM to local SSD cache
- Manages cache eviction (LRU with disk budget, pins assets in current rundown)
- Reports cache readiness to the playout controller

**Engine — content warm-up:**
- Receives `LoadCommand` / `VideoLoadCommand` with **local file paths only**
- Loads template into CEF background slot (parse HTML, execute JS, call `onLoad()`)
- Opens clip in FFmpeg background slot (demux, seek to in-point, decode first frame)
- Reports preload state via status stream

**The engine never talks to the asset management system directly. The engine never downloads anything.**

### Preload Flow

```
┌─────────────────────────────────────────────────────────────┐
│  GO CONTROL PLANE                                            │
│                                                              │
│  Rundown:                                                    │
│    Item 1  ← ON AIR                                         │
│    Item 2  ← LOADED in engine (warm, instant play)          │
│    Item 3  ← CACHED on local disk (not loaded in engine)    │
│    Item 4  ← CACHING (download in progress from MAM)        │
│    Item 5  ← QUEUED (download not started)                  │
│    ...                                                       │
│    Item 20 ← NOT CACHED (will be fetched when closer)       │
│                                                              │
│  Asset Cache Manager:                                        │
│    - Maintains look-ahead window (configurable, e.g., 10)   │
│    - Fetches assets ahead of current on-air position        │
│    - Adapts based on: disk space, network bandwidth,         │
│      asset size, estimated time until air                    │
│    - Once cached: tells playout controller → sends           │
│      LoadCommand to engine with local file paths             │
│                                                              │
│  Playout Controller:                                         │
│    - Sends LoadCommand (local paths) to engine               │
│    - Tracks two readiness states per item:                   │
│      1. Cache ready (all files on disk) — Go's concern       │
│      2. Engine ready (loaded in BG slot) — engine reports    │
│    - Both must be READY before operator's PLAY is enabled    │
│                                                              │
├──────────────────────────────────────────────────────────────┤
│  ENGINE                                                      │
│                                                              │
│  Receives LoadCommand with local file paths:                 │
│    template: /cache/templates/lower-third/v2/                │
│    clip: /cache/media/replay_goal_43.mxf                     │
│    images: /cache/assets/espn_bug.png                        │
│                                                              │
│  Loads into background slot → reports READY via status       │
└──────────────────────────────────────────────────────────────┘
```

### Engine Preload Status

The engine reports preload state as part of the existing `SlotState` in the status stream. The `LOADED` status means the background slot is warm and ready for instant play. The Go control plane uses this to enable the operator's play button and inform automation systems.

## Remote Input Interaction

CEF's Off-Screen Rendering mode has no real window, but exposes APIs to inject mouse, keyboard, and touch events programmatically. This enables remote interaction for preview, template editing, and debugging — without needing a local display on the engine machine.

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

**Input protobuf messages:**

```protobuf
message InputEvent {
  int32 channel = 1;
  int32 layer = 2;              // target specific layer, or -1 for page-level
  oneof event {
    MouseMoveEvent mouse_move = 10;
    MouseClickEvent mouse_click = 11;
    MouseWheelEvent mouse_wheel = 12;
    KeyDownEvent key_down = 13;
    KeyUpEvent key_up = 14;
    KeyPressEvent key_press = 15;
    TouchEvent touch = 16;
  }
}

message MouseMoveEvent {
  int32 x = 1;
  int32 y = 2;
  uint32 modifiers = 3;        // shift, ctrl, alt bitmask
}

message MouseClickEvent {
  int32 x = 1;
  int32 y = 2;
  MouseButton button = 3;
  bool mouse_up = 4;
  int32 click_count = 5;
  uint32 modifiers = 6;
}

message MouseWheelEvent {
  int32 x = 1;
  int32 y = 2;
  int32 delta_x = 3;
  int32 delta_y = 4;
}

message KeyDownEvent {
  int32 key_code = 1;          // Windows virtual key code (CEF convention)
  uint32 modifiers = 2;
  bool is_system_key = 3;
}

message KeyUpEvent {
  int32 key_code = 1;
  uint32 modifiers = 2;
}

message KeyPressEvent {
  int32 char_code = 1;         // Unicode character
  uint32 modifiers = 2;
}

message InputAck {
  bool accepted = 1;
  string reason = 2;           // e.g., "channel is on-air, input rejected"
}

enum MouseButton {
  LEFT = 0;
  MIDDLE = 1;
  RIGHT = 2;
}
```

**Channel modes — input is gated by channel state:**

Each channel operates in one of four modes, set by the Go control plane:

| Channel Mode | Input Allowed | Output | Use Case |
|---|---|---|---|
| **On-Air** | No — events dropped with warning | AJA + NDI + MJPEG | Live broadcast — graphics on air |
| **Preview** | Yes | NDI + MJPEG (no AJA) | Operator previewing before taking on-air |
| **Edit** | Yes | MJPEG only | WYSIWYG template design — interactive editing |
| **Debug** | Yes | MJPEG only | Developer testing — DevTools access enabled |

The Rust supervisor enforces mode rules. If a channel is on-air and input events arrive, they are dropped and an `InputAck` with `accepted=false` is returned on the stream. The Go control plane transitions channels between modes (e.g., Preview → On-Air when the operator takes the channel live).

**Latency:** Input round-trip (click → render → MJPEG update in browser) is ~150-250ms. Acceptable for preview and editing. If the WYSIWYG editor needs lower latency, a future optimization is running a local CEF instance in the Electron editor for real-time editing, syncing the template to the engine only for on-air preview.

## JavaScript SDK

The SDK is injected into every template's page by CEF before the template loads. It defines the contract between the playout engine and template code.

See `docs/sdk.md` for the full SDK API reference. See `docs/templates.md` for the template authoring guide.

**SDK namespaces (summary):**

| Namespace | Purpose | Documented In |
|---|---|---|
| `pivox.model` | Reactive view model — declarative data bindings, watchers | `docs/sdk.md` |
| `pivox.feeds` | Shared memory data feed subscriptions with read throttle | `docs/sdk.md`, `docs/data-plane.md` |
| `pivox.native` | Hardware queries, frame timing, audio metering (Rust via V8 FFI) | `docs/sdk.md` |
| `pivox.system` | System data sources (time, timecode, channel info) — always available | `docs/sdk.md` |
| `pivox.assets` | Asset resolution and preloading | `docs/sdk.md` |
| `pivox.timing` | Genlock-synced frame callbacks | `docs/sdk.md` |
| `pivox.channel` | Channel info and safe areas | `docs/sdk.md` |
| `pivox.log` | Structured logging back to engine | `docs/sdk.md` |

**Engine responsibilities for the SDK:**
- Inject SDK JavaScript into CEF before template loads
- Forward `UpdateCommand` data to CEF → SDK patches view model → bindings fire
- Implement native bindings (Rust functions callable from JS via V8 → C++ → Rust FFI)
- Memory-map shared memory region for `pivox.feeds` subscriptions
- Provide system data sources (`pivox.system`) updated every frame
- Tick `pivox.timing.requestFrame` callbacks at genlock frame rate

## Live Data Updates and Data Plane

The Pivox Data Plane handles all live data delivery to on-air templates — connecting external feeds, routing data with operator control (auto/gated/manual), throttling, schema versioning, and high-performance shared memory delivery.

See `docs/data-plane.md` for the full Data Plane architecture, including:
- Two data paths: view model (gRPC push) and shared memory feeds (subscription-based)
- Shared memory implementation: hierarchical key-value with lock-free double buffer
- Two-layer throttling (write-side operator-controlled + read-side template-controlled)
- Feed schema versioning (validation at config/load time, not runtime)
- Operator controls (per-field gating, pause, override, approval)
- Template manifest data declarations
- Feed connector interface
- Capacity estimates

### Engine Responsibilities

The engine's role in the Data Plane is minimal:

1. **View model path:** Receive `UpdateCommand` via gRPC → forward to CEF → SDK patches view model → bindings fire
2. **Shared memory path:** Memory-map the shared memory region at startup → SDK reads per frame → fires subscription callbacks

The engine does not validate, throttle, route, or gate data. It reads what it's given and renders it. All Data Plane logic lives in the Go control plane.

### SDK APIs for Data

Two SDK namespaces handle data — see [JavaScript SDK](#javascript-sdk) for full API:

- **`pivox.model`** — reactive view model bindings (push, declarative). For operator-controlled fields.
- **`pivox.feeds`** — shared memory feed subscriptions (subscribe, callback-based, with read throttle). For high-frequency live data.

## GPU and Container Strategy

### GPU Requirements

For simple HTML/CSS animations (lower thirds, tickers, scoreboards — 99% of workloads):

| Resolution | Per-channel GPU load | Realistic channels/GPU |
|---|---|---|
| 1080p59.94 | ~2-5% GPU, ~200-500MB VRAM | 12-20 channels |
| 2160p59.94 | ~8-15% GPU, ~800MB-1.5GB VRAM | 4-8 channels |

Bottlenecks are typically VRAM, PCIe bandwidth (GPU → CPU → AJA card), and CPU (CEF's Blink layout engine), not GPU compute.

WebGPU workloads (complex 3D, particle systems) will consume significantly more GPU. These are expected to be rare today but will grow as the technology matures.

### GPU Usage — Two Separate Concerns

There are two distinct GPU consumers in the system. It's important not to confuse them:

**1. CEF/Chromium GPU rendering (always uses GPU, not our code)**

CEF uses the GPU for **all** page rendering — even basic HTML/CSS. This is Chromium's standard GPU-accelerated compositing pipeline:

- CSS transforms, animations, transitions → GPU-accelerated
- Text rendering, anti-aliasing → GPU
- Page layer compositing → GPU
- Canvas2D, WebGL, WebGPU → GPU
- Even a simple `<div>` with a background color → GPU composited

A better GPU = smoother CEF rendering = more channels per machine. This is why a dedicated NVIDIA GPU matters even for "simple" HTML/CSS templates. We don't control this — CEF manages its own GPU pipeline automatically.

**2. Our frame pipeline (after CEF gives us pixels)**

After CEF renders a frame and delivers the pixel buffer via `OnPaint()`, **our Rust code** processes it:

```
CEF renders (GPU) → OnPaint() delivers pixel buffer → Our pipeline processes it
```

| Operation | Who Does It | Approach |
|---|---|---|
| All HTML/CSS/WebGL/WebGPU rendering | CEF (automatic, always GPU) | Not our code — Chromium handles it |
| Colorspace conversion (sRGB → Rec.709) | Our frame pipeline (Rust) | CPU SIMD — this is a near-no-op |
| Composite CEF layers with video layers | Our compositor (Rust) | CPU SIMD — alpha blending a few layers is CPU-feasible |
| Fill+key split | Our frame pipeline (Rust) | CPU — trivial byte operation |
| Video decode | FFmpeg | Hardware decode (NVDEC/VAAPI/VideoToolbox) — automatic |
| Compliance recording | NVENC | Hardware encoder — fixed-function, not shader code |

**Our frame pipeline uses CPU SIMD for day one.** We don't write native GPU shaders for colorspace conversion or compositing unless profiling on production hardware proves CPU is insufficient. If GPU compute is ever needed (e.g., HDR tone mapping at 4K60), use `wgpu` (Rust WebGPU implementation) as a cross-platform abstraction rather than vendor-specific APIs.

### GPU Strategy — WebGPU as the 3D Platform

For template authors who need 3D graphics, particle systems, and shader effects, **WebGPU through CEF is the platform** — not native GPU APIs.

**Why:**
- WebGPU is cross-platform — templates work on any machine, any OS
- CEF inherits Chromium's Dawn WebGPU implementation — mature, continuously improved
- AI-assisted development will make 3D browser graphics increasingly accessible — template designers won't need Unreal/Unity expertise to create sophisticated broadcast graphics
- The browser 3D stack (WebGPU + Three.js/Babylon.js + AI code generation) will reach broadcast quality for the vast majority of use cases
- No native GPU shaders to maintain across Metal + Vulkan + DirectX

### No Containers for the Render Path

The engine runs as **bare-metal processes**, not in containers.

**Rationale:**
- GPU passthrough in K8s adds complexity (NVIDIA device plugin, MIG/time-slicing) for no benefit
- PCI playout card passthrough cannot be easily shared across containers
- Extra memory copies crossing container boundaries to reach hardware increase latency
- The engine runs on 1-3 dedicated physical servers — K8s orchestration overhead is not justified

**Process model:** The Rust channel supervisor manages one OS process per channel. Process isolation provides crash containment (channel 3 crashing doesn't affect channel 1). The supervisor handles health monitoring, automatic restart, and resource limits via cgroups directly.

**Containers for the control plane:** The Go services (API, NRCS, monitoring, gateways) can run in containers/K8s. They communicate with the engine over gRPC via Unix domain sockets (co-located) or TCP (if on separate machines).

## Redundancy

### Hot Standby with State Replication

```
                    ┌──────────────────┐
                    │  Go Control Plane │
                    │  (dual-writes     │
                    │   every command   │
                    │   to both engines)│
                    └───┬──────────┬───┘
                        │          │
              ┌─────────▼──┐  ┌───▼─────────┐
              │  ENGINE A   │  │  ENGINE B   │
              │  (PRIMARY)  │  │  (STANDBY)  │
              │             │  │             │
              │ CH1 ■ on-air│  │ CH1 ■ ready │
              │ CH2 ■ on-air│  │ CH2 ■ ready │
              │ CH3 ■ on-air│  │ CH3 ■ ready │
              │ CH4 ■ on-air│  │ CH4 ■ ready │
              └──────┬──────┘  └──────┬──────┘
                     │                │
                     ▼                ▼
              ┌──────────┐    ┌──────────┐
              │ AJA Card │    │ AJA Card │
              └─────┬────┘    └────┬─────┘
                    │              │
                    ▼              ▼
              ┌──────────────────────────┐
              │  Changeover Switch       │
              │  (Nevion, Evertz, or     │
              │   mixer input select)    │
              └──────────┬───────────────┘
                         │
                         ▼
                    SDI to Mixer
```

**How it works:**

1. Both engines render simultaneously. Engine B runs identical CEF instances with identical templates and data.
2. The Go control plane dual-writes every command to both engines. Both engines execute every play, stop, update, and next command.
3. A hardware changeover switch takes Engine A's output by default. If Engine A fails, the switch cuts to Engine B cleanly — same frame, same graphic.
4. The Rust supervisor on each engine monitors CEF process health, AJA card status, genlock lock, and frame scheduling. It reports status to the changeover switch via GPI (contact closure) or protocol.

**What gets replicated:**
- Every command (play, stop, update, next, video_load, video_play, video_seek, etc.) with ordering guarantees
- Template data changes (UpdateCommand / view model patches)
- Video playback state (clip position, speed, in/out points)
- Layer state per channel (foreground/background slots)
- Channel modes (on-air, preview, edit, debug)

**What does NOT get replicated:**
- Frame buffers — each engine renders independently from the same commands
- Decoded video frames — each engine decodes independently

## Platform Support

### Support Tiers

| Tier | Platforms | AJA Hardware | Purpose |
|---|---|---|---|
| **Dev (Supported)** | macOS (Apple Silicon + Intel), Windows 10/11 | Certified Thunderbolt devices (optional — NDI is the primary dev preview) | Template development, design, iteration |
| **Staging/Prod (Certified)** | Headless Linux (distro TBD), Windows Server 2022 | Certified PCIe cards only | On-air broadcast, staging, QA |

Linux is **not** a supported dev platform. Dev is macOS and Windows only.

### Certified Hardware Matrix

| Tier | AJA Devices | Notes |
|---|---|---|
| Dev (Thunderbolt) | Kona 5, Io 4K Plus | Optional — most developers use NDI only |
| Prod/Staging (PCIe) | Corvid 88, Corvid 44 12G | Same cards certified on both Linux and Windows Server |

| Component | Certified Options |
|---|---|
| GPU | NVIDIA RTX A4000, A5000, L40, L40S (specific driver versions TBD) |
| CPU | Minimum spec TBD after profiling |
| RAM | Minimum spec TBD after profiling |
| Storage | NVMe SSD required (minimum spec TBD — needed for compliance recording) |

This matrix is published and maintained. Customers deploy from the list. Anything off-list is unsupported.

### Template Development Workflow

```
Tier 1: Browser (fastest iteration)
  - Developer opens template HTML in Chrome/Firefox
  - Includes pivox-sdk-mock.js for SDK simulation
  - Standard browser DevTools for debugging
  - Hot module reload via Vite/webpack/etc.
  - No engine running — pure frontend development
  - Good for: layout, CSS, animations, data binding logic

Tier 2: Local Engine + NDI (daily workflow)
  - Developer runs Pivox engine locally (macOS or Windows)
  - Pivox Electron app for operator UI
  - NDI output — view in NDI Monitor (free, same machine or any device on network)
  - Real PivoxSDK — native bindings, view model, timing
  - Template hot-reload: file watcher detects changes, reloads CEF page,
    preserves view model state
  - Good for: SDK integration, transitions, data binding validation
  - No AJA hardware needed

Tier 3: Staging Engine (pre-air validation)
  - Staging server: same hardware + OS + AJA card as production
  - Template deployed via asset management
  - QA runs through all data scenarios, automation tests
  - Load testing, edge case data
  - NDI preview on facility network for review
  - Output recorded for sign-off
  - Good for: final acceptance before going on-air

Tier 4: Production (on-air)
  - Approved templates only
  - Identical config to staging
```

### macOS (Dev)

- CEF OSR builds and runs natively
- Rust frame pipeline: CPU SIMD for colorspace conversion and compositing
- NDI output for preview (primary dev output — no hardware needed)
- AJA output via certified Thunderbolt device (optional — for SDI validation)
- MJPEG preview for Electron operator UI

### Windows (Dev)

- CEF OSR builds and runs natively
- Rust frame pipeline: CPU SIMD for colorspace conversion and compositing
- NDI output for preview (primary dev output)
- AJA output via certified Thunderbolt device (optional)
- MJPEG preview for Electron operator UI

### Linux (Staging/Production — Headless)

- CEF OSR headless with `--use-gl=egl`
- Colorspace conversion and compositing: CPU SIMD (GPU only if profiling shows need)
- AJA output via certified PCIe card
- NDI output for network monitoring
- MJPEG preview for remote operator UI
- GPI via AJA card
- Compliance recording via NVENC
- Go control plane services may run in containers
- Engine processes run on bare metal, managed by Rust supervisor

### Windows Server (Staging/Production — Headless)

- Same capabilities as Linux production
- Required for future Unreal Engine integration (Phase 6)
- Same certified PCIe AJA cards as Linux

## Project Structure

```
pivox/
├── cmd/
│   ├── pivox-server/           # Go — main API/NRCS/control plane
│   ├── pivox-mos-gateway/      # Go — MOS protocol bridge
│   └── pivox-monitor/          # Go — health/alerting
│
├── internal/                   # Go internal packages
│   ├── nrcs/                   # Rundowns, stories, graphic items
│   ├── templates/              # Template registry, versioning
│   ├── assets/                 # Asset management + cache manager
│   ├── dataplane/              # Data Plane — feed routing, gating, throttling, shared memory writes
│   ├── playout/                # Playout state machine, layer stack
│   ├── redundancy/             # State replication, failover
│   ├── mos/                    # MOS protocol implementation
│   └── api/                    # gRPC + REST handlers
│
├── pkg/                        # Go public libraries
│   └── sdk/                    # Go client SDK for integrations
│
├── proto/                      # Protobuf definitions (shared Go + Rust)
│   ├── playout.proto           # Commands: play, stop, update, video_*
│   ├── channel.proto           # Channel status, health, layer state
│   ├── input.proto             # Remote input: mouse, keyboard, touch events
│   └── preview.proto           # MJPEG preview signaling
│
├── engine/                     # Rust + C++ render engine
│   ├── cef-host/               # C++ — CEF initialization, OSR, JS injection
│   ├── video-engine/           # Rust + C (FFmpeg) — clip playback, decode, seek
│   ├── compositor/             # Rust — merge video + graphics layers, transitions
│   ├── frame-pipeline/         # Rust crate — buffer mgmt, colorspace, fill+key
│   ├── aja-output/             # Rust + C++ — AJA NTV2 adapter
│   ├── ndi-output/             # Rust + C++ — NDI send adapter
│   ├── mjpeg-preview/          # Rust — MJPEG HTTP streaming
│   ├── recording/              # Rust — compliance recording (NVENC encode)
│   ├── gpi/                    # Rust + C++ — GPI input/output handling
│   ├── captions/               # Rust + C++ — closed caption / VANC embedding
│   ├── supervisor/             # Rust — channel process manager
│   └── sdk-inject/             # JavaScript — PivoxSDK + browser mock
│
├── web/                        # Frontend
│   ├── operator/               # Operator UI (browser + Electron)
│   ├── editor/                 # WYSIWYG template editor (future)
│   └── electron/               # Electron shell
│
├── templates/                  # Built-in templates
│   ├── lower-third/
│   ├── ticker/
│   ├── scoreboard/
│   ├── audio-visualizer/
│   │   ├── waveform/
│   │   ├── vu-meter/
│   │   ├── spectrum/
│   │   └── minimal/
│   └── test-signals/
│       ├── smpte-bars/
│       └── slate/
│
├── deployments/
│   ├── docker/                 # Dockerfiles for Go services
│   └── k8s/                    # Helm charts for control plane
│
├── scripts/
│   ├── build-engine.sh         # Build Rust + C++ engine
│   ├── build-cef.sh            # Download / build CEF
│   └── dev-setup.sh            # macOS / Linux dev environment setup
│
├── docs/
│   ├── architecture.md         # System deployment, hybrid/cloud/on-prem
│   ├── engine.md               # This document — engine core
│   ├── control-plane.md        # NRCS, operator UI, services
│   ├── data-plane.md           # Live data feeds, shared memory
│   ├── sdk.md                  # JavaScript SDK API
│   ├── protocols.md            # Protobuf definitions
│   ├── hardware.md             # AJA, genlock, GPI, CC, NDI, HDR
│   ├── templates.md            # Template authoring guide
│   ├── tooling.md              # Dev tools, Rive, CLIs
│   ├── licensing.md            # FFmpeg LGPL, codec patents
│   ├── glossary.md             # Broadcast terminology
│   └── plugins/
│       ├── plugin-sdk.md       # Plugin Protocol & SDK
│       ├── plugin-cef.md       # CEF plugin
│       ├── plugin-ffmpeg.md    # FFmpeg plugin
│       ├── plugin-rive.md      # Rive plugin
│       └── plugin-unreal.md    # Unreal plugin (future)
│
├── go.mod
├── go.sum
├── Cargo.toml                  # Rust workspace root
└── Makefile
```

### Rust Workspace

`Cargo.toml` at project root:

```toml
[workspace]
members = [
    "engine/video-engine",
    "engine/compositor",
    "engine/frame-pipeline",
    "engine/aja-output",
    "engine/ndi-output",
    "engine/mjpeg-preview",
    "engine/recording",
    "engine/gpi",
    "engine/captions",
    "engine/supervisor",
]
```

C++ components (`cef-host`, parts of `aja-output`, `ndi-output`) build via CMake, invoked from `Makefile` or `scripts/build-engine.sh`. Rust crates that need C++ interop use `cc` or `cmake` crates in `build.rs`.

## Hardware Reference

See `docs/hardware.md` for full hardware documentation including AJA card specifications, certified hardware matrix, GPI details, closed captioning, ST 2110, genlock timing, audio pipeline, HDR, and platform support tiers.

## Additional Engine Capabilities

### Multi-Format Support

Channels are configured for a specific output format at startup. The engine supports:

| Format | Frame Rate | Notes |
|---|---|---|
| 1080p59.94 | 59.94fps | US HD standard (progressive) |
| 1080p50 | 50fps | EU HD standard (progressive) |
| 1080i59.94 | 29.97fps (PsF) | US HD legacy — AJA card handles field splitting |
| 1080i50 | 25fps (PsF) | EU HD legacy — AJA card handles field splitting |
| 720p59.94 | 59.94fps | Used by some US networks (ABC, ESPN legacy) |
| 720p50 | 50fps | EU variant |
| 2160p59.94 | 59.94fps | UHD — requires 12G-SDI or ST 2110 |
| 2160p50 | 50fps | UHD EU variant |

Different channels on the same engine can run at different formats (e.g., CH1 at 1080p59.94, CH2 at 1080i59.94) — each channel process ticks CEF at its own frame rate independently.

### Tally (TSL UMD Protocol)

Vision mixers send tally signals to indicate which source is currently on-air (program) or in preview. The Go control plane receives tally via TSL UMD (Television Systems Ltd, Universal Monitor Driver) protocol — the industry standard for tally distribution.

When a Pivox channel's tally state changes (e.g., mixer cuts to Pivox CH1), the Go control plane:
- Updates the operator UI (red = on-air, green = preview)
- Can trigger automated actions (e.g., auto-play a graphic when the channel goes on-air)
- Surfaces the state in `pivox.system.channel.tally` for templates that need tally awareness

This is a Go control plane concern — the engine is unaware of tally.

### Still Image Support

Static images (PNG, JPEG, TGA, TIFF) can be loaded as a full-frame layer. Used for holding slides, standby cards, sponsor logos, and test patterns.

Implemented via FFmpeg — a still image is a single-frame video. The engine loads it with `VideoLoadCommand`, FFmpeg decodes one frame, and it's held on screen indefinitely. No special still-image code path needed.

### Audio-Only Playback and Visualization

Audio-only files (WAV, MP3, FLAC, AAC) are played via FFmpeg on a video layer — FFmpeg handles them as a media file with no video stream. PCM audio goes to the audio mixer and out to AJA/NDI. No video frames are produced.

For visual presentation (e.g., phone interviews on news shows), the Go control plane **bundles** an audio layer with a visualizer template as a single operator action:

```
Operator clicks "Play Audio" on a rundown item
  │
  ▼
Go control plane sends multiple commands (single operator action):
  1. VideoLoadCommand → Layer 0 (audio file via FFmpeg)
  2. LoadCommand → Layer 1 (visualizer template, with audio_layer=0 in view model)
  3. LoadCommand → Layer 2 (lower-third template, with name/title data)
  │
  ▼
Engine plays audio, visualizer reads levels, lower-third displays — all composited
```

The operator sees one rundown item. The control plane handles layer assignment and wiring. The operator never sees layer numbers.

**Visualizer templates** read `pivox.native.getAudioLevels({ layer: N })` to render audio visualization. The control plane passes the audio layer ID into the visualizer's view model so it knows which layer to monitor:

```javascript
// Built-in waveform visualizer template
onLoad(model) {
  this.audioLayer = pivox.model.get('audio_layer');
}

onPlay() {
  pivox.timing.requestFrame(() => this.draw());
  pivox.ready();
}

draw() {
  const levels = pivox.native.getAudioLevels({ layer: this.audioLayer });
  // Render waveform / VU meter / spectrum — whatever the theme does
  this.renderWaveform(levels);
  pivox.timing.requestFrame(() => this.draw());
}
```

**Built-in visualizer themes** ship alongside other built-in templates:

```
templates/
  ├── lower-third/
  ├── ticker/
  ├── scoreboard/
  └── audio-visualizer/
       ├── waveform/          # animated waveform bars
       ├── vu-meter/          # classic VU meter
       ├── spectrum/          # frequency spectrum analysis
       └── minimal/           # simple level indicator + title
```

Custom visualizer themes are just more templates — designers create branded versions that match the show's look, using the same SDK and `getAudioLevels()` binding.

**This pattern — the Go control plane bundling multiple engine commands into a single operator action — applies broadly.** Audio playback is one example. Others include: loading a graphics package (multiple coordinated templates across layers), or setting up a video call (video layer + name strap + show branding). The engine deals with individual layers and commands. The control plane translates operator intent into engine commands.

### Test Signal Generator

Every playout device must be able to generate standard test signals for facility calibration and troubleshooting:

- **Color bars** (SMPTE, EBU) + reference tone (1kHz at -20dBFS)
- **Slate** (channel ident, text overlay with station info)

Implemented as built-in templates or FFmpeg test sources (`-f lavfi -i testsrc`, `smptebars`). No special engine code — just pre-installed templates and a CLI/API command to activate them.

### Automatic Rundown Advance (Timers)

The Go control plane supports automatic cycling through rundown items at configured intervals. This is a control plane feature — the engine just receives PlayCommand when the timer fires.

**Frame-accurate timing:** The control plane does not use wall-clock timers. It subscribes to the engine's `WatchStatus` stream and counts `frames_rendered` from `ChannelStatus`. This ensures timer accuracy is synced to genlock, not system clock.

| Timer Mode | Behavior |
|---|---|
| Fixed interval | Advance every N seconds (converted to frame count at channel frame rate) |
| Per-item duration | Each rundown item has its own duration |
| Timecode-triggered | Advance at specific SMPTE timecodes |
| Manual with countdown | Show countdown in operator UI, auto-advance at zero (operator can override/hold) |

## Broadcast Integration Points

| Integration | Protocol | Layer | Purpose |
|---|---|---|---|
| Newsroom (ENPS/iNEWS) | MOS (XML/TCP) — to be superseded by Pivox protocol | Go control plane | Rundown-driven graphics |
| Video server automation | VDCP (RS-422/TCP) | Go control plane | Trigger graphics from playout automation |
| Vision mixer / switcher | TSL UMD / Ember+ | Go control plane | Tally status, auto-transition triggers |
| Timing reference | Blackburst / Tri-level sync / PTP | Engine (AJA card) | Genlock — physical signal or PTP clock |
| Live data feeds | JSON / XML / WebSocket | Go Data Plane | Live scores, election results, tickers |
| Asset management | REST API | Go asset cache manager | Templates, clips, logos, images, fonts |
| Changeover switch | GPI / serial protocol | Engine supervisor | Redundancy failover signaling |

**MOS replacement (strategic goal):** MOS is an outdated XML-over-TCP protocol from the early 2000s. A long-term goal is for Pivox to define a modern NRCS integration protocol (gRPC/protobuf-based, real-time, bidirectional streaming) that newsroom system vendors can integrate against. This is a Go control plane / protocol design initiative, not an engine concern. To be designed separately.

## Development Phases

### Phase 1a — Single Channel, Graphics + Video, NDI Output (macOS)

Development platform: macOS. No AJA hardware required.

- CEF OSR in C++, single channel
- FFmpeg video engine: clip load, play, pause, seek, variable speed
- Rive plugin: native C/C++ runtime as a separate plugin process via Plugin SDK. WASM-in-CEF available as alternative for simpler animations mixed with HTML.
- Compositor: merge video (FFmpeg) + graphics (CEF, including Rive WASM) layers into single output
- Foreground/background slots per layer with preloading
- Transition engine: cut, mix/dissolve between FG/BG
- Rust frame pipeline: colorspace conversion (CPU/SIMD), fill+key split
- NDI output: fill and key as separate NDI sources
- Audio pipeline: mix CEF + FFmpeg audio, embed in NDI
- PivoxSDK: lifecycle hooks (onLoad, onPlay, onStop), view model bindings, native bindings
- gRPC control API: graphics commands + video playback commands + transitions
- MJPEG preview server for browser/Electron
- Remote input interaction (mouse/keyboard via gRPC for preview mode)

This phase validates: CEF works, FFmpeg decode works, Rive renders correctly, compositor merges all source types, FG/BG preloading works, transitions render correctly, SDK contract is sound, NDI output is functional, video playback commands work end-to-end.

### Phase 1b — AJA SDI Output (Linux)

Development platform: Linux with AJA PCIe card (Corvid 88 or Corvid 44 12G).

- AJA NTV2 output adapter: fill+key to SDI
- Audio embedding in SDI output (16 channels, configurable mapping)
- Genlock synchronization to house reference
- PsF output for 1080i facilities
- Colorspace conversion and compositing on production hardware (CPU SIMD, GPU only if profiling shows need)
- GPI input/output via AJA card (button triggers + tally lights)
- Closed caption pass-through (FFmpeg clip CC → AJA VANC)
- Compliance recording (NVENC encode to local disk)
- Verify frame-accurate, genlock-locked output on SDI
- NDI continues to run alongside AJA output

This phase validates: hardware output works, genlock timing is correct, no dropped frames under load, audio is embedded correctly, GPI triggers work, captions pass through, recording captures output.

### Phase 2 — Multi-Channel + Operational Readiness

- Rust channel supervisor managing 4+ CEF/video processes
- Layer stack per channel (multiple simultaneous video + graphics layers)
- Full transition library (push, wipe variants, DVE)
- Custom shader transitions
- Full gRPC control API (all commands in protobuf schema)
- Bidirectional status streaming (channel health, on-air state, frame counts, video position/timecode, transition state)
- Output routing: channel → physical SDI output mapping
- MJPEG preview per channel
- Channel mode enforcement (on-air, preview, edit, debug)
- Caption sideband input (accept from external caption encoders via gRPC/UDP)
- GPI mapping configuration (Go control plane)
- Recording management (start/stop, retention, transfer to nearline/cloud)
- Process crash recovery and automatic restart

### Phase 3 — Authoring + Operator UI

- Operator web UI with rundown management and transition selection
- Template registry and asset management
- Clip/media browser and management
- Asset cache manager (look-ahead preloading from nearline MAM)
- WYSIWYG template editor
- Browser and Electron preview via MJPEG with remote interaction

### Phase 4 — Redundancy + Integration

- Hot standby state replication (dual-write from Go control plane)
- Changeover monitoring (GPI / protocol to hardware switch)
- MOS gateway for external NRCS systems
- VDCP gateway for automation integration

### Phase 5 — Production Hardening

- Full monitoring and alerting (Prometheus metrics, Grafana dashboards)
- Template versioning and rollback
- Performance profiling at scale (GPU utilization, frame timing histograms, decode performance)
- ST 2110 testing with PTP infrastructure (when facility access is available)
- Certification testing with AJA hardware across Linux and Windows Server
- HDR pipeline implementation (if customer demand warrants)
- Disaster recovery procedures

### Phase 6 — Plugin SDK and Third-Party Engine Integration

#### Pivox Plugin Protocol

Define and publish a **Pivox Plugin Protocol** — an open spec that any external renderer can implement to become a source in Pivox's compositor. This is a platform play: instead of Pivox building integrations for every 3D engine, third parties build plugins that speak the Pivox protocol.

**Critical design principle: Pivox's own built-in engines (CEF, FFmpeg, and Rive) are built on top of this same Plugin SDK.** The protocol is not an afterthought or second-class interface — it is the interface. If the built-in plugins work on it, third-party plugins will too. The compositor treats every source identically regardless of origin.

**The protocol defines:**

1. **Command reception** — play, stop, update, load, next, clear (same command set as CEF templates)
2. **Frame delivery** — RGBA pixel buffers via shared memory at the channel's frame rate
3. **Audio delivery** — PCM samples synced to frames via shared memory
4. **Status reporting** — loaded, playing, stopped, health, errors
5. **Genlock sync** — render at the frame rate Pivox specifies

```protobuf
service PivoxPluginHost {
  // Pivox → Plugin: commands
  rpc Configure (PluginConfig) returns (PluginConfigAck);
  rpc Execute (stream PluginCommand) returns (stream PluginCommandAck);
}

service PivoxPluginClient {
  // Plugin → Pivox: frame delivery + status
  rpc DeliverFrames (stream PluginFrame) returns (stream FrameAck);
  rpc ReportStatus (stream PluginStatus) returns (Empty);
}

// Pivox sends configuration to the plugin at startup
message PluginConfig {
  int32 width = 1;
  int32 height = 2;
  float frame_rate = 3;           // plugin must render at this rate
  string shared_memory_path = 4;  // for frame delivery (zero-copy)
  int32 audio_sample_rate = 5;    // 48000
  int32 audio_channels = 6;
}

// Plugin responds with its capabilities — tells Pivox what it can do
message PluginConfigAck {
  bool accepted = 1;
  string error = 2;
  PluginCapabilities capabilities = 3;
}

message PluginCapabilities {
  string name = 1;                       // "CEF HTML Renderer", "FFmpeg Video", "Unreal Engine 5"
  string version = 2;                    // plugin version
  PluginType type = 3;                   // GRAPHICS, VIDEO, AUDIO, HYBRID

  // Supported commands
  bool supports_load = 10;              // can pre-load / cue
  bool supports_play = 11;
  bool supports_stop = 12;
  bool supports_update = 13;            // can receive live data updates
  bool supports_next = 14;              // multi-step / multi-page
  bool supports_seek = 15;             // frame-accurate seeking (video)
  bool supports_variable_speed = 16;   // slow-mo, reverse (video)
  bool supports_loop = 17;

  // Output capabilities
  bool outputs_video = 20;             // delivers RGBA frames
  bool outputs_audio = 21;             // delivers PCM audio
  bool outputs_alpha = 22;             // RGBA includes meaningful alpha channel
  bool outputs_captions = 23;          // can extract/pass-through closed captions

  // Content capabilities
  repeated string supported_formats = 30;    // file extensions: [".html", ".js"] or [".mxf", ".mov", ".mp4"]
  repeated string supported_codecs = 31;     // codec names: ["h264", "prores", "dnxhr"]
  map<string, string> custom_metadata = 40;  // engine-specific capabilities
}

enum PluginType {
  GRAPHICS = 0;     // CEF, Rive, etc. — primarily graphics/animation
  VIDEO = 1;        // FFmpeg — primarily video/clip playback
  AUDIO = 2;        // audio-only playback
  HYBRID = 3;       // Unreal, Godot — video + graphics + audio
}

message PluginCommand {
  string request_id = 1;
  oneof command {
    PluginLoadCommand load = 10;    // load project/scene/file + initial data
    PluginPlayCommand play = 11;
    PluginStopCommand stop = 12;
    PluginUpdateCommand update = 13; // patch data (same as view model update)
    PluginNextCommand next = 14;
    PluginClearCommand clear = 15;
  }
}

message PluginLoadCommand {
  string project = 1;    // engine-specific: UE level, Godot scene, Rive file, etc.
  bytes data = 2;         // JSON — initial data
}

message PluginUpdateCommand {
  bytes data = 1;         // JSON — data patches
}

message PluginFrame {
  uint64 frame_number = 1;
  // Pixel data (RGBA) delivered via shared memory, not over gRPC
  // This message signals "frame N is ready at the shared memory location"
  bool has_audio = 2;
  // Audio (PCM) delivered via separate shared memory region
}

message PluginStatus {
  PluginState state = 1;
  string error = 2;
  map<string, string> metadata = 3;  // engine-specific info
}

enum PluginState {
  IDLE = 0;
  LOADING = 1;
  READY = 2;        // equivalent to LOADED — warm, ready for instant play
  PLAYING = 3;
  ERROR = 4;
}
```

**What Pivox publishes (not open-source — published spec + SDK):**

1. **Pivox Plugin Protocol spec** — the protobuf definitions, documented with expected behavior and timing requirements
2. **Pivox Plugin SDK** — a small library (C, Rust, and C++ bindings) that handles gRPC connection, shared memory setup, frame delivery boilerplate, and genlock sync. A plugin author includes this SDK and implements a few callbacks. ~500-1000 lines of code.
3. **Reference plugin** — a minimal example (e.g., simple OpenGL renderer drawing a spinning cube) demonstrating the full integration

**What a plugin author implements:**

```rust
// Example: Rive plugin — what the third-party developer writes
struct RivePlugin {
    rive_runtime: RiveRuntime,
}

impl PivoxPlugin for RivePlugin {
    fn on_load(&mut self, project: &str, data: &[u8]) {
        self.rive_runtime.load_file(project);
        self.rive_runtime.set_inputs(data);
    }
    fn on_play(&mut self) { self.rive_runtime.play(); }
    fn on_update(&mut self, data: &[u8]) { self.rive_runtime.set_inputs(data); }
    fn on_stop(&mut self) { self.rive_runtime.stop(); }

    fn render_frame(&mut self, buffer: &mut [u8], width: u32, height: u32) {
        self.rive_runtime.render_to_buffer(buffer, width, height);
    }
}

fn main() {
    pivox_plugin_sdk::run(RivePlugin::new());
    // SDK handles: gRPC, shared memory, frame sync, status reporting
}
```

**How built-in engines use the same SDK:**

```
┌──────────────────────────────────────────────────────────────┐
│  Pivox Channel Process                                        │
│                                                               │
│  ┌──────────────────────────────────────────────────────┐    │
│  │              Plugin Receiver (Rust)                    │    │
│  │  Accepts N plugin connections — each becomes a layer   │    │
│  └──┬─────────────────┬─────────────────┬───────────────┘    │
│     │                 │                 │                      │
│     ▼                 ▼                 ▼                      │
│  CEF Plugin        FFmpeg Plugin     (any 3rd-party plugin)   │
│  (built-in,        (built-in,        connected via            │
│   ships with        ships with        Plugin Protocol)        │
│   Pivox)            Pivox)                                    │
│     │                 │                 │                      │
│     │  All use the same Plugin SDK     │                      │
│     │  All deliver frames via shared memory                   │
│     │  All receive commands via the same protocol             │
│     ▼                 ▼                 ▼                      │
│  ┌──────────────────────────────────────────────────────┐    │
│  │                    Compositor                         │    │
│  │           (treats all sources identically)            │    │
│  └──────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

#### Third-Party Engine Evaluation

Once the Plugin SDK is stable (proven by CEF and FFmpeg running on it), evaluate third-party engines for official Pivox-built plugins:

**3D engines:**

| Engine | Language | Linux | In-Process | Notes |
|---|---|---|---|---|
| Unreal Engine | C++ | Limited (editor Windows, runtime Linux) | No — separate process | Industry standard for broadcast AR/virtual sets. Largest ecosystem. |
| Bevy | Rust | Native | Yes — Rust library | Growing ecosystem, ECS-based, wgpu renderer. Same language as engine. |
| Godot | C++/GDScript | Native | Partial | Open-source, growing community. Headless rendering possible. |
| Custom wgpu | Rust | Native | Yes | Purpose-built, minimal. No ecosystem. |

**2D motion graphics:**

Rive is included in **Phase 1a** (day one) — not an evaluation candidate. It ships as a built-in plugin alongside CEF and FFmpeg. See `docs/tooling.md` for Rive integration details.

**Evaluation criteria:**
- Linux support (in-process preferred, separate process acceptable)
- Frame transport latency
- Headless rendering capability
- Ecosystem and tooling maturity for designers (not just developers)
- Cross-platform dev workflow (macOS/Windows for design, Linux/Windows for production)
- Licensing implications
- Community health and long-term viability

**Any third-party engine that implements the Pivox Plugin Protocol works as a source — whether Pivox builds the plugin or the engine vendor does.** The plugin SDK is the integration surface, not bespoke per-engine code.

## Enabled Use Cases

These use cases are not separate features — they emerge naturally from the engine's architecture (CEF as a full browser, remote input interaction, audio pipeline). No additional engine work is required beyond what is already planned.

### Live Video Calls in Broadcast

Templates can embed live video calls (Teams, Zoom, Meet, or custom WebRTC) directly in the broadcast output. CEF is a full Chromium browser — WebRTC calls just work.

**Workflow:**

1. Operator loads a "video call" template on a channel (not on-air)
2. Via remote input interaction (mouse/keyboard through preview), operator navigates to the call URL, clicks "Join", grants permissions
3. Remote guest's video and audio render inside CEF alongside graphics (name strap, show branding, layout)
4. Operator takes the channel on-air — the call and graphics composite as a single fill+key output
5. Audio from the call routes through the audio pipeline to SDI/NDI

**Why this works without additional engine features:**
- CEF supports `getUserMedia` and WebRTC natively
- Remote input interaction (already planned) handles the pre-air setup
- Audio pipeline (already planned) captures CEF audio and routes to outputs
- CEF permission auto-grant (already configured) handles camera/mic prompts
- The call is just an HTML page — it's a template, not a feature

**Integration depth options (template-level, not engine-level):**

| Approach | Effort | Control |
|---|---|---|
| Iframe a web call URL (Teams/Zoom/Meet link) | Minimal | Limited — depends on service's web UI |
| Custom WebRTC template with own signaling server | Medium | Full control over layout, video quality, caller management |
| Platform SDK integration (Teams Graph API, etc.) | High | Programmatic call management — vendor-specific |

All three approaches are template decisions, not engine decisions. The engine just renders whatever the template does.

**Licensing caveat:** Major video conferencing platforms (Teams, Zoom, WebEx, Google Meet) have Terms of Service that may restrict broadcast redistribution of their web UIs. The recommended approach for production is building a custom WebRTC solution (e.g., using LiveKit, an open-source WebRTC platform) rather than iframing third-party call services. This avoids licensing issues entirely and gives full control over layout, branding, and video quality.

## Licensing

See `docs/licensing.md` for full licensing details including FFmpeg LGPL compliance, codec patent licensing (customer responsibility), third-party library obligations, and build configuration.

**Key points for engine development:**
- FFmpeg: link dynamically (LGPL 2.1). Never enable `--enable-gpl` or `--enable-nonfree`.
- CEF: BSD 3-Clause. Attribution only.
- AJA NTV2 SDK: MIT. Attribution only.
- NDI SDK: Proprietary, free to use. Accept Vizrt license.
- Codec patents: customer responsibility — Pivox does not include codec royalties.
- Pivox engine code: proprietary, closed source.
