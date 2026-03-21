# Pivox Glossary — Broadcast & System Terminology

Terms used across Pivox documentation. Organized alphabetically.

---

**AFV (Audio Follow Video)** — When transitioning between video/graphics elements, audio crossfades in sync with the video transition. Cut video = cut audio. Dissolve video = crossfade audio.

**AJA** — Manufacturer of professional video I/O hardware (PCIe cards and Thunderbolt devices). Pivox uses AJA cards for SDI and ST 2110 output. Key products: Corvid 88, Corvid 44 12G, Kona 5.

**AutoCirculate** — AJA NTV2 SDK API for scheduling video frames to output at precise genlock-synced timing. The engine writes frames to AutoCirculate buffers; the card outputs them at the correct time.

**Blackburst** — An analog reference signal (black video with sync pulses) used to synchronize all equipment in a broadcast facility. Being replaced by tri-level sync and PTP. See also: Genlock.

**CEA-608 / CEA-708** — Closed captioning standards. CEA-608 is the legacy analog standard. CEA-708 is the digital standard for HD/UHD. Carried in the SDI signal's VANC space.

**CEF (Chromium Embedded Framework)** — An open-source framework for embedding a Chromium browser in applications. Pivox uses CEF in Off-Screen Rendering (OSR) mode to render HTML/JS templates without a visible window.

**Channel** — One output path in the engine. Each channel produces a fill+key SDI pair (or NDI stream). Each channel runs as a separate OS process containing CEF, FFmpeg, compositor, and audio mixer.

**Changeover Switch** — Hardware device (Nevion, Evertz) that switches between primary and backup video sources. Used for Pivox redundancy — if Engine A fails, the switch cuts to Engine B's output.

**Closed Captioning (CC)** — Text overlay for hearing-impaired viewers. Carried as data in the SDI signal (VANC), not burned into the video. A regulatory requirement in most broadcast markets.

**Compositor** — The Rust component that merges all layers (video + graphics) into a single RGBA output per frame. Handles layer stacking, alpha blending, and transitions between foreground/background slots.

**Data Plane** — Pivox's live data infrastructure. Connects external data feeds to on-air templates with operator control (gating, approval, pause, override), throttling, schema versioning, and high-performance shared memory delivery.

**DOG (Digitally Originated Graphic)** — A persistent on-screen graphic, typically a channel logo or bug in the corner of the screen. Also called a "bug."

**DSK (Downstream Key)** — A compositing method where the vision mixer overlays graphics (from Pivox) on top of live video. Pivox outputs fill (the graphic) and key (the transparency mask) as separate SDI signals. The mixer's downstream keyer composites them.

**DVE (Digital Video Effect)** — A transition or effect that transforms video/graphics spatially — squeeze, zoom, spin, picture-in-picture.

**EGL** — An API for connecting rendering APIs (OpenGL, Vulkan) to the native windowing system. CEF uses `--use-gl=egl` on Linux for GPU-accelerated rendering without X11/Wayland.

**Ember+** — A control protocol used by broadcast equipment (vision mixers, audio mixers, routers) for monitoring and automation. TCP-based, hierarchical parameter model.

**Fill** — The RGB video content of a graphic (the visible part). Paired with a Key signal for compositing. See also: Key, DSK.

**Foreground / Background (FG/BG)** — Each layer has two slots. Foreground is on-air/visible. Background is cued/warm/invisible. Transitioning plays the background item into the foreground with a configurable transition effect.

**Frame Pipeline** — The Rust component that processes composited frames for output: colorspace conversion (sRGB to Rec.709), fill+key split, genlock sync, and routing to output adapters.

**Genlock (Generator Lock)** — Synchronizing all video equipment in a facility to a common timing reference. Ensures all sources are frame-aligned. The reference signal is typically blackburst, tri-level sync, or PTP.

**GPI (General Purpose Interface)** — Physical contact closure pins on broadcast equipment. Used for hardware button triggers (play, stop, next) and tally lights. Pivox uses AJA card GPI.

**HLG (Hybrid Log-Gamma)** — An HDR transfer function designed for live broadcast. Backward-compatible with SDR displays. Used by BBC, NHK. See also: PQ, HDR.

**Key** — A grayscale mask representing the transparency of a graphic. White = fully opaque, black = fully transparent. Paired with a Fill signal. The vision mixer uses the key to composite the fill over live video.

**Layer** — A compositing level within a channel. Layers stack bottom-to-top (higher number = on top). Each layer can contain a graphics template (CEF) or video clip (FFmpeg). Each layer has foreground and background slots.

**Lower Third** — A graphic overlay in the lower third of the screen, typically showing a person's name and title.

**MOS (Media Object Server)** — A legacy XML-over-TCP protocol for communication between newsroom computer systems (NRCS) and media devices (graphics, video servers). Pivox supports MOS for backward compatibility but aims to replace it with a modern gRPC protocol.

**NDI (Network Device Interface)** — A protocol by Vizrt for sending video/audio over standard IP networks (ethernet). Uses mDNS for auto-discovery. ~150 Mbps per 1080p60 stream. Pivox uses NDI for development preview and facility integration.

**NRCS (Newsroom Computer System)** — Software used by newsrooms to manage stories, scripts, and rundowns. Examples: AP ENPS, Avid iNEWS, Octopus. Integrates with Pivox via MOS or the future Pivox protocol.

**NTV2** — AJA's SDK/driver for their video I/O cards. Open source (GitHub: aja-video/ntv2). Provides APIs for frame output, genlock, GPI, audio embedding, VANC.

**NVDEC / NVENC** — NVIDIA's hardware video decoder and encoder on their GPUs. FFmpeg uses NVDEC for hardware-accelerated video decode and NVENC for compliance recording.

**OSR (Off-Screen Rendering)** — CEF rendering mode where the browser renders to a pixel buffer in memory instead of a visible window. The engine controls frame timing by ticking CEF's message loop.

**PQ (Perceptual Quantizer)** — An HDR transfer function (SMPTE ST 2084). Used primarily for mastered content (Netflix, Dolby Vision). Not backward-compatible with SDR. See also: HLG, HDR.

**PsF (Progressive Segmented Frame)** — A method for transmitting progressive frames over interlaced infrastructure. The AJA card splits each progressive frame into two fields. The engine always renders progressive; PsF is a card-level output configuration.

**PTP (Precision Time Protocol, IEEE 1588)** — A network protocol for sub-microsecond time synchronization. Used in ST 2110 IP facilities instead of physical genlock signals. A PTP grandmaster clock synchronizes all devices.

**Rec.709** — The color space standard for HD broadcast (HDTV). Defines color primaries, transfer function, and white point. sRGB (used by CEF/browsers) shares the same primaries but has a slightly different transfer function.

**Rec.2020** — The color space standard for UHD/4K broadcast. Wider color gamut than Rec.709. Required for HDR content.

**Rundown** — An ordered list of items (graphics, video, audio) that make up a show segment or entire show. The operator or automation system advances through the rundown.

**rustfs** — S3-compatible object storage for on-prem deployments. Used instead of MinIO due to licensing concerns. Stores templates, assets, and compliance recordings.

**SDI (Serial Digital Interface)** — The standard physical interface for broadcast video. Carries video, audio (embedded), and metadata (VANC) on a single coaxial cable. Variants: 3G-SDI (1080p), 12G-SDI (4K).

**Shared Memory** — Memory-mapped regions used by the Data Plane to deliver high-frequency feed data to the engine. Lock-free double-buffer pattern ensures zero-contention reads.

**SRT (Secure Reliable Transport)** — A protocol for sending video over unreliable networks (internet). Handles packet loss, jitter, encryption. Used for WAN delivery in Pivox Cloud tier.

**ST 2110** — SMPTE standard for professional video over IP. Separates video (ST 2110-20), audio (ST 2110-30), and metadata (ST 2110-40) into independent IP streams. Uses PTP for synchronization. Replacing SDI routers in large facilities.

**Stinger** — A short animated transition graphic (typically 0.5-2 seconds) used between show segments or as a branded transition element.

**SW-P-08** — A serial/TCP protocol for controlling broadcast video routers (Evertz, Grass Valley). The control plane uses this to route sources to destinations.

**Tally** — An indicator showing which source is currently on-air (red = program/live) or in preview (green). Distributed via TSL UMD protocol from the vision mixer to all connected devices.

**Ticker** — A scrolling text crawl, typically at the bottom of the screen. Used for news headlines, stock prices, sports scores.

**Tri-Level Sync** — A timing reference signal for HD broadcast equipment. Replaces blackburst for HD. See also: Genlock, Blackburst.

**TSL UMD (Television Systems Ltd, Universal Monitor Driver)** — A protocol for distributing tally information and under-monitor display labels from vision mixers to monitoring equipment.

**VANC (Vertical Ancillary Data)** — A data region within the SDI signal used to carry metadata: closed captions (CEA-608/708), timecode, AFD (Active Format Description). AJA NTV2 SDK supports reading/writing VANC.

**VDCP (Video Disk Control Protocol)** — A serial protocol (RS-422 or TCP) for controlling video servers and playout automation. Used to trigger play/stop/cue commands from automation systems.

**View Model** — The SDK's reactive data layer. Templates bind fields to DOM elements via `pivox.model.bind()`. When data changes (from any source), bindings fire automatically and the template updates.

**Vision Mixer (Production Switcher)** — Hardware that switches between video sources (cameras, graphics, VTRs) and composites them for broadcast output. Pivox feeds the mixer as one or more sources. Examples: Grass Valley Kayenne, Sony XVS, Ross Carbonite, Blackmagic ATEM.

**WebGPU** — A modern web API for GPU-accelerated graphics and compute. Available in CEF via Chromium's Dawn implementation. Used by templates for 3D graphics, particle effects, and shader-based animations.

**wgpu** — A Rust implementation of the WebGPU standard. Available as a cross-platform GPU abstraction if the engine ever needs native GPU compute (e.g., HDR tone mapping).
