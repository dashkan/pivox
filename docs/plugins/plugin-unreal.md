# Pivox Unreal Plugin — 3D Engine Integration (Future)

## Overview

Unreal Engine integration is a **future capability** (Phase 6) for use cases that exceed WebGPU and Rive's capabilities — virtual sets, photorealistic AR, complex 3D environments. This document captures the evaluation criteria, architecture options, and estimated effort.

Unreal is not a day-one plugin. The Plugin SDK and architecture support it without changes — it's additive when the time comes.

**Related docs:**
- `docs/plugins/plugin-sdk.md` — Plugin SDK that Unreal would build on
- `docs/engine.md` — Phase 6 development plan
- `docs/tooling.md` — 3D engine evaluation criteria

## Why Unreal

| Capability | WebGPU (CEF) | Rive | Unreal |
|---|---|---|---|
| 2D graphics | Excellent | Excellent | Overkill |
| Motion graphics | Good | Excellent | Good |
| 3D environments | Growing (Three.js) | No | Industry standard |
| Virtual sets | Limited | No | Industry standard |
| AR (augmented reality) | Limited | No | Industry standard |
| Photorealistic rendering | Not yet | No | Yes (Lumen, Nanite) |
| Ray tracing | WebGPU spec in progress | No | Yes |
| Ecosystem / tooling | Web tools | rive.app | Unreal Editor (massive ecosystem) |

Unreal is justified only when customers need virtual sets, photorealistic AR, or complex 3D environments. For everything else, CEF + Rive covers it.

## Windows Dependency

Unreal Editor is Windows-only (and macOS with limitations). The runtime can be built for Linux, but the content creation pipeline is Windows-centric.

**Implications:**
- Designers create content on Windows in Unreal Editor
- Production can run on either Windows Server or Linux (runtime supports both)
- Pivox engine compiles for Windows (LOE: ~3-4 weeks from the Linux baseline)

## Architecture — Separate Process

Unreal is a heavy process with its own GPU context, window management, and render loop. It cannot run in-process like Rive. It connects as an external plugin via the Plugin SDK.

```
┌──────────────────────────────────────────────────────────┐
│  Channel Process                                          │
│                                                           │
│  Plugin Receiver                                          │
│    ├── CEF plugin (in via Plugin Protocol)                │
│    ├── FFmpeg plugin (in via Plugin Protocol)             │
│    ├── Rive plugin (in via Plugin Protocol)               │
│    └── Unreal plugin (in via Plugin Protocol)  ← same    │
│                                                           │
│  Compositor (treats all sources identically)              │
└──────────────────────┬────────────────────────────────────┘
                       │ shared memory (RGBA frames)
┌──────────────────────┴────────────────────────────────────┐
│  Unreal Engine Process (separate, heavy)                   │
│                                                            │
│  ┌──────────────────────┐  ┌───────────────────────────┐  │
│  │ Unreal Project        │  │ Pivox Unreal Plugin       │  │
│  │                       │  │ (C++ UE plugin)           │  │
│  │ - 3D scene            │  │                           │  │
│  │ - Virtual set         │  │ - Receives commands       │  │
│  │ - AR elements         │  │   via Plugin Protocol     │  │
│  │ - Sequencer           │  │ - Controls scene /        │  │
│  │                       │  │   sequencer / blueprints  │  │
│  │ Renders at house      │  │ - Captures rendered       │  │
│  │ frame rate            │  │   frame to shared memory  │  │
│  └───────────────────────┘  └───────────────────────────┘  │
└────────────────────────────────────────────────────────────┘
```

## Plugin Capabilities (Expected)

```
PluginCapabilities:
  name: "Unreal Engine 5"
  version: <UE version>
  type: HYBRID

  supports_load: true       # load level/sequence
  supports_play: true       # start sequencer / trigger blueprint
  supports_stop: true       # stop / out-animation
  supports_update: true     # set blueprint variables (data binding)
  supports_next: true       # advance sequence
  supports_seek: true       # seek in sequencer timeline
  supports_variable_speed: true  # sequencer speed control
  supports_loop: true

  outputs_video: true       # RGBA with alpha
  outputs_audio: true       # UE audio output
  outputs_alpha: true       # alpha channel for compositing
  outputs_captions: false

  supported_formats: [".uproject", ".umap", ".ulevel"]
```

## Frame Transport

| Method | Latency | Quality | Complexity | Platform |
|---|---|---|---|---|
| Shared memory (CPU readback) | ~1 frame | Lossless | Low | Cross-platform |
| DMA-BUF (GPU texture sharing) | ~0 (zero copy) | Lossless | Medium | Linux only |
| NDI (network) | ~1-3 frames | Visually lossless | Low (UE has NDI plugins) | Cross-platform |

**Recommended:** Shared memory for day one (same as other plugins). DMA-BUF as optimization on Linux if latency matters.

## Command Mapping

| Pivox Command | Unreal Action |
|---|---|
| `LoadCommand(project, data)` | Load level/sequence, set initial blueprint variables |
| `PlayCommand` | Start sequencer, trigger blueprint event |
| `UpdateCommand(data)` | Set blueprint variables from JSON data |
| `StopCommand` | Trigger out-animation via sequencer/blueprint |
| `SeekCommand(timecode)` | Seek sequencer to position |
| `SpeedCommand(speed)` | Set sequencer playback speed |

## The Pivox Unreal Plugin (C++ UE Plugin)

A C++ Unreal Engine plugin that:
- Includes the Pivox Plugin SDK
- Connects to the channel process via gRPC + shared memory
- Receives commands and dispatches to UE Sequencer / Blueprints
- Captures each rendered frame (GPU readback to shared memory)
- Reports status back to Pivox

## Genlock Synchronization

Unreal must render at the house frame rate. The plugin:
1. Receives target frame rate from PluginConfig
2. Locks UE's render loop to that rate (custom game mode with fixed timestep)
3. Captures each frame after render completes
4. Writes to shared memory and signals "frame ready"

## LOE Estimate

| Work Item | Effort |
|---|---|
| Pivox UE Plugin (core) | 4-6 weeks |
| Shared memory frame transport | 1 week |
| Channel supervisor changes | 1 week |
| Unreal frame receiver in channel process | 1 week |
| Protobuf schema additions | 2-3 days |
| Integration testing | 2-3 weeks |
| DMA-BUF optimization (optional) | 2 weeks |
| **Total** | **~10-12 weeks** |

Working prototype in ~6 weeks.

## Evaluation Criteria (Before Building)

Before committing to Unreal integration, evaluate:

- [ ] Customer demand — do paying customers actually need virtual sets / photorealistic AR?
- [ ] Linux runtime — does UE5 runtime work reliably on the Linux production target?
- [ ] Windows production — is running the engine on Windows Server acceptable for Unreal channels?
- [ ] GPU sharing — can Unreal and CEF share the same GPU effectively?
- [ ] Licensing — Unreal Engine royalty structure (5% over $1M revenue) — impact on Pivox pricing?
- [ ] Content pipeline — who creates UE content? Customers? Pivox services? Third-party designers?
- [ ] Alternatives — has WebGPU + AI closed the gap enough to make Unreal unnecessary?

## Other 3D Engine Candidates

If Unreal's Windows dependency or licensing is unacceptable, evaluate:

| Engine | Language | Linux | In-Process | Notes |
|---|---|---|---|---|
| Bevy | Rust | Native | Yes | Same language as engine. Growing ecosystem. wgpu renderer. |
| Godot | C++/GDScript | Native | Partial | Open-source, MIT. Headless rendering possible. |
| Custom wgpu | Rust | Native | Yes | Purpose-built, minimal. No ecosystem. |

All would connect via the same Plugin SDK. The architecture is engine-agnostic.
