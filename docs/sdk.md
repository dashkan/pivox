# Pivox SDK — Template Developer Reference

## Overview

The Pivox SDK is the JavaScript API available to every HTML/JS template running in the playout engine. It is injected by CEF (Chromium Embedded Framework) before the template loads — template authors do not import or include it.

The template is a **pure view**. It binds to a reactive view model managed by the SDK and handles animations. The template does not know:

- Where data comes from (operator input, live feed, or automation)
- Whether updates are automatic, gated for editorial approval, or manual
- What the throttle rate is, or how many data sources are connected
- Anything about the playout hardware, output format, or channel topology

All data routing, gating, throttling, and operator workflow decisions happen in the Go control plane, external to the template. The SDK is the abstraction boundary that keeps templates simple.

**Related documents:**
- `docs/engine.md` — playout engine architecture, rendering pipeline, compositing, hardware output
- `docs/data-plane.md` — data plane architecture, shared memory, feed connectors, operator controls, schema versioning

## Lifecycle Hooks

The engine calls these global functions on the template via CEF's `ExecuteJavaScript`. The template implements whichever hooks it needs.

```javascript
onLoad(model)    // Template loaded into a layer. Set up view model bindings.
                 // model = initial data snapshot (object with field values).
                 // Called on LOAD command, or on first PLAY if no prior LOAD.

onPlay()         // Animate IN. Called when the template transitions to on-air.
                 // Must call pivox.ready() when the in-animation completes
                 // and the graphic is fully visible.
                 // No data argument — bindings are already live from onLoad.

onStop()         // Animate OUT. Called when the template is taken off-air.
                 // Must call pivox.done() when the out-animation completes
                 // and the graphic is fully transparent.

onNext()         // Advance to the next state. For multi-page or multi-step
                 // graphics (e.g., cycling through candidates, pages of stats).
```

There is **no `onUpdate()` hook**. Data changes are handled reactively through the view model binding system. Templates that need custom animation or logic when data changes use `pivox.model.watch()`.

## View Model — `pivox.model`

The view model is the core of the SDK. Templates declaratively bind fields to DOM elements. When data changes — from any source — the SDK patches the view model and automatically updates all bound elements and watchers.

### Binding — Declarative, DOM-Level

```javascript
// Bind field to element's text content
pivox.model.bind('candidate_a_votes', document.getElementById('votes-a'));

// Bind with formatter (transform the value before display)
pivox.model.bind('candidate_a_votes', document.getElementById('votes-a'), {
  format: (value) => value.toLocaleString()
});

// Bind to a DOM attribute (e.g., style for progress bars)
pivox.model.bind('candidate_a_pct', document.getElementById('bar-a'), {
  attribute: 'style.width',
  format: (value) => value + '%'
});

// Bind to CSS class (add class when truthy, remove when falsy)
pivox.model.bind('is_winner', document.getElementById('winner-banner'), {
  className: 'called'
});

// Bind to visibility (show element when truthy, hide when falsy)
pivox.model.bind('breaking_alert', document.getElementById('alert-bar'), {
  visible: true
});
```

### Watching — For Custom Animation Logic

```javascript
// Watch a single field — callback receives new and old values
pivox.model.watch('candidate_a_votes', (newValue, oldValue) => {
  animateCounter(element, oldValue, newValue, 500);
});

// Watch multiple fields — callback fires when any dependency changes
pivox.model.watchAll(['candidate_a_votes', 'candidate_b_votes'], (model) => {
  updateChart(model.candidate_a_votes, model.candidate_b_votes);
});
```

### Read — Imperative Access

```javascript
// Read current value of a single field
const votes = pivox.model.get('candidate_a_votes');

// Get entire model as a plain object snapshot
const snapshot = pivox.model.snapshot();
```

## Data Feeds — `pivox.feeds`

For high-frequency live data (scores, clocks, tickers, telemetry), templates subscribe to shared memory feeds via `pivox.feeds`. This path bypasses gRPC entirely — the engine reads shared memory directly with sub-microsecond latency.

See `docs/data-plane.md` for the shared memory architecture, two-layer throttling, feed connectors, and operator controls.

### Subscribe

```javascript
// Subscribe to an entire feed
pivox.feeds.subscribe('scores', {
  maxUpdatesPerSec: 10,
  onUpdate: (data) => {
    // data = full feed snapshot as an object
  }
});

// Subscribe to specific fields with wildcards
pivox.feeds.subscribe('match', {
  fields: ['clock.*'],           // matches clock.elapsed, clock.period, clock.stoppage
  maxUpdatesPerSec: 60,
  onUpdate: (data) => {
    // data = { clock: { elapsed: "43:21", period: "2nd", stoppage: "00:00" } }
  }
});

// Subscribe with schema version (validated at load time, not runtime)
pivox.feeds.subscribe('scores', {
  schema: 'pivox.sports.nfl.v1',
  fields: ['home.score', 'away.score'],
  maxUpdatesPerSec: 10,
  onUpdate: (data) => {
    // data = { home: { score: 3 }, away: { score: 2 } }
  }
});
```

The `maxUpdatesPerSec` parameter is the **read throttle** — it controls how often the SDK fires the `onUpdate` callback to the template. This is independent of the write throttle (how often the Data Plane writes to shared memory), which is operator-controlled. A complex visualization might only want 5 updates/sec even if the data changes 60x/sec.

### Read — One-Shot

```javascript
// Read all fields from a feed
const allData = pivox.feeds.read('match');

// Read a single field
const elapsed = pivox.feeds.read('match', 'clock.elapsed');
// Returns: "43:21"

// Read specific fields
const data = pivox.feeds.read('match', ['clock', 'period']);
```

### Discovery and Cleanup

```javascript
// List all available feeds
const feeds = pivox.feeds.list();

// List fields in a feed
const fields = pivox.feeds.fields('match');

// Unsubscribe from a feed
pivox.feeds.unsubscribe('match');
```

## Services — `pivox`

### Lifecycle Signals

```javascript
pivox.ready()    // Signal: in-animation complete, graphic is on-air.
                 // Must be called from onPlay() when animation finishes.

pivox.done()     // Signal: out-animation complete, safe to remove.
                 // Must be called from onStop() when animation finishes.
```

### Asset Resolution

```javascript
// Convert asset ID to a servable URL.
// In the engine: resolves to pivox-asset:// protocol (local asset cache).
// In the browser mock: resolves to local dev server paths.
const url = pivox.assets.resolve('logo-cnn-hd');

// Warm the asset cache before going on-air (preload into memory)
pivox.assets.preload(['logo-cnn-hd', 'bg-election-night', 'font-custom']);
```

### Channel Information

```javascript
// Returns: { width: 1920, height: 1080, fps: 59.94, channelId: 'ch1' }
const info = pivox.channel.info();

// Returns title-safe and action-safe boundary rectangles
const safe = pivox.channel.safeArea();
```

### Logging

```javascript
// Structured logging back to the engine (appears in engine logs, not browser console)
pivox.log.info('Animation phase 2 started');
pivox.log.error('Missing required field: candidate_name');
```

### Timing

```javascript
// Frame-accurate callback synced to genlock (not browser rAF).
// Fires once per output frame at the house frame rate (e.g., 59.94 Hz).
pivox.timing.requestFrame((frameInfo) => {
  // called every output frame
});

// Milliseconds since onPlay() was called
const elapsed = pivox.timing.duration();
```

## Native Bindings — `pivox.native`

Some operations are impossible or unacceptably slow in JavaScript. The SDK exposes native functions implemented in Rust, callable directly from JS through CEF's V8 binding mechanism.

**Call path:**

```
Template JS                C++ (thin)              Rust (implementation)
─────────────────────────────────────────────────────────────────────
pivox.native.getTimecode()
       │
       ▼
  CefV8Handler::Execute()
       │
       ▼ (FFI call)
                                              engine_get_timecode()
                                              → reads AJA card timecode
                                              → returns SMPTE timecode
       ◄────────────────────────────────────
       │
       ▼
  returns {h:1, m:23, s:45, f:12}
```

JS to V8 to C++ handler (one-line pass-through) to Rust via FFI. Sub-microsecond overhead — no serialization, no IPC.

### Frame Timing and Synchronization

```javascript
pivox.native.getFrameNumber()       // absolute output frame counter
pivox.native.getTimecode()          // current SMPTE timecode from AJA card
pivox.native.getGenlockPhase()      // phase relative to house sync reference
pivox.native.getFrameTimestamp()    // high-resolution timestamp of current frame
```

### Hardware State

```javascript
pivox.native.getOutputStatus()      // AJA card state: signal present, format, genlock locked
pivox.native.getChannelConfig()     // resolution, frame rate, color space, output mode
```

### Audio Analysis

```javascript
pivox.native.getAudioLevels()                  // mixed output levels (all layers)
pivox.native.getAudioLevels({ layer: 0 })      // levels for a specific layer
pivox.native.getAudioLevels({ layers: [0, 3] }) // levels for multiple layers
```

### GPU-Accelerated Operations

For advanced templates that need image processing beyond what CSS/WebGPU provides. Async — runs on GPU via Rust compute shaders, returns a Promise.

```javascript
const blurred = await pivox.native.gpuBlur(imageData, radius);
const transformed = await pivox.native.gpuColorTransform(imageData, matrix);
```

## System Data Sources — `pivox.system`

The engine provides system-level data sources that are always present in every template's view model. They are updated automatically every frame. No Data Plane configuration or feed subscription is needed.

```javascript
// Time — synced to house reference (NTP or genlock timecode)
pivox.system.time.hours          // 0-23
pivox.system.time.minutes        // 0-59
pivox.system.time.seconds        // 0-59
pivox.system.time.frames         // 0-N (frame within current second)
pivox.system.time.display        // pre-formatted: "14:32:05"
pivox.system.time.iso            // ISO 8601: "2026-03-19T14:32:05.200Z"

// SMPTE timecode — from genlock / AJA card
pivox.system.timecode.hours
pivox.system.timecode.minutes
pivox.system.timecode.seconds
pivox.system.timecode.frames
pivox.system.timecode.display    // "01:23:45:12"

// Channel info
pivox.system.channel.id
pivox.system.channel.mode        // "on-air", "preview", "edit", "debug"
pivox.system.channel.resolution  // "1920x1080"
pivox.system.channel.frameRate   // 59.94

// Layer info
pivox.system.layer.id
pivox.system.layer.status        // "playing", "loaded", etc.
```

System data sources can be bound to elements like any other view model field:

```javascript
pivox.model.bind('pivox.system.time.display', document.getElementById('clock'));
```

**Playout UI context (TBD):** The Go control plane will inject additional context into the view model for templates that need playout-level information — rundown position, show name, segment info, operator-defined metadata. The specific fields are to be defined as the control plane architecture is designed.

## When to Use Which Namespace

| Namespace | Data Type | Delivery | Latency | Use Case |
|---|---|---|---|---|
| `pivox.model` | Operator-controlled fields, editorial data | Push — SDK patches model, bindings fire automatically | ~0.3-1ms | Lower thirds, election boards, name straps — anything the operator controls or approves |
| `pivox.feeds` | High-frequency live data | Subscribe — SDK reads shared memory per frame, fires callback at requested rate | ~0.001ms | Sports clocks, stock tickers, telemetry, real-time visualizations |
| `pivox.native` | Hardware state, frame timing, audio, GPU | Direct native call — JS to V8 to Rust FFI | Sub-microsecond | Timecode sync, audio VU meters, genlock phase, GPU image processing |
| `pivox.system` | System time, timecode, channel/layer info | Always available, updated every frame | Per-frame | Clocks, channel idents, debug overlays |
| Regular JS | DOM manipulation, CSS animations, general logic | V8 execution | N/A | Animation, layout, any template-internal logic |

## Timing Model

`pivox.timing.requestFrame()` is synced to genlock, not the browser's `requestAnimationFrame`. CEF runs in off-screen rendering (OSR) mode, which allows the engine to control frame timing — the engine ticks the browser at exactly the house frame rate (e.g., 59.94 Hz). Every `requestFrame` callback fires once per output frame, and every frame is captured. No dropped frames.

The `pivox.timing` functions are convenience wrappers around `pivox.native` timing calls, providing a simpler API for common animation needs. For frame-precise synchronization (e.g., syncing animation to timecode), use `pivox.native.getTimecode()` and `pivox.native.getFrameNumber()` directly.

```javascript
// Convenience — good for most animations
pivox.timing.requestFrame(() => {
  const t = pivox.timing.duration() / 1000;
  element.style.opacity = Math.min(t / 0.5, 1);
});

// Frame-precise — for timecode-locked sync
pivox.timing.requestFrame(() => {
  const tc = pivox.native.getTimecode();
  const frame = pivox.native.getFrameNumber();
  // sync animation to absolute timecode
});
```

## UpdateCommand Flow

When the engine receives an `UpdateCommand` from the Go control plane (whether triggered by an operator, an automated feed, or an approved gate):

```
1. Engine receives UpdateCommand via gRPC
       │
       ▼
2. Engine forwards data payload to CEF via ExecuteJavaScript
       │
       ▼
3. SDK patches the internal view model state
       │
       ▼
4. SDK triggers all bind() elements (DOM updates)
   and watch() callbacks (custom logic)
       │
       ▼
5. CEF renders the next frame with the updated content
```

The template never handles raw update commands. The SDK's view model layer is the abstraction boundary. The template does not know — and should not care — whether the update came from an operator typing in the UI, an automated election feed, or an approved gate.

## Browser Mock

Most template development happens in a standard browser, not inside the engine. The SDK ships a browser mock (`pivox-sdk-mock.js`) that simulates the full SDK API in Chrome, Firefox, and Safari.

| SDK Feature | In Engine (CEF) | In Browser Mock |
|---|---|---|
| `pivox.model.*` | Reactive bindings via SDK internals | Works identically — pure JS, no native code needed |
| `pivox.ready()` / `pivox.done()` | Signals to engine frame pipeline | Logged to console |
| `pivox.native.*` | Rust FFI calls (real hardware data) | Returns mock data (fake timecode, static hardware config) |
| `pivox.timing.requestFrame()` | Synced to genlock at house frame rate | Falls back to `requestAnimationFrame` |
| `pivox.assets.resolve()` | Resolves to `pivox-asset://` protocol | Returns local dev server paths |
| `pivox.feeds.*` | Reads shared memory, fires callbacks | Simulated with configurable mock data |
| `pivox.system.*` | Updated every frame from hardware | Returns browser clock and mock channel info |

**Development workflow:**

1. Include `pivox-sdk-mock.js` in the HTML during development
2. Open the template in Chrome — iterate on HTML, CSS, and JS with hot reload
3. Test in the engine for final validation (genlock timing, fill+key alpha, native bindings)
4. Remove the mock script for production — the real SDK is injected by CEF

The mock is published as an npm package (`@pivox/sdk-mock`) for template developers.

## Template Examples

### Election Results — Model Bindings and Watchers

A full-frame election results board with operator-controlled fields, live vote counts, and a gated race call.

```javascript
class ElectionResults {
  $(selector) { return document.querySelector(selector); }

  onLoad(model) {
    // Static fields — operator-controlled (manual mode)
    pivox.model.bind('race_name', this.$('#race-title'));
    pivox.model.bind('candidate_a_name', this.$('#name-a'));
    pivox.model.bind('candidate_b_name', this.$('#name-b'));

    // Live vote counts — auto mode from AP Elections feed
    pivox.model.bind('candidate_a_votes', this.$('#votes-a'), {
      format: v => v.toLocaleString()
    });
    pivox.model.bind('candidate_b_votes', this.$('#votes-b'), {
      format: v => v.toLocaleString()
    });

    // Progress bar width bound to percentage
    pivox.model.bind('candidate_a_pct', this.$('#bar-a'), {
      attribute: 'style.width',
      format: v => v + '%'
    });
    pivox.model.bind('candidate_b_pct', this.$('#bar-b'), {
      attribute: 'style.width',
      format: v => v + '%'
    });

    // Reporting percentage
    pivox.model.bind('reporting_pct', this.$('#reporting'), {
      format: v => v + '% reporting'
    });

    // Race call — gated mode, requires editorial approval
    pivox.model.watch('projected_winner', (winner, prev) => {
      if (winner && !prev) {
        this.playWinnerAnimation(winner);
      }
    });

    // Show/hide the "RACE CALLED" banner
    pivox.model.bind('projected_winner', this.$('#called-banner'), {
      visible: true
    });
    pivox.model.bind('projected_winner', this.$('#called-banner'), {
      className: 'called'
    });
  }

  onPlay() {
    this.$('#board').classList.add('animate-in');
    setTimeout(() => pivox.ready(), 800);
  }

  onStop() {
    this.$('#board').classList.add('animate-out');
    setTimeout(() => pivox.done(), 800);
  }

  playWinnerAnimation(winner) {
    this.$('#winner-name').textContent = winner;
    this.$('#winner-overlay').classList.add('reveal');
  }
}
```

### Sports Scoreboard — Field-Level Feed Subscriptions

A scoreboard that subscribes to different feed fields at different rates — the clock needs 60 updates/sec, but scores only need 10.

```javascript
class SportsScoreboard {
  $(selector) { return document.querySelector(selector); }

  onLoad(model) {
    // Team names and logos from view model (operator-set)
    pivox.model.bind('home_name', this.$('#home-name'));
    pivox.model.bind('away_name', this.$('#away-name'));
    pivox.model.bind('home_logo', this.$('#home-logo'), {
      attribute: 'src',
      format: id => pivox.assets.resolve(id)
    });
    pivox.model.bind('away_logo', this.$('#away-logo'), {
      attribute: 'src',
      format: id => pivox.assets.resolve(id)
    });

    // Clock — high frequency, every frame
    pivox.feeds.subscribe('match', {
      fields: ['clock.*'],
      maxUpdatesPerSec: 60,
      onUpdate: (data) => {
        this.$('#clock').textContent = data.clock.elapsed;
        this.$('#period').textContent = data.clock.period;
      }
    });

    // Scores — lower frequency, 10x/sec is plenty
    pivox.feeds.subscribe('match', {
      fields: ['home.score', 'away.score'],
      maxUpdatesPerSec: 10,
      onUpdate: (data) => {
        if (data.home) this.$('#home-score').textContent = data.home.score;
        if (data.away) this.$('#away-score').textContent = data.away.score;
      }
    });

    // Powerplay / penalty indicators — low frequency
    pivox.feeds.subscribe('match', {
      fields: ['home.penalties', 'away.penalties'],
      maxUpdatesPerSec: 2,
      onUpdate: (data) => {
        if (data.home) this.updatePenaltyIndicator('home', data.home.penalties);
        if (data.away) this.updatePenaltyIndicator('away', data.away.penalties);
      }
    });
  }

  onPlay() {
    this.$('#scoreboard').classList.add('slide-in');
    setTimeout(() => pivox.ready(), 600);
  }

  onStop() {
    this.$('#scoreboard').classList.add('slide-out');
    setTimeout(() => pivox.done(), 600);
    pivox.feeds.unsubscribe('match');
  }

  updatePenaltyIndicator(team, count) {
    const el = this.$(`#${team}-penalties`);
    el.textContent = count;
    el.classList.toggle('active', count > 0);
  }
}
```

### Stock Ticker — Full Feed Subscription with Model Bindings

A scrolling ticker that subscribes to the full financial feed and uses model bindings for the header.

```javascript
class StockTicker {
  $(selector) { return document.querySelector(selector); }

  onLoad(model) {
    // Header from view model (operator-controlled)
    pivox.model.bind('ticker_label', this.$('#ticker-label'));

    // Subscribe to the full ticker feed
    pivox.feeds.subscribe('market', {
      schema: 'pivox.financial.ticker.v1',
      maxUpdatesPerSec: 2,
      onUpdate: (data) => {
        this.updateTicker(data);
      }
    });
  }

  onPlay() {
    this.$('#ticker-bar').classList.add('slide-up');
    setTimeout(() => pivox.ready(), 400);
    this.startScroll();
  }

  onStop() {
    this.$('#ticker-bar').classList.add('slide-down');
    setTimeout(() => pivox.done(), 400);
    pivox.feeds.unsubscribe('market');
  }

  updateTicker(data) {
    const container = this.$('#ticker-items');
    container.innerHTML = '';
    for (const [symbol, quote] of Object.entries(data)) {
      const item = document.createElement('span');
      item.className = 'ticker-item';
      const change = quote.change >= 0 ? 'up' : 'down';
      item.innerHTML = `
        <span class="symbol">${symbol}</span>
        <span class="price">${quote.price.toFixed(2)}</span>
        <span class="change ${change}">${quote.change > 0 ? '+' : ''}${quote.change.toFixed(2)}</span>
      `;
      container.appendChild(item);
    }
  }

  startScroll() {
    const container = this.$('#ticker-items');
    let offset = 0;
    pivox.timing.requestFrame(() => {
      offset -= 2;
      if (offset < -container.scrollWidth) offset = container.parentElement.offsetWidth;
      container.style.transform = `translateX(${offset}px)`;
    });
  }
}
```

### Channel Ident with Clock — System Data Sources

A channel ident that uses built-in system data sources. No Data Plane configuration, no feed subscriptions — system data is always available.

```javascript
class ChannelIdent {
  $(selector) { return document.querySelector(selector); }

  onLoad(model) {
    // Station name from view model (operator-set)
    pivox.model.bind('station_name', this.$('#ident'));

    // Clock from system data — always available, updated every frame
    pivox.model.bind('pivox.system.time.display', this.$('#clock'));

    // Channel mode indicator (useful for preview vs on-air styling)
    pivox.model.watch('pivox.system.channel.mode', (mode) => {
      this.$('#ident-container').dataset.mode = mode;
    });
  }

  onPlay() {
    this.$('#ident-container').classList.add('fade-in');
    setTimeout(() => pivox.ready(), 1000);
  }

  onStop() {
    this.$('#ident-container').classList.add('fade-out');
    setTimeout(() => pivox.done(), 1000);
  }
}
```

### Audio Visualizer — Native Audio Levels + requestFrame

A real-time audio visualizer using native audio metering and genlock-synced rendering.

```javascript
class AudioVisualizer {
  $(selector) { return document.querySelector(selector); }

  onLoad(model) {
    this.canvas = this.$('#visualizer');
    this.ctx = this.canvas.getContext('2d');

    // Get channel dimensions for canvas sizing
    const info = pivox.channel.info();
    this.canvas.width = info.width;
    this.canvas.height = info.height;

    this.barCount = 16;
    this.levels = new Array(this.barCount).fill(0);
  }

  onPlay() {
    this.$('#viz-container').classList.add('fade-in');
    setTimeout(() => pivox.ready(), 300);
    this.startVisualization();
  }

  onStop() {
    this.running = false;
    this.$('#viz-container').classList.add('fade-out');
    setTimeout(() => pivox.done(), 500);
  }

  startVisualization() {
    this.running = true;

    const render = () => {
      if (!this.running) return;

      // Read audio levels from the native audio pipeline (not Web Audio API)
      const audio = pivox.native.getAudioLevels({ layer: 0 });

      // Get frame-precise timing for smooth decay
      const timestamp = pivox.native.getFrameTimestamp();

      this.drawBars(audio, timestamp);

      // Genlock-synced — fires exactly once per output frame
      pivox.timing.requestFrame(render);
    };

    pivox.timing.requestFrame(render);
  }

  drawBars(audio, timestamp) {
    const { width, height } = this.canvas;
    const barWidth = width / this.barCount;

    this.ctx.clearRect(0, 0, width, height);

    for (let i = 0; i < this.barCount; i++) {
      const target = audio.channels?.[i] ?? 0;

      // Smooth decay — peak hold with falloff
      this.levels[i] = Math.max(target, this.levels[i] * 0.92);

      const barHeight = this.levels[i] * height;
      const x = i * barWidth + 2;
      const y = height - barHeight;

      // Color gradient: green → yellow → red
      const hue = 120 - (this.levels[i] * 120);
      this.ctx.fillStyle = `hsl(${hue}, 80%, 50%)`;
      this.ctx.fillRect(x, y, barWidth - 4, barHeight);

      // Peak indicator
      this.ctx.fillStyle = '#ffffff';
      this.ctx.fillRect(x, y - 4, barWidth - 4, 2);
    }
  }
}
```
