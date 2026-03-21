# Pivox Hardware & Facility Integration

## Overview

This document covers hardware, facility integration, and output capabilities for the Pivox playout engine. For engine internals (compositor, frame pipeline, CEF, FFmpeg, JavaScript SDK, IPC, redundancy), see `docs/engine.md`.

**Topics covered:**
- AJA NTV2 cards (SDI and ST 2110 output)
- Genlock and frame timing
- GPI (General Purpose Interface)
- Closed captioning
- NDI network video output
- HDR (future capability)
- MJPEG preview
- Certified hardware matrix and platform support tiers

## AJA NTV2 Cards

AJA NTV2 is the primary output path to broadcast infrastructure. Pivox targets three AJA card models:

| Card | SDI Ports | ST 2110 | Channels (fill+key) | Use Case |
|---|---|---|---|---|
| Corvid 88 | 8x SDI | No | 4 | Production -- SDI only, 4 channels on one card |
| Corvid 44 12G | 4x 12G-SDI | Yes | 2 | Production -- SDI + ST 2110, pair two for 4 channels |
| Kona 5 | 4x 12G-SDI | Yes | 2 | Development -- Thunderbolt on macOS, SDI + ST 2110 |

### SDI Fill + Key Output

Pivox outputs **two separate SDI signals per channel**:

- **Fill**: The rendered output (RGB) -- video, graphics, or composited video+graphics
- **Key**: A grayscale mask (the alpha channel) -- white = fully opaque, black = fully transparent

When a channel contains only graphics layers (no video), the vision mixer uses its downstream keyer (DSK) to composite the fill+key over live camera feeds. When a channel contains video layers with graphics composited on top, the output is a full-frame signal that can be used as a direct source on the mixer.

This dual-use model (DSK for graphics-only, full-frame for video+graphics) is standard in broadcast. The vision mixer operator selects the appropriate input mode.

### NTV2 SDK

The AJA NTV2 SDK is open-source: https://github.com/aja-video/ntv2 (Linux + macOS).

**Implementation:**
- C++ thin wrapper around NTV2 SDK (`CNTV2Card`, `AutoCirculate` API)
- Rust driver manages frame scheduling and crosspoint routing
- Output routing configured programmatically: "framebuffer 0 -> SDI out 1 (fill), SDI out 2 (key)"

### ST 2110 Note

ST 2110 support is handled by the same AJA NTV2 SDK. The engine's output adapter configures the card for either SDI or ST 2110 mode -- no separate code path is needed. Facilities using ST 2110 also require PTP (IEEE 1588) timing infrastructure instead of or in addition to traditional genlock.

## Genlock and Timing

CEF does not know about broadcast timing. The engine controls frame cadence:

1. Engine receives genlock reference signal via AJA card
2. On each genlock edge, engine ticks CEF's `DoMessageLoopWork()`
3. CEF renders and fires `OnPaint()` with the pixel buffer
4. Engine captures the buffer and routes to compositor -> frame pipeline -> outputs
5. AJA card's `AutoCirculate` schedules the frame for the next output field

This ensures every rendered frame aligns with house sync. No dropped frames, no judder.

### PsF for Interlaced Output

The engine always renders progressive frames. For interlaced output formats (1080i), the AJA card handles Progressive Segmented Frame (PsF) conversion -- splitting each progressive frame into two fields. No interlacing logic is needed in the engine.

### Frame Rates per Output Format

| Format | Frame Rate | Notes |
|---|---|---|
| 1080p59.94 | 59.94fps | US HD standard (progressive) |
| 1080p50 | 50fps | EU HD standard (progressive) |
| 1080i59.94 | 29.97fps (PsF) | US HD legacy -- AJA card handles field splitting |
| 1080i50 | 25fps (PsF) | EU HD legacy -- AJA card handles field splitting |
| 720p59.94 | 59.94fps | Used by some US networks (ABC, ESPN legacy) |
| 720p50 | 50fps | EU variant |
| 2160p59.94 | 59.94fps | UHD -- requires 12G-SDI or ST 2110 |
| 2160p50 | 50fps | UHD EU variant |

Different channels on the same engine can run at different formats (e.g., CH1 at 1080p59.94, CH2 at 1080i59.94) -- each channel process ticks CEF at its own frame rate independently.

### PTP for ST 2110

ST 2110 facilities use PTP (Precision Time Protocol, IEEE 1588) for synchronization instead of physical blackburst/tri-level reference signals. A PTP grandmaster clock synchronizes all devices on the network to sub-microsecond accuracy. AJA cards handle PTP synchronization at the hardware level -- the engine's frame scheduling works the same way regardless of whether timing comes from a physical genlock input or PTP.

### Genlock Reference

AJA cards accept external reference input (blackburst or tri-level sync). The card locks its output timing to this reference. The engine reads the card's frame clock to synchronize CEF rendering.

## SDI Output (Fill + Key)

### How Fill + Key Works

Pivox outputs two separate SDI signals per channel:

- **Fill**: The rendered output (RGB) -- video, graphics, or composited video+graphics
- **Key**: A grayscale mask (the alpha channel) -- white = fully opaque, black = fully transparent

The frame pipeline splits the final composited RGBA output:
1. Fill buffer: RGBA -> RGB
2. Key buffer: extract alpha channel

### DSK Model

When a channel contains only graphics layers (no video), the vision mixer uses its **downstream keyer (DSK)** to composite the fill+key over live camera feeds. The DSK uses the key signal to determine which parts of the fill are opaque (graphic content) and which are transparent (show the camera feed through).

### Dual-Use: DSK and Full-Frame

- **DSK for graphics-only**: Channel outputs just graphics layers (lower thirds, tickers, bugs). The vision mixer composites these over live cameras using the fill+key pair and its DSK.
- **Full-frame for video+graphics**: Channel outputs video layers with graphics composited on top. The output is a complete video signal that can be used as a direct source on the mixer.

The vision mixer operator selects the appropriate input mode. This dual-use model is standard in broadcast.

## ST 2110 (SMPTE IP Output)

SMPTE ST 2110 is the broadcast industry standard for professional video over IP. Major facilities (ESPN, BBC, Sky, Discovery) are migrating from SDI routers to all-IP plants based on ST 2110.

### How It Differs from SDI

SDI sends video, audio, and metadata as one signal on one cable. ST 2110 separates them into independently routable IP streams:

- **ST 2110-20**: Uncompressed video (raw pixels over RTP)
- **ST 2110-30**: Audio (PCM over RTP)
- **ST 2110-40**: Metadata / ancillary data
- **ST 2110-22**: Compressed video (JPEG XS -- lower bandwidth variant)

Each stream is independently routable on standard 25/100GbE network switches -- no proprietary SDI routers required.

### How It Differs from NDI

NDI is a convenience protocol for LAN production (compressed, auto-discovery, easy to use). ST 2110 is the professional infrastructure standard (uncompressed, PTP-timed, facility-scale). They serve different purposes and Pivox supports both.

### PTP Timing

ST 2110 facilities use PTP (Precision Time Protocol, IEEE 1588) for synchronization instead of physical blackburst/tri-level reference signals. A PTP grandmaster clock synchronizes all devices on the network to sub-microsecond accuracy. AJA cards handle PTP synchronization at the hardware level -- the engine's frame scheduling works the same way regardless of whether timing comes from a physical genlock input or PTP.

### Bandwidth

| Format | Uncompressed (ST 2110-20) | JPEG XS (ST 2110-22) |
|---|---|---|
| 1080p59.94 | ~3 Gbps per stream | ~100-300 Mbps per stream |
| 2160p59.94 | ~12 Gbps per stream | ~400 Mbps-1 Gbps per stream |

Four channels with fill+key = 8 streams. Uncompressed 1080p requires ~24 Gbps total -- needs 25GbE or 100GbE infrastructure. JPEG XS brings this down to ~1-2 Gbps, workable on 10GbE.

### AJA Handles It Natively

Since AJA cards handle ST 2110 natively, the engine's AJA output adapter covers both SDI and ST 2110. Configuration determines which output mode is active. No separate output adapter needed.

## NDI (Network Video Output)

NDI (Network Device Interface) sends video over standard IP networks.

### Purpose

- Development preview on macOS without AJA hardware
- Network-based monitoring (any machine on the subnet can view output)
- Integration with NDI-capable mixers and software (vMix, TriCaster, OBS)
- Redundancy monitoring (view standby engine output remotely)

### Characteristics

- **Discovery**: mDNS (automatic -- receivers see sources appear on the network)
- **Codec**: SpeedHQ (~100-150 Mbps per 1080p60 stream, visually lossless)
- **Latency**: ~1-3 frames
- **Bandwidth**: 4 channels x fill+key = 8 streams ~= 1.2 Gbps (requires 10GbE)
- **SDK**: Free to use (binary library from Vizrt), C/C++ headers, Linux/macOS/Windows

### Implementation

- Thin C++ wrapper around NDI SDK
- Each channel announces two NDI sources: "Pivox CH1 Fill", "Pivox CH1 Key"
- Frame data passed directly from the frame pipeline -- minimal copy

### Development Preview Use Case

For development on macOS, NDI is the only output (no AJA card needed). Developers run the engine locally and view output in NDI Monitor (free, same machine or any device on the network). This is the primary dev workflow -- AJA Thunderbolt devices are optional.

## Audio Pipeline

Both CEF and the video engine produce audio. The engine captures and routes audio through a pipeline parallel to the video path.

### Capture Sources

- **CEF**: `CefAudioHandler::OnAudioStreamPacket()` delivers raw PCM samples per graphics layer (WebRTC calls, HTML5 audio/video, sound effects)
- **FFmpeg**: decoded audio packets from video clips, synchronized to video frames

The engine mixes all audio sources (graphics layers + video layers) into a single stereo or multichannel output per channel.

### Routing

```
CEF audio streams (per graphics layer)
  |
  +------------------------------+
  |                              |
  v                              v
FFmpeg audio (per video layer)   |
  |                              |
  +----------+-------------------+
             v
  Audio mixer (Rust)
    |  - per-channel mix of all layer audio (graphics + video)
    |  - sample rate conversion if needed
    |  - level control per layer
    |
    +---> AJA SDI audio embedder (NTV2 SDK)
    |    -> embedded audio in SDI output, routed to facility audio mixer
    |
    +---> NDI audio (embedded in NDI stream)
         -> monitoring and integration
```

AJA's NTV2 SDK supports embedding up to 16 channels of audio in each SDI output. The engine writes PCM samples to the card's audio buffer alongside video frames.

### Audio Capabilities

| Feature | Description |
|---|---|
| Per-layer volume/mute | Operator controls volume and mute per layer at runtime via gRPC |
| Audio follow video (AFV) | During transitions, audio crossfades in sync with video -- cut video = cut audio, dissolve video = crossfade audio |
| Audio channel mapping | Route layer audio to specific SDI output channel pairs (e.g., CH1-2 = program, CH3-4 = clean feed) |
| Audio delay compensation | Configurable delay (typically 1-3 frames) to maintain lip sync -- video processing adds latency, audio must be delayed to match |
| Silence generation | When no layers produce audio, output valid silence (zero samples). AJA cards require continuous audio. |
| Sample rate conversion | All sources resampled to 48kHz (broadcast standard). CEF outputs 48kHz natively. FFmpeg clips may be 44.1kHz or other rates |

## Closed Captioning

Closed captioning is a regulatory requirement in most broadcast markets (FCC in the US, Ofcom in the UK, EU accessibility directives).

### How Captions Work in Broadcast

Captions are a **data sideband** embedded in the video signal, not burned-in text. They are carried in the SDI signal's **VANC (Vertical Ancillary Data)** space in CEA-608 (analog legacy) or CEA-708 (digital) format. For ST 2110, captions travel as a separate **ST 2110-40** metadata stream.

### Live Captioning (Stenographer / AI)

Live captions are handled by a **dedicated caption encoder** (EEG, Softel, Verbit) that sits **downstream** of Pivox in the SDI chain. Pivox does not handle live caption timing -- the caption encoder receives text from the stenographer/AI service, encodes it into CEA-608/708 format, synchronizes it to the audio, and inserts it into the SDI signal's VANC.

```
Pivox AJA output (SDI -- no live captions)
  |
  v
Caption encoder (EEG, Softel, etc.)  <- text from stenographer/AI
  |                                    <- encodes CEA-608/708
  |                                    <- syncs to audio
  |                                    <- inserts into VANC
  v
SDI with captions -> transmission chain
```

Pivox does not need to handle live captioning -- it's a separate specialized system in the signal chain.

### Pre-Produced Clip Captions (Pass-Through)

Video clips (MXF/MOV) may contain embedded caption data tracks. When Pivox plays these clips, FFmpeg extracts the caption data and the engine passes it through to the AJA card's VANC output, frame-synchronized with the video. This is automatic -- the caption data is already timed to the clip's video frames.

```
MXF clip with embedded CEA-708
  |
  v
FFmpeg demuxes caption data stream (alongside video + audio)
  |
  v
Engine writes caption data to AJA VANC per frame (frame-synced)
  |
  v
SDI output includes captions
```

AJA's NTV2 SDK supports VANC insertion via `CNTV2Card::SetAncInsertMode()` and related APIs.

### Caption Detection and Alerting

When a video clip is loaded, FFmpeg immediately reports whether a caption track exists. The engine exposes this in the `SlotState` status stream (`has_captions`, `caption_format`). The Go control plane surfaces this in the operator UI:

- **CC detected:** Green indicator with format (e.g., "CEA-708")
- **No CC detected:** Warning indicator -- operator sees the alert before and during playout

Whether missing CC blocks playout is a **configurable policy** in the Go control plane -- rundown items can be marked as "CC required" and playout blocked if the clip lacks captions. This is an editorial/compliance decision, not an engine decision. The engine just reports `has_captions: true/false`.

## GPI

GPI (General Purpose Interface) provides physical button triggers and tally lights via the AJA card's built-in GPIO pins. Heavily used in broadcast facilities for critical operations. Supported day one.

Pivox targets AJA cards exclusively for GPI -- no third-party USB or IP GPI devices. This keeps the hardware stack unified and reduces integration complexity.

### GPI Inputs (Physical Buttons -> Engine Commands)

```
Operator panel / GPI button box
  |
  |  Button press -> contact closure -> AJA card GPI input pin
  |
  v
Rust supervisor detects GPI edge via NTV2 SDK
  |
  v
Maps to configured command:
  GPI 1 rising -> PlayCommand on CH1 Layer 1
  GPI 2 rising -> StopCommand on CH1 Layer 1
  GPI 3 rising -> NextCommand on CH1 Layer 1
  GPI 4 rising -> PlayCommand on CH2 Layer 1
  ...
```

### GPI Outputs (Engine State -> Tally Lights)

```
Channel 1 transitions to on-air mode
  |
  v
Rust supervisor sets AJA card GPI output pin high
  |
  v
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
  |
  v
Frame pipeline colorspace conversion (GPU shader):
  sRGB -> Rec.709 (minimal -- sRGB and Rec.709 share the same primaries,
                   only the transfer function differs slightly)
  |
  v
AJA card outputs Rec.709 SDI (8-bit or 10-bit 4:2:2)
```

This is straightforward -- sRGB and Rec.709 are nearly identical color spaces.

### HDR Pipeline (Future)

HDR requires a fundamentally different colorspace conversion:

```
CEF renders sRGB (8-bit, gamma 2.2, Rec.709 gamut)
  |
  v
Frame pipeline HDR conversion (GPU shader):
  |
  |  Step 1: Linearize
  |  Remove sRGB gamma -> linear light values
  |
  |  Step 2: Gamut mapping
  |  Rec.709 primaries -> Rec.2020 primaries (wider color space)
  |  This is a 3x3 matrix transform on linear RGB values
  |
  |  Step 3: Tone mapping (SDR graphics -> HDR range)
  |  Map the 0-100 nit SDR range into the HDR range
  |  - For HLG: map into 0-1000 nit range using HLG OETF
  |  - For PQ: map into 0-1000+ nit range using PQ EOTF
  |  This determines how bright/vivid the graphics appear
  |  against HDR video content
  |
  |  Step 4: Apply HDR transfer function
  |  - HLG (Hybrid Log-Gamma): ARIB STD-B67 OETF
  |  - PQ (Perceptual Quantizer): SMPTE ST 2084 EOTF
  |
  |  Step 5: Quantize to 10-bit
  |  8-bit sRGB -> 10-bit HDR (required for both HLG and PQ)
  |
  v
AJA card outputs Rec.2020 HDR SDI (10-bit 4:2:2)
```

### The Compositing Problem

When mixing SDR graphics (CEF) with HDR video clips (FFmpeg), the compositor must handle mismatched color spaces:

```
HDR video clip (Rec.2020, PQ, 10-bit)     CEF graphic (sRGB, 8-bit)
  |                                          |
  v                                          v
  +----------------------------------------------+
  |  Compositor (must operate in a single        |
  |  color space -- choose one, convert the other)|
  |                                              |
  |  Option A: Composite in HDR space            |
  |  - Convert CEF sRGB -> Rec.2020/PQ            |
  |  - Alpha-blend in HDR space                  |
  |  - Output HDR                                |
  |  Correct -- HDR video is untouched           |
  |                                              |
  |  Option B: Composite in SDR space            |
  |  - Tone-map HDR video -> SDR                  |
  |  - Alpha-blend in SDR space                  |
  |  - Output SDR                                |
  |  Loses HDR quality -- defeats the purpose    |
  +----------------------------------------------+
```

**Option A is correct.** The graphics are converted to HDR space, the video stays in HDR, and compositing happens in the HDR color space. The tone-mapping of SDR graphics into HDR must be carefully tuned so graphics look natural -- not washed out (too dim) or eye-searing (too bright).

### HDR Standards in Broadcast

| Standard | Transfer Function | Use Case | Region |
|---|---|---|---|
| HLG (Hybrid Log-Gamma) | ARIB STD-B67 | Live broadcast -- backward-compatible with SDR displays | BBC, NHK, common in Europe/Asia |
| PQ (Perceptual Quantizer) | SMPTE ST 2084 | Mastered content, streaming -- not backward-compatible | Netflix, Dolby Vision, Disney+ |

**For broadcast, HLG is the more likely target** -- it's designed for live production and is backward-compatible (an SDR display can show HLG content, just without the HDR benefit). PQ is primarily used for pre-mastered content.

### What the AJA Card Does and Doesn't Do

**AJA cards do NOT perform HDR conversion.** They output whatever pixel data the engine feeds them. If you feed Rec.709 data and the downstream chain expects Rec.2020/HLG, the graphics will look wrong (washed out, wrong colors, incorrect brightness).

**AJA cards DO support:**
- 10-bit and 12-bit output (required for HDR)
- Rec.2020 color space metadata signaling in SDI
- ST 2110 with HDR metadata (ST 2110-40)

### Architectural Impact

The frame pipeline's colorspace conversion is a pluggable stage. For SDR, it does sRGB -> Rec.709 (CPU SIMD). For HDR, it does sRGB -> Rec.2020 + PQ/HLG -- this is more compute-intensive and may warrant GPU acceleration via `wgpu` (Rust WebGPU implementation) when the time comes. Switching between SDR and HDR is a conversion function swap + buffer format change (8-bit -> 10-bit), not an architectural change.

**Day-one design decisions that enable future HDR:**
1. Frame pipeline uses pluggable colorspace conversion -- swap the conversion function without rearchitecting
2. Compositor designed to operate on linear-light internally -- enables correct blending in any color space
3. Buffer management supports 10-bit formats -- size buffers for 10-bit from the start, even if day-one output is 8-bit
4. FFmpeg decodes HDR metadata -- `av_frame_get_side_data()` provides mastering display info, content light level. Store and pass through even if not used yet.

## MJPEG Preview

Low-bandwidth preview for operator UI.

- Rust encodes every Nth frame as JPEG at reduced resolution (720p)
- Served as HTTP multipart stream (`Content-Type: multipart/x-mixed-replace`)
- Consumed by `<img>` tag in browser or Electron
- Go API server proxies and authenticates the stream
- Target: 15fps, 100-200ms latency -- sufficient for operator monitoring

For the MJPEG preview path, every 2nd-4th frame is JPEG-encoded at 720p and served over HTTP. Preview latency of 100-200ms is acceptable.

## Platform Support Tiers

| Tier | Platforms | AJA Hardware | Purpose |
|---|---|---|---|
| **Dev (Supported)** | macOS (Apple Silicon + Intel), Windows 10/11 | Certified Thunderbolt devices (optional -- NDI is the primary dev preview) | Template development, design, iteration |
| **Staging/Prod (Certified)** | Headless Linux (distro TBD), Windows Server 2022 | Certified PCIe cards only | On-air broadcast, staging, QA |

Linux is **not** a supported dev platform. Dev is macOS and Windows only.

### macOS (Dev)

- CEF OSR builds and runs natively
- Rust frame pipeline: CPU SIMD for colorspace conversion and compositing
- NDI output for preview (primary dev output -- no hardware needed)
- AJA output via certified Thunderbolt device (optional -- for SDI validation)
- MJPEG preview for Electron operator UI

### Windows (Dev)

- CEF OSR builds and runs natively
- Rust frame pipeline: CPU SIMD for colorspace conversion and compositing
- NDI output for preview (primary dev output)
- AJA output via certified Thunderbolt device (optional)
- MJPEG preview for Electron operator UI

### Linux (Staging/Production -- Headless)

- CEF OSR headless with `--use-gl=egl`
- Colorspace conversion and compositing: CPU SIMD (GPU only if profiling shows need)
- AJA output via certified PCIe card
- NDI output for network monitoring
- MJPEG preview for remote operator UI
- GPI via AJA card
- Compliance recording via NVENC
- Go control plane services may run in containers
- Engine processes run on bare metal, managed by Rust supervisor

### Windows Server (Staging/Production -- Headless)

- Same capabilities as Linux production
- Required for future Unreal Engine integration (Phase 6)
- Same certified PCIe AJA cards as Linux

## Certified Hardware Matrix

### AJA Devices by Tier

| Tier | AJA Devices | Notes |
|---|---|---|
| Dev (Thunderbolt) | Kona 5, Io 4K Plus | Optional -- most developers use NDI only |
| Prod/Staging (PCIe) | Corvid 88, Corvid 44 12G | Same cards certified on both Linux and Windows Server |

### GPU, CPU, RAM, Storage

| Component | Certified Options |
|---|---|
| GPU | NVIDIA RTX A4000, A5000, L40, L40S (specific driver versions TBD) |
| CPU | Minimum spec TBD after profiling |
| RAM | Minimum spec TBD after profiling |
| Storage | NVMe SSD required (minimum spec TBD -- needed for compliance recording) |

This matrix is published and maintained. Customers deploy from the list. Anything off-list is unsupported.
