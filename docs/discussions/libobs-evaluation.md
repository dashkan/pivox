# Discussion: libobs as Playout Engine Foundation

**Date:** 2026-04-03
**Decision:** Do not use libobs. Build the engine in Rust as designed.
**Status:** Decided

## Context

We evaluated whether OBS Studio's underlying library (libobs) could serve as the foundation for Pivox's playout engine, potentially reducing development effort for compositing, CEF integration, FFmpeg media playback, and the plugin system.

libobs is the core C library that powers OBS Studio. It is decoupled from the Qt frontend and can be used as a standalone shared library. It provides a complete rendering pipeline: sources, scenes, filters, transitions, encoders, outputs, and a multi-threaded compositor with GPU acceleration.

## What libobs Provides That Overlaps with Pivox

- **CEF/browser source** (`obs-browser` plugin) — offscreen Chromium rendering with interaction forwarding
- **FFmpeg media source** (`obs-ffmpeg` plugin) — video/audio decode via libavformat/libavcodec with hardware acceleration
- **Image sources** — PNG, JPEG, animated GIF
- **Compositing** — scene graph with z-ordering, alpha blending, scaling
- **Transitions** — cut, fade, swipe, slide, stinger, custom
- **Audio mixing** — hierarchical mix tree, resampling, per-source volume, filters
- **Filter pipeline** — video/audio filter chains per source (25+ built-in filters)
- **Encoder abstraction** — x264, NVENC, QSV, VideoToolbox, VAAPI
- **Output abstraction** — RTMP, recording, MPEG-TS, WebRTC (WHIP)
- **Plugin system** — `obs_source_info` struct with well-designed callback contract

## Why We Rejected It

### 1. Genlock Timing (Dealbreaker)

Pivox's entire architecture is built around `AJA genlock edge → tick plugins → compositor → AutoCirculate → SDI`. libobs's rendering loop runs on a software timer (`obs_graphics_thread` sleeps until the next frame). There is no hook point for external timing reference. Replacing the core render loop would mean forking libobs, at which point we're no longer using it — we're maintaining a fork of a large C codebase.

### 2. No Fill + Key Output

libobs composites to a single RGBA output. Pivox requires separate fill (RGB) and key (alpha) SDI signals per channel. This is fundamental to the output pipeline and would require deep changes to libobs's rendering and output architecture.

### 3. No AJA/SDI Output

libobs has zero support for AJA cards or professional SDI output. A custom `obs_output_info` plugin could be written, but libobs's output system is designed for encoded streams (RTMP, file recording), not uncompressed frame delivery to hardware with AutoCirculate timing.

### 4. No ST 2110 Support

SMPTE ST 2110 IP output is not in libobs's output pipeline.

### 5. Process Model Mismatch

libobs is a single-process library with global state (`struct obs_core`). Pivox runs each channel as a separate OS process with supervisor-managed crash isolation. These models are incompatible.

### 6. Single Canvas Architecture

libobs is designed around a single output canvas. Multiple Pivox channels would require multiple libobs instances in separate processes — negating the benefit of a shared library.

### 7. Inadequate Media Control

libobs's FFmpeg source provides consumer-grade playback. Pivox requires frame-accurate jog/shuttle, variable speed (0.1x–4x), reverse playback, caption pass-through (CEA-608/708 VANC), and in/out points. These would all need to be built on top.

### 8. Data Plane Has No Analog

Pivox's shared memory lock-free double-buffer feeds, per-field versioning, two-layer throttling, and operator gating have no equivalent in libobs. The entire data plane would be built from scratch regardless.

### 9. JavaScript SDK Limitations

`obs-browser` provides basic JS interaction, but not the reactive model binding (`pivox.model`), shared memory feeds (`pivox.feeds`), genlock-synced frame callbacks (`pivox.timing`), or V8 FFI to Rust (`pivox.native`) that Pivox templates require. Heavy forking of obs-browser would be needed.

### 10. GPL Licensing

libobs is **GPLv2+**. Pivox ships on-prem hardware (Tier 3), which means distributing binaries. Any application linked against libobs must be GPL-compatible. Server-side-only use (Tiers 1-2) would be fine under GPLv2, but the on-prem deployment model creates a licensing conflict for a commercial product.

### 11. Rive Plugin Doesn't Get Easier

Whether we use libobs or our own engine, the Rive integration work is roughly the same: initialize Rive C++ runtime, render to offscreen surface, transfer texture to compositor. libobs's graphics abstraction (`gs_*`) doesn't expose enough low-level control for Rive's renderer (which requires OpenGL 4.6 / Metal / Vulkan / D3D11), so the same offscreen-render-and-transfer pattern is needed regardless.

## What We Took Away

libobs's implementations are valuable as **reference architectures**, not dependencies:

- **`obs-browser`** — how they handle CEF OSR, texture transfer, interaction forwarding
- **`obs-ffmpeg`** — decode pipeline structure and hardware acceleration paths
- **`obs-transitions`** — transition rendering pattern (`transition-cut.c` is 76 lines — clean design)
- **`obs_source_info` struct** — well-designed plugin contract that influenced many media frameworks
- **Backend design document** (`docs.obsproject.com/backend-design`) — one of the better architectural docs in open-source media software

## Summary

The overlap between libobs and Pivox is real but shallow. The parts libobs provides for free (CEF, FFmpeg, basic compositing) are the easier parts of the engine to build in Rust. The hard parts — genlock timing, AJA output, fill+key, data plane, frame-accurate media, process isolation — have zero support in libobs. Using it would mean fighting its architecture more than benefiting from it, while picking up GPLv2 licensing constraints.

## Resources

- [OBS Studio GitHub](https://github.com/obsproject/obs-studio)
- [OBS Backend Design](https://docs.obsproject.com/backend-design)
- [obs-headless (headless libobs usage)](https://github.com/a-rose/obs-headless)
- [obs-studio-node (Streamlabs' libobs bindings)](https://github.com/streamlabs/obs-studio-node)
