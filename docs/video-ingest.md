# Video Ingest and Replay — Future Capability

## Overview

Pivox's day-one architecture is a **playout engine** — it plays pre-existing files and renders graphics. This document describes the phased expansion path toward **live video ingest, recording, and replay** workflows that would replace dedicated replay servers (EVS, Grass Valley K-Frame) in broadcast facilities.

This is not a day-one capability. Each phase is independently valuable and can be shipped incrementally.

## What EVS Does Today

EVS replay servers are the industry standard for live sports production. Their core workflows:

| Workflow | Description |
|---|---|
| **Multi-camera live ingest** | Record 6-12 live SDI camera feeds simultaneously in real time |
| **Live slow-motion replay** | Operator marks a moment during ingest, plays it back seconds later at variable speed (including super slow-mo) |
| **Growing-file playback** | Play back content that is still being recorded — the read head chases the write head |
| **High-frame-rate cameras** | Ingest 180fps, 360fps super slow-mo cameras; play back at broadcast rate (59.94fps) for dramatic slow-motion |
| **Multi-operator access** | Multiple LSM (Live Slow Motion) operators simultaneously build different replays from the same recorded content pool |
| **Highlight compilation** | Build highlight packages in real time during a live event |
| **Near-instant access** | Sub-second latency from marking a moment to playing it on air |

## What Pivox Replaces Today (Without Ingest)

Pivox's FFmpeg plugin already handles:

- Pre-produced package (VT) playout
- Pre-recorded clip playback with frame-accurate control
- Jog/shuttle, variable speed (0.1x–4x, reverse)
- Frame-accurate in/out points, looping
- Stills (PNG, JPEG, TGA, TIFF)

This covers **news studios, corporate broadcast, houses of worship, and any facility** where clips are pre-recorded and loaded from storage. These workflows do not need live ingest — Pivox handles them today alongside graphics in one box, replacing the need for separate clip servers.

## Architecture for Ingest

### AJA Cards Are Bidirectional

The same AJA Corvid/Kona hardware used for SDI output supports SDI input. Port direction is a configuration choice — some ports can capture while others output. No additional hardware is required for ingest beyond what's already deployed for playout.

### Ingest Pipeline

```
Live SDI feed → AJA card (input port) → NTV2 SDK capture → Raw frames
  → Encode (NVENC or libavcodec) → Write to storage
```

The ingest pipeline is the reverse of the output pipeline:

| Stage | Technology | Notes |
|---|---|---|
| SDI capture | AJA NTV2 SDK | Same C++ FFI shim used for output, configured for input |
| Frame capture | Rust | Raw RGBA/YUV frames from AJA, same buffer management as output |
| Encoding | NVENC (preferred) or FFmpeg libavcodec | NVENC for lowest latency; FFmpeg for codec flexibility |
| Storage write | Rust async I/O | Write to NVMe SSD array |
| Metadata | Rust | Timecode, source ID, mark points embedded in recording |

### Record/Play Storage Engine

This is the core technical challenge — a storage layer that supports **concurrent write (ingest) and read (playout)** with frame-accurate random access.

**Requirements:**
- Write frames while simultaneously reading them (growing-file playback)
- Frame-accurate random access into content still being recorded
- Sustained bandwidth for multiple concurrent HD/4K streams
- Sub-second latency from capture to playback availability

**Design approach — chunked ring buffer:**

```
Recording session
├── Chunk 0000 (2 seconds / 120 frames) — COMPLETE, readable
├── Chunk 0001 (2 seconds / 120 frames) — COMPLETE, readable
├── Chunk 0002 (2 seconds / 120 frames) — COMPLETE, readable
├── Chunk 0003 (2 seconds / 120 frames) — WRITING (partial, readable up to last complete frame)
└── (new chunks appended as recording continues)
```

Each chunk is independently seekable. Multiple readers can open different chunks simultaneously. The write head and read heads never contend on the same chunk boundary for more than one frame duration. Per-chunk index stores frame offsets for instant seek.

**Storage bandwidth requirements:**

| Resolution | Per-stream bandwidth (compressed) | Per-stream bandwidth (uncompressed) |
|---|---|---|
| 1080p59.94 | ~50-100 MB/s (H.264/HEVC) | ~373 MB/s (YUV 4:2:2 10-bit) |
| 2160p59.94 | ~150-300 MB/s (HEVC) | ~1.49 GB/s |
| 1080p180 (super slo-mo) | ~150-300 MB/s (HEVC) | ~1.12 GB/s |

NVMe SSDs sustain 3-7 GB/s sequential write. A 4-drive NVMe array handles multiple concurrent HD streams comfortably. For uncompressed 4K or high-frame-rate workflows, a larger array or compressed ingest is required.

### Hardware Configurations

**Dedicated ingest machine (sports truck):**

```
┌─────────────────────────────────┐
│ Ingest Engine (Machine 1)       │
│ Corvid 88 — 8 SDI inputs       │
│ 6 cameras + 2 super slo-mo     │
│ NVMe array for recording        │
│ NVENC for compressed ingest      │
└──────────────┬──────────────────┘
               │ storage network (NFS, iSCSI, or shared NVMe-oF)
┌──────────────┴──────────────────┐
│ Playout Engine (Machine 2)      │
│ Corvid 88 — 8 SDI outputs      │
│ 4 channels × fill+key          │
│ Graphics + replay playout       │
└─────────────────────────────────┘
```

**Single-box hybrid (news studio):**

```
┌──────────────────────────────��──┐
│ Single Engine (Machine 1)       │
│ Corvid 88 — 8 SDI ports        │
│ Ports 1-2: SDI input (ingest)  │
│ Ports 3-8: 3 channels output   │
│ Local NVMe for recording        │
│ Ingest + playout on one box    │
└─────────────────────────────────┘
```

Same engine binary in both configurations. Port direction is config. The control plane orchestrates across machines — ingest engine writes to shared storage, playout engine reads from it. For single-box, it's all local NVMe.

## High-Frame-Rate Camera Support

Super slow-motion cameras (Sony HDC-4800, Grass Valley LDX 150) output 180fps, 360fps, or higher over multiple SDI links. The AJA NTV2 SDK supports multi-link capture.

**Ingest flow:**
1. Camera outputs 180fps over 3× 3G-SDI links (or 1× 12G-SDI)
2. AJA captures all links and reassembles full-rate frames
3. NVENC encodes at the native frame rate (180fps HEVC)
4. Storage engine writes at full rate

**Playout flow:**
1. FFmpeg plugin opens the 180fps recording
2. Operator sets playback speed (e.g., 0.33x = real-time slow-mo at 59.94fps output)
3. Jog/shuttle via USB HID or serial controller for frame-accurate scrubbing
4. Engine's clock source ticks at the output frame rate (59.94fps); FFmpeg selects the correct source frame via timestamp mapping

The FFmpeg plugin's existing variable-speed architecture handles this — high-frame-rate content is just a video file with more frames per second. The difference is on the ingest side, not the playout side.

## Jog/Shuttle Hardware Integration

Replay operators use physical jog/shuttle controllers for frame-accurate media scrubbing. See `docs/hardware.md` for full details on supported hardware (USB HID controllers, serial/IP broadcast panels) and the input adapter architecture.

The control plane maps hardware events to engine transport commands:

| Hardware Event | Engine Command |
|---|---|
| Jog tick clockwise | Advance +1 frame |
| Jog tick counter-clockwise | Advance -1 frame |
| Shuttle position | Set playback speed (proportional to ring position) |
| Mark In button | Set in-point at current timecode |
| Mark Out button | Set out-point at current timecode |
| Play button | Play at 1x from current position |
| Record button | Start recording on ingest channel |

## Phased Roadmap

### Phase 1 — File Playout + Graphics (Day One)

What ships first. Covers the majority of non-live-replay workflows.

- Pre-recorded clip playback with frame-accurate control
- Graphics rendering (CEF, Rive, stills)
- Compositing, transitions, data plane
- SDI/NDI/ST 2110 output

**Replaces:** Vizrt/CasparCG (graphics), basic clip servers, separate still stores

### Phase 2 — SDI Ingest + Record-to-Storage

Basic ingest capability. Independently valuable for news and live production.

- AJA SDI input capture (bidirectional port config)
- Real-time encoding to storage (NVENC)
- Timecode-stamped recordings with metadata
- Clip playout from locally recorded content
- Basic mark in/out during or after recording

**Replaces:** Basic VTR workflows, ingest-only servers
**Target:** News studios, corporate live events, houses of worship

### Phase 3 — Growing-File Playback

The capability that enables near-instant replay.

- Record/play storage engine with concurrent read/write
- Play back content still being recorded (read head chases write head)
- Sub-second latency from capture to playout availability
- Multiple read heads on the same recording session

**Replaces:** EVS for basic replay workflows (non-super-slo-mo)
**Target:** Live sports (standard cameras), live events with instant replay

### Phase 4 — High-Frame-Rate + Multi-Operator

Full EVS replacement for premium sports production.

- High-frame-rate camera ingest (180fps, 360fps) via multi-link SDI
- Super slow-motion playout at broadcast frame rate
- Multi-operator concurrent access to shared recording pool
- Highlight compilation and playlist building
- Dedicated jog/shuttle controller integration (DNF, Skaarhoj panels)

**Replaces:** EVS XT-VIA, Grass Valley K-Frame for replay workflows
**Target:** Premium sports trucks, major live event production

## What's Not Changing

The engine architecture supports all phases without redesign:

- **Plugin SDK** — ingest is a new plugin type (or extension of FFmpeg plugin), same interface
- **Process model** — ingest channels are additional processes managed by the supervisor
- **Clock source** — ingest captures at the incoming signal's rate, not the output genlock (separate timing domain)
- **Control plane API** — same gRPC interface, new command types for record/mark/playback
- **Output pipeline** — replay playout uses the same compositor and output adapters
- **Hardware** — AJA cards already support bidirectional SDI; same certified hardware

The hard engineering is the **record/play storage engine** (concurrent read/write with frame-accurate seeking) and the **operator workflow** for replay (LSM-style interface). These are the investments that unlock each phase.
