# Pivox Licensing

This document consolidates all licensing information for the Pivox playout engine and its dependencies. For technical architecture details, see [engine.md](engine.md). For the plugin protocol specification, see the plugin protocol documentation.

---

## FFmpeg — LGPL 2.1

Pivox uses FFmpeg libraries (libavformat, libavcodec, libavutil, libswscale) for video decoding and encoding. FFmpeg is dual-licensed as LGPL 2.1 or GPL 2, depending on build configuration. **Pivox builds FFmpeg under LGPL 2.1 to keep the Pivox engine proprietary.**

**LGPL compliance requirements:**
1. Link dynamically to FFmpeg shared libraries (.so/.dylib) — not statically
2. Provide the FFmpeg source code used in the build (or a link to it)
3. Include LGPL license text in documentation / about screen
4. If FFmpeg source is modified, release those modifications (Pivox's own code remains proprietary)

**Why dynamic linking:** LGPL permits static linking, but requires distributing application object files (.o) so users can re-link against their own FFmpeg build. Dynamic linking avoids this obligation entirely and is the standard approach.

**Build configuration — features to avoid:**

| Feature | Flag | License Impact | Pivox Policy |
|---|---|---|---|
| x264 encoder | `--enable-libx264` | Escalates to **GPL** | Do not enable — use NVENC instead |
| x265 encoder | `--enable-libx265` | Escalates to **GPL** | Do not enable — use NVENC instead |
| fdk-aac encoder | `--enable-libfdk-aac` | Escalates to **nonfree** (non-redistributable) | Do not enable |
| `--enable-gpl` | (global) | Escalates entire build to **GPL** | Never set |
| `--enable-nonfree` | (global) | Makes binary **non-redistributable** | Never set |

**FFmpeg build flags for Pivox:**

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

This provides: all decoders (LGPL), hardware-accelerated encode via NVENC (LGPL-compatible), VAAPI decode on Linux, VideoToolbox decode on macOS. Pivox source remains fully proprietary.

## Codec Patent Licensing — Customer Responsibility

FFmpeg's software license (LGPL) is separate from codec **patent** licensing. Certain codecs are covered by patents managed by patent pools. Pivox's license fees do not include codec patent royalties.

| Codec | Patent Pool | Status |
|---|---|---|
| H.264 / AVC | MPEG LA | Active — per-unit royalty (capped) |
| H.265 / HEVC | MPEG LA + Access Advance | Active — complex, two separate pools |
| MPEG-2 | Expired (2018) | Free — patents expired worldwide |
| ProRes | Apple | Free to decode — no enforced licensing |
| DNxHR / DNxHD | Avid | Free — open specification |
| AV1 | Alliance for Open Media | Free — royalty-free by design |
| JPEG XS | intoPIX | Active — per-device licensing |

**Pivox license agreement language (to be reviewed by legal):**

> Customer is responsible for obtaining any required codec patent licenses from the relevant patent pools (e.g., MPEG LA for H.264/AVC, Access Advance for HEVC). Pivox software license fees do not include codec patent royalties. Most broadcast facilities already hold blanket codec licenses through their existing hardware and software agreements.

This is the standard approach used by Vizrt, Blackmagic, EVS, and other broadcast software vendors.

## CEF — BSD 3-Clause

Chromium Embedded Framework (CEF) is licensed under the BSD 3-Clause license. This is a permissive license that requires only attribution in documentation and the about screen. CEF is used as the HTML/JS graphics rendering engine in Pivox and imposes no obligations on the Pivox engine source code — it remains fully proprietary.

## NDI SDK — Proprietary (Free to Use)

The NDI SDK from Vizrt/NewTek is proprietary software that is free to use and redistribute. To use the NDI SDK, developers and distributors must accept the Vizrt/NewTek SDK license agreement. Key points:

- The SDK is free of charge — no royalties or per-unit fees
- Redistribution of the NDI runtime is permitted under the SDK license
- Attribution is required in documentation and the about screen
- The license must be accepted before integrating or distributing the SDK
- The SDK license terms are set by Vizrt/NewTek and may be updated; always use the current version from the official NDI SDK download

## Other Third-Party Libraries

| Library | License | Obligation |
|---|---|---|
| CEF (Chromium Embedded Framework) | BSD 3-Clause | Attribution in docs |
| AJA NTV2 SDK | MIT | Attribution in docs |
| NDI SDK | Proprietary (free to use) | Accept Vizrt/NewTek SDK license, attribution |
| Rust crates | Varies (MIT/Apache 2.0 typical) | Attribution, check per crate |

## Pivox Plugin Protocol SDK

The Pivox Plugin Protocol defines the interface for third-party engines (Unreal, Godot, Rive, etc.) to connect to Pivox as additional rendering sources. The licensing model for the Plugin Protocol SDK is **to be determined**.

Current direction:
- The protocol specification will be **published** so that third parties can build plugins
- The Pivox engine itself is **not open source** — only the plugin interface spec and SDK will be made available
- The SDK licensing terms (e.g., permissive open source, source-available, or proprietary with free use) have not been finalized
- This section will be updated once the licensing model is decided

## Summary

| Component | License | Pivox Source Impact | Customer Cost |
|---|---|---|---|
| Pivox engine code | Proprietary | Closed source | Included in license |
| FFmpeg libraries | LGPL 2.1 | None (link dynamically) | Free |
| Codec patents | Per-patent pool | None | Customer responsibility |
| CEF | BSD 3-Clause | None | Free |
| AJA SDK | MIT | None | Free |
| NDI SDK | Proprietary | None | Free |
| Plugin Protocol SDK | TBD | N/A | TBD |
