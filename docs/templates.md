# Pivox Templates — Authoring Guide

## Overview

Pivox templates are HTML/CSS/JS applications that render broadcast graphics, video overlays, and data visualizations. They run inside CEF (Chromium Embedded Framework) and interact with the playout engine via the Pivox SDK.

**Related docs:**
- `docs/sdk.md` — full SDK API reference (view model, feeds, native bindings)
- `docs/data-plane.md` — live data feeds, shared memory, schema versioning
- `docs/engine.md` — engine internals, compositor, rendering pipeline

## Template Anatomy

A template is a directory containing HTML, CSS, JS, and a manifest:

```
templates/
  └── lower-third/
       ├── manifest.json       # declares fields, data bindings, transitions
       ├── index.html          # entry point — CEF loads this
       ├── style.css
       ├── animation.js
       └── assets/             # local assets (bundled with template)
            ├── background.png
            └── font.woff2
```

CEF loads `index.html`. The Pivox SDK is injected before the page loads — templates do not include it manually.

## Template Manifest

The manifest declares the template's metadata, fields, data bindings, and default transition:

```json
{
  "name": "lower-third",
  "version": "1.2.0",
  "description": "Standard lower-third name strap",
  "author": "Pivox",
  "thumbnail": "thumbnail.png",
  "category": "lower-thirds",
  "tags": ["name", "title", "strap"],

  "default_transition": {
    "type": "PUSH",
    "direction": "UP",
    "duration_ms": 500
  },

  "fields": {
    "name": {
      "type": "string",
      "label": "Name",
      "default_update_mode": "manual",
      "required": true
    },
    "title": {
      "type": "string",
      "label": "Title / Role",
      "default_update_mode": "manual"
    },
    "logo": {
      "type": "asset",
      "label": "Logo",
      "default_update_mode": "manual"
    }
  },

  "feeds": {},

  "capabilities": {
    "multi_step": false,
    "audio": false
  }
}
```

### Field Types

| Type | Description | Example |
|---|---|---|
| `string` | Text value | Name, title, headline |
| `number` | Numeric value | Score, vote count, percentage |
| `boolean` | True/false | Winner flag, alert active |
| `asset` | Asset reference (resolved via `pivox.assets.resolve()`) | Logo, photo, background image |
| `color` | CSS color value | Team color, brand color |
| `enum` | Constrained string choices | Position (left/center/right) |

### Update Modes

Declared as `default_update_mode` in manifest — operator can override at runtime:

| Mode | Behavior |
|---|---|
| `manual` | Operator enters value in UI. No data feed. |
| `auto` | Data feed updates value automatically. No operator approval. |
| `gated` | Data feed provides value. Held as pending until operator approves. |

See `docs/data-plane.md` for full data routing architecture.

## Template Lifecycle

```
LOAD (background slot)
  │  CEF loads index.html
  │  SDK injected
  │  onLoad(model) called — set up bindings
  │  Template is warm, invisible
  │
  ▼
PLAY (transition BG → FG)
  │  onPlay() called
  │  Template animates in
  │  Call pivox.ready() when animation complete
  │  Template is on-air
  │
  ▼
UPDATE (while on-air)
  │  SDK view model patched
  │  Bindings/watchers fire automatically
  │  Template updates visually (animation, count-up, etc.)
  │  [repeats as data changes]
  │
  ▼
NEXT (optional — multi-step graphics)
  │  onNext() called
  │  Template advances to next page/state
  │
  ▼
STOP (transition FG → gone)
  │  onStop() called
  │  Template animates out
  │  Call pivox.done() when animation complete
  │  Template is removed
```

## Template Design Principles

### Data belongs in feeds, presentation belongs in templates

**Feed data (shared memory / view model):**
```
candidate.votes: 1234567
candidate.party: "D"
```

**Template logic (JavaScript):**
```javascript
color = candidate.party === "D" ? "#0015BC" : "#E9141D"
formatted = candidate.votes.toLocaleString()  // "1,234,567"
barWidth = (candidate.votes / totalVotes * 100) + "%"
```

Don't put presentation concerns (colors, formatting, layout variations) in the data feed. The feed carries raw data. The template transforms and presents it.

### Templates are pure views

Templates don't know:
- Where data comes from (operator, feed, automation)
- Whether updates are auto, gated, or manual
- What the throttle rate is
- Which data provider is connected

Templates do know:
- What fields they need (declared in manifest)
- How to display those fields (HTML/CSS/JS)
- How to animate in/out
- How to handle data changes visually

### Handle missing data gracefully

Fields may be `undefined` if:
- The data feed hasn't provided a value yet
- The operator hasn't entered a value
- The schema version doesn't include this field

Always provide fallbacks:
```javascript
pivox.model.bind('score', this.$('#score'), {
  format: v => v !== undefined ? v.toString() : '--'
});
```

### Handle overlapping animations

If `pivox.model.watch()` fires while a previous animation is still running (e.g., a count-up animation mid-way through when new data arrives), retarget the animation to the new value — don't queue or ignore:

```javascript
pivox.model.watch('votes', (newVal, oldVal) => {
  // Cancel current animation if running, retarget to new value
  this.animateCounter(this.$('#votes'), newVal, 500);
});
```

## Built-In Templates

Pivox ships built-in templates for common broadcast use cases:

```
templates/
  ├── lower-third/           # Name strap
  ├── ticker/                # Scrolling text crawl
  ├── scoreboard/            # Sports scoreboard
  ├── audio-visualizer/
  │   ├── waveform/          # Animated waveform bars
  │   ├── vu-meter/          # Classic VU meter
  │   ├── spectrum/          # Frequency spectrum
  │   └── minimal/           # Simple level indicator + title
  └── test-signals/
      ├── smpte-bars/        # SMPTE color bars + tone
      └── slate/             # Channel ident slate
```

### Audio Visualizer Templates

Audio visualizers are standard templates that use `pivox.native.getAudioLevels()` to read audio levels from a specific layer. The Go control plane bundles an audio file (FFmpeg layer) with a visualizer template as a single operator action:

```
Operator clicks "Play Audio" →
  Control plane sends:
    1. VideoLoadCommand (audio file on layer 0)
    2. LoadCommand (visualizer on layer 1, audio_layer=0 in view model)
    3. LoadCommand (lower-third on layer 2)
```

The visualizer reads the layer ID from its view model and subscribes to audio levels:

```javascript
onLoad(model) {
  this.audioLayer = pivox.model.get('audio_layer');
}
onPlay() {
  pivox.timing.requestFrame(() => this.draw());
  pivox.ready();
}
draw() {
  const levels = pivox.native.getAudioLevels({ layer: this.audioLayer });
  this.renderVisualization(levels);
  pivox.timing.requestFrame(() => this.draw());
}
```

Custom visualizer themes are just more templates using the same pattern.

## Development Workflow

### Tier 1: Browser (fastest iteration)

1. Include `pivox-sdk-mock.js` in your HTML
2. Open in Chrome/Firefox
3. Use browser DevTools for debugging
4. Hot module reload via Vite/webpack/etc.
5. No engine needed — pure frontend development

```html
<!-- Development only — remove for production -->
<script src="https://unpkg.com/@pivox/sdk-mock"></script>

<script>
  // Mock data for development
  PivoxMock.setModel({
    name: "John Smith",
    title: "CEO, Acme Corp"
  });
</script>
```

### Tier 2: Local Engine + NDI (validation)

1. Run Pivox engine locally (macOS or Windows)
2. Open Pivox Electron app
3. Load template — real SDK, real bindings, real timing
4. View output in NDI Monitor (free, any device on network)
5. Template hot-reload: edit → save → auto-reload in engine

### Tier 3: Staging (pre-air)

1. Deploy template to staging server via template registry
2. QA runs through all data scenarios
3. Verify on SDI output if available
4. Producer signs off

### Tier 4: Production

- Only approved/published templates available
- Template registry enforces version control and approval workflow

## Template Versioning

Templates follow semantic versioning and go through an approval workflow:

```
Draft → In Review → Approved → Published → Deprecated
                 ↓
              Rejected (with feedback)
```

| State | Available On |
|---|---|
| Draft | Developer's machine only |
| In Review | Staging engine (for review) |
| Approved | Staging engine (for final testing) |
| Published | Production engine |
| Deprecated | Production (existing uses continue, new uses blocked) |

See `docs/control-plane.md` — Template Registry for full versioning details.

## Custom Web Apps as Templates

Templates are not limited to simple graphics. Any web application that implements the Pivox SDK lifecycle hooks is a valid template:

- Complex data visualizations (D3.js, Chart.js)
- 3D graphics (Three.js, Babylon.js via WebGPU)
- Interactive maps
- Multi-page infographics (using `onNext()`)
- Video call interfaces (WebRTC via CEF)

The only requirement: implement `onLoad()`, `onPlay()`, `onStop()`, and call `pivox.ready()` / `pivox.done()` at the right times.
