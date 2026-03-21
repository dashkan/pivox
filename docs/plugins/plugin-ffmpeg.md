# Pivox FFmpeg Plugin — Video, Audio & Still Image Engine

## Overview

- FFmpeg is Pivox's media playback engine for video clips, audio files, and still images
- Built-in plugin — ships with Pivox, built on the same Plugin SDK
- Runs **in-process** inside each channel process (loaded on demand)
- Uses FFmpeg libraries: libavformat, libavcodec, libavutil, libswscale
- Delivers decoded frames via direct buffer pointer to compositor (zero copy, same process)

## Plugin Capabilities

- type: VIDEO
- supports: load, play, stop, seek, variable_speed, loop
- does NOT support: update, next (not interactive/data-driven)
- outputs: video (RGBA/YUV), audio (PCM), captions (if present in source)
- formats: .mxf, .mov, .mp4, .ts, .wav, .mp3, .flac, .aac, .png, .jpg, .tga, .tiff
- codecs: h264, hevc, prores, dnxhr, mpeg2, xdcam, avc-intra, mjpeg, pcm

## Media Types

### Video Clips

- Full playback with frame-accurate seeking
- Variable speed (0.1x to 4x+, reverse)
- Jog (frame-by-frame) and shuttle
- In/out points, loop between points
- Playlist/sequence (gapless playback)

### Audio-Only Files

- WAV, MP3, FLAC, AAC
- PCM decoded, routed to audio mixer
- No video frames produced — compositor skips this layer visually
- Used with audio visualizer templates on a separate CEF layer (see [templates.md](../templates.md))

### Still Images

- PNG, JPEG, TGA, TIFF
- Loaded as single-frame video — decoded once, held on screen indefinitely
- Used for: standby cards, sponsor logos, holding slides, test patterns

## Decode Pipeline

```
file
  │
  ▼
libavformat — demux container
  │
  ├── Video stream
  │   ▼
  │   libavcodec — decode (NVDEC/VAAPI/VideoToolbox or CPU)
  │   │
  │   ▼
  │   libswscale — convert to RGBA
  │   │
  │   ▼
  │   RGBA buffer → shared memory
  │
  ├── Audio stream
  │   ▼
  │   libavcodec — decode to PCM
  │   │
  │   ▼
  │   PCM → shared memory audio region
  │
  └── Caption stream
      ▼
      Extracted, passed through to channel process for AJA VANC
```

## Hardware-Accelerated Decode

- **NVDEC (NVIDIA GPUs)** — Linux and Windows production
- **VAAPI** — Linux fallback
- **VideoToolbox** — macOS development
- FFmpeg selects automatically based on available hardware
- CPU fallback for all codecs if hardware decode unavailable

## Frame Timing for Variable Speed

- **Normal (1x)**: one decoded frame per genlock tick
- **Slow motion (0.5x)**: each frame held for 2 ticks, engine tracks fractional position
- **Fast forward (2x)**: skip frames or decode at double rate
- **Reverse**: decode GOP in reverse. Intra-frame codecs (ProRes, DNxHR) trivial. Long-GOP (H.264) requires full GOP decode + buffer + reverse output
- **Jog**: seek to next/previous frame, decode, hold

## Supported Formats (Day One)

| Container | Codecs |
|---|---|
| MXF (OP1a, OPAtom) | XDCAM HD, AVC-Intra, DNxHR, MPEG-2 |
| QuickTime (.mov) | ProRes (422, 4444), DNxHR, H.264 |
| MP4 | H.264, H.265/HEVC |
| MPEG-TS | MPEG-2, H.264 |
| Audio | WAV (PCM), MP3, FLAC, AAC |
| Stills | PNG, JPEG, TGA, TIFF |

## Closed Caption Pass-Through

- FFmpeg demuxes caption data streams (CEA-608/CEA-708) from MXF/MOV files
- Caption data forwarded to channel process → AJA VANC output
- Frame-synced (caption data timed to video frames in the source file)
- Detection: FFmpeg reports caption track presence at demux → engine reports `has_captions` in SlotState

## Audio Decode

- Decoded to PCM (48kHz, resampled if source differs)
- Synced to video frames
- Written to shared memory audio region
- Channel process audio mixer combines with other layers

## FFmpeg Build Configuration (LGPL)

- Link dynamically to shared libraries (.so/.dylib)
- LGPL 2.1 — Pivox source stays proprietary
- Do NOT enable: `--enable-gpl`, `--enable-nonfree`, `--enable-libx264`, `--enable-libx265`
- Use NVENC for encoding (compliance recording) — LGPL compatible

**Build flags:**

```bash
./configure \
  --enable-shared \
  --disable-static \
  --enable-nvdec \
  --enable-nvenc \
  --enable-vaapi \
  --enable-videotoolbox \
  --disable-libx264 \
  --disable-libx265 \
  --disable-libfdk-aac
```

See [licensing.md](../licensing.md) for full LGPL compliance details.

Stripped build: disable unused codecs/demuxers for smaller binary, faster startup.

## Process Model

- FFmpeg runs **in-process** inside the channel process as a loaded module
- Loaded on demand when a channel needs video/audio/still layers
- Frame delivery via direct buffer pointer — zero copy
- If FFmpeg crashes: the entire channel process goes down. Supervisor restarts, reloads all plugins, resumes clip from last known position. In practice, FFmpeg crashes are very rare — it's deterministic media decoding with no user code.
