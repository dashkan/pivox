# Pivox Documentation

Architecture and design documentation for the Pivox broadcast playout system.

## Documents

### System Architecture
- `architecture.md` — Deployment tiers (cloud, hybrid, on-prem), hybrid model, storage, security, disaster recovery
- `control-plane.md` — Go control plane: NRCS, operator UI, hardware automation, services
- `data-plane.md` — Live data infrastructure: feeds, shared memory, hierarchical KV store, throttling, schemas

### Playout Engine
- `engine.md` — Core engine: compositor, frame pipeline, process model, output routing
- `hardware.md` — AJA cards, genlock, GPI, closed captioning, ST 2110, NDI, audio pipeline, HDR
- `protocols.md` — gRPC protobuf definitions: playout commands, channel status, input, plugin protocol

### Plugins
- `plugins/plugin-sdk.md` — Plugin SDK: in-process/out-of-process dual mode, PivoxPlugin trait, capabilities
- `plugins/plugin-cef.md` — CEF plugin: HTML/JS graphics, OSR, V8 bindings, WebGPU, remote debugging
- `plugins/plugin-ffmpeg.md` — FFmpeg plugin: video, audio, stills, variable speed, codecs
- `plugins/plugin-rive.md` — Rive plugin: 2D animation, C/C++ runtime, state machines, designer workflow
- `plugins/plugin-unreal.md` — Unreal Engine integration (future, Phase 6)

### Developer Guides
- `sdk.md` — JavaScript SDK: view model, feeds, native bindings, system data sources, browser mock
- `templates.md` — Template authoring: manifest spec, lifecycle hooks, development workflow
- `tooling.md` — Developer tools: Rive, CLIs, SDKs, scaffolding, build tools

### Reference
- `licensing.md` — FFmpeg LGPL, codec patents, third-party library obligations
- `glossary.md` — Broadcast terminology for the team

### Internal
- `dev/repo-structure.md` — Repository structure: 10 repos under dashkan org, dependency graph, development order
- `dev/engine-build.md` — Engine build system: Cargo + CMake + vcpkg, CEF binary distribution

## Related Repositories

| Repo | Description |
|---|---|
| [pivox-server](https://github.com/dashkan/pivox-server) | Go control plane |
| [pivox-web](https://github.com/dashkan/pivox-web) | React operator UI + Electron |
