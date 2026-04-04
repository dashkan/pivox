# Pivox Glossary — Broadcast & System Terminology

Terms used across Pivox documentation. Organized alphabetically.

---

**AFV (Audio Follow Video)** — When transitioning between video/graphics elements, audio crossfades in sync with the video transition. Cut video = cut audio. Dissolve video = crossfade audio.

**AJA** — Manufacturer of professional video I/O hardware (PCIe cards and Thunderbolt devices). Pivox uses AJA cards for SDI and ST 2110 output. Key products: Corvid 88, Corvid 44 12G, Kona 5.

**AutoCirculate** — AJA NTV2 SDK API for scheduling video frames to output at precise genlock-synced timing. The engine writes frames to AutoCirculate buffers; the card outputs them at the correct time.

**Blackburst** — An analog reference signal (black video with sync pulses) used to synchronize all equipment in a broadcast facility. Being replaced by tri-level sync and PTP. See also: Genlock.

**CEA-608 / CEA-708** — Closed captioning standards. CEA-608 is the legacy analog standard. CEA-708 is the digital standard for HD/UHD. Carried in the SDI signal's VANC space.

**CEF (Chromium Embedded Framework)** — An open-source framework for embedding a Chromium browser in applications. Pivox uses CEF in Off-Screen Rendering (OSR) mode to render HTML/JS templates without a visible window.

**Channel** — One compositor producing one SDI fill+key output pair (or NDI stream). Each channel runs as a separate OS process. A channel contains a stack of independently addressable layers. The compositor merges all layers onto a transparent canvas. See also: Layer.

**Clock Source** — The abstraction that drives the engine's frame timing. Two implementations: `AjaGenlockClock` (production — blocks on AJA hardware interrupt) and `SoftwareClock` (development/staging/cloud — high-resolution software timer with rational frame duration arithmetic). The engine loop is identical regardless of clock source.

**Cloud Controller** — The SaaS management layer at api.pivox.app. Source of truth for user management, licensing, NRCS/rundowns, template registry, asset metadata, and agent management. Serves the web UI. Manages both Storage Agents and Playout Agents via bidi gRPC. See also: Playout Agent, Storage Agent.

**Changeover Switch** — Hardware device (Nevion, Evertz) that switches between primary and backup video sources. Used for Pivox redundancy — if Engine A fails, the switch cuts to Engine B's output.

**Closed Captioning (CC)** — Text overlay for hearing-impaired viewers. Carried as data in the SDI signal (VANC), not burned into the video. A regulatory requirement in most broadcast markets.

**Compositor** — The Rust component that merges all layers (video + graphics) into a single RGBA output per frame. Handles layer stacking, alpha blending, and transitions between foreground/background slots.

**Data Plane** — Pivox's live data infrastructure. Connects external data feeds to on-air templates with operator control (gating, approval, pause, override), throttling, schema versioning, and high-performance shared memory delivery.

**DOG (Digitally Originated Graphic)** — A persistent on-screen graphic, typically a channel logo or bug in the corner of the screen. Also called a "bug."

**DSK (Downstream Key)** — A compositing method where the vision mixer overlays graphics (from Pivox) on top of live video. Pivox outputs fill (the graphic) and key (the transparency mask) as separate SDI signals. The mixer's downstream keyer composites them.

**DVE (Digital Video Effect)** — A transition or effect that transforms video/graphics spatially — squeeze, zoom, spin, picture-in-picture.

**Embedded Engine** — The playout engine loaded in-process inside the native app for Designer mode and engine development. Full engine capability (GPU compositor, all plugins, hardware encode/decode) minus broadcast I/O (AJA, GPI). Uses software clock and direct framebuffer output. Connected via in-process gRPC — same API as standalone engine.

**EGL** — An API for connecting rendering APIs (OpenGL, Vulkan) to the native windowing system. CEF uses `--use-gl=egl` on Linux for GPU-accelerated rendering without X11/Wayland.

**Element** — A named graphic or media item in the director's vocabulary (e.g., "lower third", "bug", "ticker"). The Playout Agent's show configuration maps element names to channel+layer addresses. Directors say "take lower third" — they never reference layer numbers. See also: Layer.

**Ember+** — A control protocol used by broadcast equipment (vision mixers, audio mixers, routers) for monitoring and automation. TCP-based, hierarchical parameter model.

**Fill** — The RGB video content of a graphic (the visible part). Paired with a Key signal for compositing. See also: Key, DSK.

**Foreground / Background (FG/BG)** — Each layer has two slots. Foreground is on-air/visible. Background is cued/warm/invisible. Transitioning plays the background item into the foreground with a configurable transition effect.

**Frame Pipeline** — The Rust component that processes composited frames for output: colorspace conversion (sRGB to Rec.709), fill+key split, genlock sync, and routing to output adapters.

**Genlock (Generator Lock)** — Synchronizing all video equipment in a facility to a common timing reference. Ensures all sources are frame-aligned. The reference signal is typically blackburst, tri-level sync, or PTP.

**GPI (General Purpose Interface)** — Physical contact closure pins on broadcast equipment. Used for hardware button triggers (play, stop, next) and tally lights. Pivox uses AJA card GPI. GPI events route through the Playout Agent (not handled locally by the engine), enabling cross-engine triggering.

**Growing-File Playback** — Playing back content that is still being recorded. The read head chases the write head. A key capability for live replay workflows. See also: Video Ingest.

**HLG (Hybrid Log-Gamma)** — An HDR transfer function designed for live broadcast. Backward-compatible with SDR displays. Used by BBC, NHK. See also: PQ, HDR.

**Jog/Shuttle Controller** — Physical hardware for frame-accurate media scrubbing. Jog wheel provides frame-by-frame advance (relative ticks). Shuttle ring provides variable-speed playback (absolute position). USB HID devices (ShuttlePRO, Blackmagic Editor Keyboard) or serial/IP broadcast panels (DNF, Skaarhoj). Events route through the Playout Agent to the engine.

**Key** — A grayscale mask representing the transparency of a graphic. White = fully opaque, black = fully transparent. Paired with a Fill signal. The vision mixer uses the key to composite the fill over live video.

**Layer** — A compositing level within a channel. Layers stack bottom-to-top (higher number = on top). Any layer can use any plugin type (CEF, Rive, FFmpeg, stills) — the plugin is determined by the content assigned to it, not the layer position. Each layer has foreground and background slots and is independently addressable with its own cue/take/stop lifecycle. See also: Element.

**Layer Transform** — Per-layer positioning on the output canvas: position (x, y), scale, anchor point, crop, and opacity. Plugins render at their native size; the compositor places them on the canvas using the transform. Enables reusable template components across shows without baking position into content.

**License Entitlement** — The set of features a customer's license permits: channel count, resolution, output types, app modes, etc. Distributed from the Cloud Controller to the Playout Agent via the bidi gRPC stream. Cached locally for 30 days of offline operation.

**Lower Third** — A graphic overlay in the lower third of the screen, typically showing a person's name and title.

**MOS (Media Object Server)** — A legacy XML-over-TCP protocol for communication between newsroom computer systems (NRCS) and media devices (graphics, video servers). Pivox supports MOS for backward compatibility but aims to replace it with a modern gRPC protocol.

**NDI (Network Device Interface)** — A protocol by Vizrt for sending video/audio over standard IP networks (ethernet). Uses mDNS for auto-discovery. ~150 Mbps per 1080p60 stream. Pivox uses NDI for development preview and facility integration.

**NRCS (Newsroom Computer System)** — Software used by newsrooms to manage stories, scripts, and rundowns. Examples: AP ENPS, Avid iNEWS, Octopus. Integrates with Pivox via MOS or the future Pivox protocol.

**NTV2** — AJA's SDK/driver for their video I/O cards. Open source (GitHub: aja-video/ntv2). Provides APIs for frame output, genlock, GPI, audio embedding, VANC.

**Native App** — The Pivox operator application, built with SwiftUI (macOS) and WinUI 3 (Windows) with a shared C++ core. Replaces Electron. Native shell for menus/toolbars/forms, shared custom-drawn components for complex UI (timeline, waveform), CEF viewports for template preview. Can embed the engine for Designer mode.

**NVDEC / NVENC** — NVIDIA's hardware video decoder and encoder on their GPUs. FFmpeg uses NVDEC for hardware-accelerated video decode and NVENC for compliance recording.

**OSR (Off-Screen Rendering)** — CEF rendering mode where the browser renders to a pixel buffer in memory instead of a visible window. The engine controls frame timing by ticking CEF's message loop. Required on Windows (WinUI 3's DirectComposition paints over child HWNDs). macOS uses windowed mode (SetAsChild) instead.

**Playout Agent** — On-prem agent installed alongside the engine. Manages local engines, routes playout commands, relays data feeds, caches config and entitlements. Connects outbound to the Cloud Controller via bidi gRPC. Operates independently during cloud outages. Installed with a single script (same pattern as Storage Agent). See also: Cloud Controller, Storage Agent.

**PQ (Perceptual Quantizer)** — An HDR transfer function (SMPTE ST 2084). Used primarily for mastered content (Netflix, Dolby Vision). Not backward-compatible with SDR. See also: HLG, HDR.

**PsF (Progressive Segmented Frame)** — A method for transmitting progressive frames over interlaced infrastructure. The AJA card splits each progressive frame into two fields. The engine always renders progressive; PsF is a card-level output configuration.

**PTP (Precision Time Protocol, IEEE 1588)** — A network protocol for sub-microsecond time synchronization. Used in ST 2110 IP facilities instead of physical genlock signals. A PTP grandmaster clock synchronizes all devices.

**Rec.709** — The color space standard for HD broadcast (HDTV). Defines color primaries, transfer function, and white point. sRGB (used by CEF/browsers) shares the same primaries but has a slightly different transfer function.

**Rec.2020** — The color space standard for UHD/4K broadcast. Wider color gamut than Rec.709. Required for HDR content.

**Remote MCP Server** — A Model Context Protocol server running on a remote build machine (e.g., Windows). Enables cross-platform development from macOS — Claude Code on macOS calls the Windows MCP server to trigger builds, run tests, and capture results. Git is the only shared state. See `docs/dev/cross-platform-workflow.md`.

**Rive** — A design tool and runtime for interactive 2D animations. Pivox uses Rive's native C/C++ runtime (not WASM) for high-performance motion graphics via the Rive plugin.

**Rundown** — An ordered list of items (graphics, video, audio) that make up a show segment or entire show. The operator or automation system advances through the rundown.

**rustfs** — S3-compatible object storage for on-prem deployments. Used instead of MinIO due to licensing concerns. Stores templates, assets, and compliance recordings.

**SDI (Serial Digital Interface)** — The standard physical interface for broadcast video. Carries video, audio (embedded), and metadata (VANC) on a single coaxial cable. Variants: 3G-SDI (1080p), 12G-SDI (4K).

**Shared Memory** — Memory-mapped regions used by the Data Plane to deliver high-frequency feed data to the engine. Lock-free double-buffer pattern ensures zero-contention reads.

**SRT (Secure Reliable Transport)** — A protocol for sending video over unreliable networks (internet). Handles packet loss, jitter, encryption. Used for WAN delivery in Pivox Cloud tier.

**ST 2110** — SMPTE standard for professional video over IP. Separates video (ST 2110-20), audio (ST 2110-30), and metadata (ST 2110-40) into independent IP streams. Uses PTP for synchronization. Replacing SDI routers in large facilities.

**Stinger** — A short animated transition graphic (typically 0.5-2 seconds) used between show segments or as a branded transition element.

**Storage Agent** — On-prem agent that proxies and caches assets on the local network. Serves templates, images, video clips to browsers, native app, and engines via HTTPS. Installed with a single script. Managed from the Cloud Controller. Same agent pattern as Playout Agent. See `docs/storage.md`.

**SW-P-08** — A serial/TCP protocol for controlling broadcast video routers (Evertz, Grass Valley). The Playout Agent uses this to route sources to destinations.

**Tally** — An indicator showing which source is currently on-air (red = program/live) or in preview (green). Distributed via TSL UMD protocol from the vision mixer to all connected devices.

**Ticker** — A scrolling text crawl, typically at the bottom of the screen. Used for news headlines, stock prices, sports scores.

**Tri-Level Sync** — A timing reference signal for HD broadcast equipment. Replaces blackburst for HD. See also: Genlock, Blackburst.

**TSL UMD (Television Systems Ltd, Universal Monitor Driver)** — A protocol for distributing tally information and under-monitor display labels from vision mixers to monitoring equipment.

**VANC (Vertical Ancillary Data)** — A data region within the SDI signal used to carry metadata: closed captions (CEA-608/708), timecode, AFD (Active Format Description). AJA NTV2 SDK supports reading/writing VANC.

**VDCP (Video Disk Control Protocol)** — A serial protocol (RS-422 or TCP) for controlling video servers and playout automation. Used to trigger play/stop/cue commands from automation systems.

**Video Ingest** — Future capability: capturing live SDI feeds into storage for replay workflows. AJA cards are bidirectional — same hardware used for output can capture input. Phased roadmap from basic ingest through growing-file playback to full EVS-style replay. See `docs/video-ingest.md`.

**VideoToolbox** — Apple's hardware video encode/decode framework on macOS and iOS. Uses Apple Silicon's dedicated media engine. Equivalent to NVENC/NVDEC on NVIDIA GPUs. Used by the engine and native app for hardware-accelerated encoding on macOS.

**View Model** — The SDK's reactive data layer. Templates bind fields to DOM elements via `pivox.model.bind()`. When data changes (from any source), bindings fire automatically and the template updates.

**Vision Mixer (Production Switcher)** — Hardware that switches between video sources (cameras, graphics, VTRs) and composites them for broadcast output. Pivox feeds the mixer as one or more sources. Examples: Grass Valley Kayenne, Sony XVS, Ross Carbonite, Blackmagic ATEM.

**WebGPU** — A modern web API for GPU-accelerated graphics and compute. Available in CEF via Chromium's Dawn implementation. Used by templates for 3D graphics, particle effects, and shader-based animations.

**wgpu** — A Rust implementation of the WebGPU standard. Available as a cross-platform GPU abstraction if the engine ever needs native GPU compute (e.g., HDR tone mapping).

**Workspace** — A mode within the native app that presents a role-specific panel layout. Workspaces: Operator, Designer, Library, Engineering, Admin. Users with multiple roles switch between workspaces. License entitlements gate which workspaces are visible.
