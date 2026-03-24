# Pivox Protocols & API Definitions

## Overview

- All inter-component communication uses gRPC with protobuf
- Reference docs: [engine.md](engine.md), [control-plane.md](control-plane.md), [data-plane.md](data-plane.md)
- Three protocol boundaries: CP-Engine, Supervisor-Channel, Plugin Protocol

## Communication Architecture

### Go Control Plane <-> Rust Engine

**Protocol:** gRPC over Unix domain sockets (or TCP for remote deployments).

### Rust Supervisor <-> Channel Processes

**Protocol:** Raw protobuf frames over Unix domain sockets (no gRPC overhead).

The supervisor and channel processes are both Rust (with C++ CEF host). The command set is small and fixed. Using raw length-prefixed protobuf messages avoids linking gRPC into every channel process.

### Plugin Protocol

**Protocol:** gRPC + shared memory for third-party engine plugins.

gRPC carries commands and signaling; shared memory carries pixel and audio data at frame rate (zero-copy).

### Why gRPC

**Performance justification:** Command latency (~0.3ms over UDS) is negligible vs. frame period (16.68ms at 59.94fps). A play command is ~50-200 bytes as protobuf, ~300 bytes on the wire with gRPC framing. The command triggers an animation that takes 50+ ms to visually manifest. Sub-millisecond IPC overhead is irrelevant.

The performance-critical path is the frame pipeline (CEF render -> GPU readback -> colorspace -> AJA output), not the command channel.

Additional benefits:
- Broadcast custom protocols (AMCP, VDCP) were designed in an era before gRPC existed
- Bidirectional streaming lets the engine push status continuously without polling
- Protobuf schemas are shared between Go and Rust -- type-safe, versioned
- Both Go and Rust have excellent gRPC implementations (tonic for Rust)

## Playout Engine Service

```protobuf
service PlayoutEngine {
  // Streaming command channel: control plane -> engine
  rpc Execute (stream PlayoutCommand) returns (stream CommandAck);

  // Continuous status stream: engine -> control plane
  rpc WatchStatus (StatusRequest) returns (stream ChannelStatus);

  // Input injection for preview/design interaction (mouse, keyboard, touch)
  rpc SendInput (stream InputEvent) returns (stream InputAck);
}
```

### RPCs

| RPC | Direction | Pattern | Purpose |
|---|---|---|---|
| `Execute` | CP -> Engine | Bidi stream | Send playout commands, receive acks |
| `WatchStatus` | Engine -> CP | Server stream | Continuous channel state updates |
| `SendInput` | CP -> Engine | Bidi stream | Mouse, keyboard, touch injection |

## Command Messages

### PlayoutCommand

Top-level command envelope addressing a specific channel and layer.

```protobuf
message PlayoutCommand {
  string request_id = 1;
  int32 channel = 2;
  int32 layer = 3;
  oneof command {
    // Graphics template commands
    LoadCommand load = 10;       // load into background slot (warm, invisible)
    PlayCommand play = 11;       // transition BG -> FG, or load+play directly
    StopCommand stop = 12;       // animate out foreground
    UpdateCommand update = 13;   // patch view model data on-air
    NextCommand next = 14;       // advance multi-step graphic
    ClearCommand clear = 15;     // immediately remove (no animation), both slots

    // Video playback commands
    VideoLoadCommand video_load = 20;
    VideoPlayCommand video_play = 21;
    VideoPauseCommand video_pause = 22;
    VideoSeekCommand video_seek = 23;
    VideoSpeedCommand video_speed = 24;
    VideoStopCommand video_stop = 25;
  }
}
```

### Graphics Template Commands

```protobuf
message LoadCommand {
  string template_uri = 1;        // e.g. "template://lower-third/v2"
  bytes data = 2;                 // JSON payload -- initial view model state
}

message PlayCommand {
  string template_uri = 1;        // if set, load+play in one step (no prior Load needed)
  bytes data = 2;                 // JSON payload -- initial view model state
  TransitionType transition = 3;
  uint32 transition_duration_ms = 4;  // 0 = cut (instant)
  TransitionDirection direction = 5;  // for push/wipe types
  string custom_shader_ref = 6;      // asset reference for CUSTOM type
}

message UpdateCommand {
  bytes data = 1;                 // JSON payload -- patches to view model fields
}

message StopCommand {
  TransitionType transition = 1;          // optional out-transition
  uint32 transition_duration_ms = 2;
}

message NextCommand {}
message ClearCommand {}
```

### Video Playback Commands

```protobuf
message VideoLoadCommand {
  string uri = 1;                 // file path or network URL
  Timecode in_point = 2;          // mark in (optional)
  Timecode out_point = 3;         // mark out (optional)
  bool loop = 4;
  bool paused = 5;                // load paused (cue to first frame)
}

message VideoPlayCommand {
  float speed = 1;                // 1.0 = normal, 0.5 = half speed, -1.0 = reverse
  TransitionType transition = 2;
  uint32 transition_duration_ms = 3;
  TransitionDirection direction = 4;
  string custom_shader_ref = 5;
}

message VideoPauseCommand {}

message VideoSeekCommand {
  oneof target {
    Timecode timecode = 1;        // seek to timecode
    int64 frame_number = 2;       // seek to absolute frame
    float percentage = 3;         // seek to percentage of clip duration
  }
}

message VideoSpeedCommand {
  float speed = 1;                // variable speed (-4.0 to 4.0)
}

message VideoStopCommand {}
```

### Timecode

```protobuf
message Timecode {
  int32 hours = 1;
  int32 minutes = 2;
  int32 seconds = 3;
  int32 frames = 4;
}
```

## Status Messages

### CommandAck

```protobuf
message CommandAck {
  string request_id = 1;
  bool success = 2;
  string error = 3;
}
```

### ChannelStatus

```protobuf
message ChannelStatus {
  int32 channel = 1;
  repeated LayerState layers = 2;
  HealthStatus health = 3;
  TimecodeInfo timecode = 4;
  uint64 frames_rendered = 5;
  uint64 frames_dropped = 6;
}
```

### LayerState

```protobuf
message LayerState {
  int32 layer = 1;
  LayerType type = 2;            // GRAPHICS or VIDEO
  SlotState foreground = 3;      // currently on-air / visible
  SlotState background = 4;      // cued / warm / invisible
  bool transitioning = 5;        // true during FG->BG transition
  float transition_progress = 6; // 0.0-1.0 during transition
}
```

### SlotState

```protobuf
message SlotState {
  string source = 1;            // template_uri or clip path
  LayerStatus status = 2;       // EMPTY, LOADED, PLAYING, PAUSED, STOPPING
  // Video-specific (populated only for VIDEO layers)
  Timecode position = 3;        // current playback position
  Timecode duration = 4;        // total clip duration
  float speed = 5;              // current playback speed
  bool has_captions = 6;        // true if clip contains CC track
  string caption_format = 7;    // "CEA-608", "CEA-708", "" if none
}
```

### Enums

```protobuf
enum LayerType {
  GRAPHICS = 0;
  VIDEO = 1;
}

enum TransitionType {
  CUT = 0;
  MIX = 1;
  PUSH = 2;
  WIPE_EDGE = 3;
  WIPE_BOX = 4;
  WIPE_CIRCLE = 5;
  DVE = 6;
  CUSTOM = 7;          // user-provided GPU shader from asset system
}

enum TransitionDirection {
  LEFT = 0;
  RIGHT = 1;
  UP = 2;
  DOWN = 3;
}

enum LayerStatus {
  EMPTY = 0;
  LOADED = 1;       // cued, first frame visible (video) or populated (graphics)
  PLAYING = 2;
  PAUSED = 3;       // video only -- frozen on current frame
  STOPPING = 4;     // graphics out-animation in progress
}
```

## Input Messages

### InputEvent

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
```

### Mouse Events

```protobuf
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
```

### Keyboard Events

```protobuf
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
```

### InputAck

```protobuf
message InputAck {
  bool accepted = 1;
  string reason = 2;           // e.g., "channel is on-air, input rejected"
}
```

### MouseButton Enum

```protobuf
enum MouseButton {
  LEFT = 0;
  MIDDLE = 1;
  RIGHT = 2;
}
```

## Plugin Protocol

The Plugin Protocol is an open spec that any external renderer can implement to become a source in Pivox's compositor. Pivox's own built-in engines (CEF and FFmpeg) are built on this same protocol -- it is not an afterthought or second-class interface.

The protocol defines:
1. **Command reception** -- play, stop, update, load, next, clear (same command set as CEF templates)
2. **Frame delivery** -- RGBA pixel buffers via shared memory at the channel's frame rate
3. **Audio delivery** -- PCM samples synced to frames via shared memory
4. **Status reporting** -- loaded, playing, stopped, health, errors
5. **Genlock sync** -- render at the frame rate Pivox specifies

### PivoxPluginHost Service

```protobuf
service PivoxPluginHost {
  // Pivox -> Plugin: commands
  rpc Configure (PluginConfig) returns (PluginConfigAck);
  rpc Execute (stream PluginCommand) returns (stream PluginCommandAck);
}
```

### PivoxPluginClient Service

```protobuf
service PivoxPluginClient {
  // Plugin -> Pivox: frame delivery + status
  rpc DeliverFrames (stream PluginFrame) returns (stream FrameAck);
  rpc ReportStatus (stream PluginStatus) returns (Empty);
}
```

### Configuration Messages

```protobuf
// Pivox sends configuration to the plugin at startup
message PluginConfig {
  int32 width = 1;
  int32 height = 2;
  float frame_rate = 3;           // plugin must render at this rate
  string shared_memory_path = 4;  // for frame delivery (zero-copy)
  int32 audio_sample_rate = 5;    // 48000
  int32 audio_channels = 6;
}

// Plugin responds with its capabilities -- tells Pivox what it can do
message PluginConfigAck {
  bool accepted = 1;
  string error = 2;
  PluginCapabilities capabilities = 3;
}
```

### PluginCapabilities

```protobuf
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
```

### Plugin Command Messages

```protobuf
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
  bytes data = 2;         // JSON -- initial data
}

message PluginPlayCommand {}
message PluginStopCommand {}

message PluginUpdateCommand {
  bytes data = 1;         // JSON -- data patches
}

message PluginNextCommand {}
message PluginClearCommand {}
```

### Frame and Status Messages

```protobuf
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
```

### Plugin Enums

```protobuf
enum PluginType {
  GRAPHICS = 0;     // CEF, Rive, etc. -- primarily graphics/animation
  VIDEO = 1;        // FFmpeg -- primarily video/clip playback
  AUDIO = 2;        // audio-only playback
  HYBRID = 3;       // Unreal, Godot -- video + graphics + audio
}

enum PluginState {
  IDLE = 0;
  LOADING = 1;
  READY = 2;        // equivalent to LOADED -- warm, ready for instant play
  PLAYING = 3;
  ERROR = 4;
}
```

## Pivox Integration Protocol (Future -- MOS Replacement)

**Strategic goal:** Replace MOS with a modern integration protocol for NRCS and automation systems.

**Design principles:**
- gRPC/protobuf (not XML/TCP)
- Bidirectional streaming (not request/response polling)
- Real-time (sub-second, not seconds-to-minutes like MOS)
- Type-safe schemas (not freeform XML)
- Event-driven (push state changes, don't poll)
- Authentication and encryption (TLS, API keys)
- Versioned (backward-compatible schema evolution)

**Scope:**
- Rundown sync (create, update, reorder items)
- Item control (play, stop, update data, cue, clear)
- Status subscription (channel state, on-air items, tally)
- Asset management (upload, reference, query)
- Template management (list, query capabilities, field schemas)
- Data binding (push data updates, subscribe to feed state)

This protocol would be published as an open spec -- allowing NRCS vendors (AP ENPS, Avid iNEWS, Octopus, others) and automation vendors to integrate directly without MOS as an intermediary. It would also serve as the API for custom integrations, third-party control surfaces, and Pivox's own mobile apps.

To be designed separately -- requires input from potential NRCS integration partners and broadcast automation vendors.

## Proto File Organization

```
proto/
├── playout.proto    # Commands, status
├── channel.proto    # Channel/layer state
├── input.proto      # Mouse, keyboard, touch
├── plugin.proto     # Plugin protocol
└── preview.proto    # MJPEG preview signaling
```
