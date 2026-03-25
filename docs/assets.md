# Pivox Asset Management

## Overview

The asset system manages media files (images, video, audio, documents) throughout their lifecycle — from upload and ingestion through versioning, search, and retention. Assets are project-scoped and stored on Storage Endpoints managed by Storage Gateways.

**Key design decisions:**

1. **Flat assets, not folders.** Assets have no folder hierarchy. Organization is handled entirely by tags (flexible, multi-dimensional). This avoids recursive permission checks, cascading renames, and move operations.

2. **Versions are ops chains, not duplicate blobs.** Upload versions store files. Edit versions store a crop operation + pointer to the source version. The gateway renders edits on demand from the source blob + ops chain. No duplicate storage for edits.

3. **Immutable storage keys.** Each version has a unique storage key: `assets/{id}/v{n}/original.ext`. Eliminates cache invalidation — the URL changes when the content changes.

4. **Tag-based auto-categorization.** The ingestion pipeline extracts metadata and generates tags automatically. Tags have an `origin` field (USER, SYSTEM, AI) so re-ingestion can replace auto-generated tags without touching user-applied tags.

5. **Metadata as a singleton sub-resource.** Full EXIF/XMP metadata is not included in Asset Get/List responses. Accessed via a dedicated `GetAssetMetadata` RPC to keep list views lightweight.

6. **Request workflow for asset orders.** Producers create Requests with LineItems. Each LineItem creates a PLACEHOLDER asset. Artists fulfill LineItems by uploading files. Producers approve or request revisions.

**Related documents:**
- `docs/storage.md` — Storage Gateways, S3 proxy, caching, session auth
- `docs/architecture.md` — system-level architecture

---

## Asset Resource

**Pattern:** `organizations/{org}/projects/{project}/assets/{asset}`
**Package:** `pivox.assets.v1`

### Lifecycle States

```
                    ┌──────────────┐
                    │  PLACEHOLDER │  Created by Request workflow
                    └──────┬───────┘  (no file content)
                           │
                    Upload / Fulfill
                           │
                    ┌──────▼───────┐
                    │  PROCESSING  │  Ingestion pipeline running
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
       ┌──────▼──┐  ┌──────▼──┐  ┌─────▼──────────┐
       │  ACTIVE │  │  FAILED │  │ DELETE_REQUESTED│
       └─────────┘  └─────────┘  └────────────────┘
                                   (30-day grace,
                                    then purged)
```

- **PLACEHOLDER** — Created by the Request workflow. No file content. Becomes PROCESSING when an artist fulfills the linked LineItem.
- **PROCESSING** — Ingestion pipeline is running (validation, metadata extraction, thumbnail generation, AI processing).
- **ACTIVE** — Fully ingested and available for serving.
- **FAILED** — An ingestion step failed. Check the latest version's `ingestion_error`.
- **DELETE_REQUESTED** — Soft-deleted. Restorable via UndeleteAsset until `purge_time` (30 days).

### Media Types

Derived from content type during ingestion. Used for UI grouping, icons, and pipeline routing.

| MediaType | Content Types | Notes |
|-----------|--------------|-------|
| IMAGE | image/jpeg, image/png, image/webp, image/tiff, image/gif, image/svg+xml, image/bmp | Includes vector (SVG) |
| VIDEO | video/mp4, video/quicktime, video/x-mxf, video/webm, video/x-matroska | |
| AUDIO | audio/wav, audio/mpeg, audio/aac, audio/flac, audio/ogg | |
| DOCUMENT | application/pdf | |

Unknown content types are rejected at upload.

### Key Fields

| Field | Type | Notes |
|-------|------|-------|
| `display_name` | string | Human-readable name |
| `content_type` | string | MIME type from magic bytes, not file extension |
| `filename` | string | Original uploaded filename |
| `media_type` | enum | Broad category (IMAGE, VIDEO, AUDIO, DOCUMENT) |
| `import_path` | string | Original filesystem path from ImportAssets (read-only) |
| `endpoint` | resource ref | Storage endpoint where files are stored |
| `checksum_sha256` | string | SHA-256 of original file, used for dedup |
| `size_bytes` | int64 | File size |
| `width`, `height` | int32 | Dimensions (image/video) |
| `duration` | Duration | Length (video/audio) |
| `latest_version` | AssetVersion | Embedded for convenience |
| `version_count` | int32 | Total version count |
| `expire_time` | Timestamp | Auto-delete after this time |
| `ttl` | Duration | INPUT_ONLY alternative to expire_time |

### Deduplication

Assets are deduped within a project by SHA-256 checksum. If a file with the same checksum already exists in the project, the existing asset is returned instead of creating a duplicate.

### Metadata

Full extracted metadata (EXIF, XMP, video codec info, etc.) is available via a singleton sub-resource:

```
GET /v1/organizations/{org}/projects/{project}/assets/{asset}/metadata
```

Returns an `AssetMetadata` message with a `google.protobuf.Struct` containing all extracted fields. Junk metadata (Adobe comments, software markers) is stripped during ingestion.

---

## Versioning

**Pattern:** `organizations/{org}/projects/{project}/assets/{asset}/versions/{version}`

Each version is either an **upload** (has a blob) or an **edit** (has a crop + pointer to source version).

### Version Types

| Mode | Has blob? | Fields set |
|------|-----------|------------|
| Upload | Yes | `storage_key`, `checksum_sha256`, `size_bytes`, `content_type` |
| Edit (crop) | No | `source_version`, `crop` |
| Revert | No | `source_version` (copies as new latest) |

### Version Chain

```
v1: upload (source blob: sunset-beach.mp4)
v2: crop(0,0,1920,1080) + straighten(5°) → source: v1
v3: upload (new file replaces source)
v4: rotation(90) + flip_horizontal → source: v3
v5: revert → source: v1
```

- Upload versions store files at `assets/{id}/v{n}/original.ext`
- Edit versions store no files — the gateway renders from source blob + ops
- Revert creates a new version number pointing to the old source
- The UI walks the chain to find the original upload and replays edits client-side

### Crop Operation

Combines crop area, rotation, straighten, and flip in a single operation (modeled after img.ly's unified crop tool):

```
Crop {
  CropArea area      — x, y, width (>0), height (>0) in original image pixels
  float straighten   — degrees (-45 to 45)
  bool flip_horizontal
  bool flip_vertical
  int32 rotation     — 90-degree increments (0, 90, 180, 270)
}
```

Applied order: crop → rotation → straighten → flip.

### Image Editor State Model

The web image editor (`@pivox/image-editor`) uses a **crop-as-viewport** model internally. The crop rect is a fixed window; the image transforms (scale, rotate, translate) behind it. This ensures zero dead pixels at any rotation angle.

**Editor state (what the UI tracks):**

| Field | Type | Description |
|-------|------|-------------|
| `cropWidth` | number | Crop viewport width in image pixels |
| `cropHeight` | number | Crop viewport height in image pixels |
| `rotation` | 0/90/180/270 | Quarter-turn rotation |
| `straighten` | number | Fine rotation adjustment (-45° to 45°) |
| `scale` | number | Image scale factor (≥ minScale for current rotation) |
| `tx` | number | Image X translation relative to crop center |
| `ty` | number | Image Y translation relative to crop center |
| `flipHorizontal` | boolean | Horizontal mirror |
| `flipVertical` | boolean | Vertical mirror |

**Key algorithm** — prevents dead pixels at any rotation angle:

```
minScale = max(
  (cropW × |cos θ| + cropH × |sin θ|) / imgW,
  (cropW × |sin θ| + cropH × |cos θ|) / imgH
)

boundaryX = (imgW × scale × |cos θ| + imgH × scale × |sin θ| − cropW) / 2
boundaryY = (imgW × scale × |sin θ| + imgH × scale × |cos θ| − cropH) / 2

tx = clamp(tx, −boundaryX, boundaryX)
ty = clamp(ty, −boundaryY, boundaryY)
```

### Converting Editor State → Proto (CropArea)

The proto expects `CropArea { x, y, width, height }` in original image pixel coordinates. The editor's viewport state (cropW/H, tx, ty, scale, angle) converts to image-space coordinates:

```
// Inverse-rotate the translation to image space
offsetX = (−tx / scale) × cos(−θ) − (−ty / scale) × sin(−θ)
offsetY = (−tx / scale) × sin(−θ) + (−ty / scale) × cos(−θ)

// Crop rect center in image space
centerX = imgW / 2 + offsetX
centerY = imgH / 2 + offsetY

// Crop rect dimensions in image space (unscaled)
w = cropW / scale
h = cropH / scale

CropArea { x: centerX − w/2, y: centerY − h/2, width: w, height: h }
```

Use `stateToImageCropRect()` from `@pivox/image-editor` for this conversion.

### Rendering with CSS (no Canvas)

The editor state maps directly to CSS transforms for displaying the crop result via an `<img>` tag:

```html
<div style="width: {cropWidth}px; height: {cropHeight}px; overflow: hidden;">
  <img
    src="..."
    style="transform-origin: center;
           transform: translate({tx}px, {ty}px)
                      rotate({rotation + straighten}deg)
                      scale({flipH * scale}, {flipV * scale});"
  />
</div>
```

The container clips. The image transforms. Same math as the Canvas renderer.

### Mutability

- Source blob can be replaced at any time via a new upload version
- All versions are immutable once created
- Version history provides full audit trail

---

## Ingestion Pipeline

When an asset is created with a file upload, the LRO runs the following pipeline:

### Implemented

| Step | Tool | Description |
|------|------|-------------|
| Format validation | `net/http.DetectContentType` | Magic bytes check, reject unknown types |
| Checksum | `crypto/sha256` | SHA-256 for dedup |
| Metadata extraction | `exiftool -json -n` | Full EXIF/XMP/IPTC extraction, junk stripped |
| Dimensions/duration | exiftool output | Promoted to Asset fields |

### Deferred (TODO)

| Step | Tool | Description |
|------|------|-------------|
| Thumbnail generation | `disintegration/imaging` (image), ffmpeg (video) | Small, medium, large + poster frame + animated preview |
| Proxy generation | ffmpeg | H.264 720p for web playback, MP3 preview for audio |
| AI description | Ollama (dev) / GenAI API (prod) | Visual/audio description → pgvector embedding |
| Transcription | AI provider | Audio/video speech-to-text → full-text search index |
| Auto-tagging | AI tool calling | Suggests tags from existing taxonomy, origin=AI |

### LRO Metadata

The CreateAsset LRO reports pipeline progress via `CreateAssetMetadata`:

```
Steps: AWAITING_UPLOAD → VALIDATING → DEDUPLICATING →
       EXTRACTING_METADATA → GENERATING_THUMBNAILS →
       GENERATING_PROXY → AI_PROCESSING → TRANSCRIBING →
       AUTO_TAGGING → FINALIZING
```

Each step includes a progress percentage (0-100).

---

## Request Workflow

**Pattern:** `organizations/{org}/projects/{project}/requests/{request}`

Requests represent asset orders. A producer creates a request, an artist fulfills it, the producer approves.

### State Machine

```
DRAFT → OPEN → IN_PROGRESS → DELIVERED → APPROVED
                    ↑              │
                    │              ▼
                    └──── REVISION_REQUESTED

Any state → CANCELLED (except APPROVED)
DELIVERED → REJECTED
```

### Line Items

**Pattern:** `...requests/{request}/lineItems/{line_item}`

Each request has one or more line items. Each line item creates a PLACEHOLDER asset in the project.

```
Request: "Q4 Social Media Graphics"
├── LineItem: "Facebook banner" → Asset (PLACEHOLDER)
├── LineItem: "Instagram story" → Asset (PLACEHOLDER)
└── LineItem: "Twitter header"  → Asset (PLACEHOLDER)
```

### Assignment

- **Manual assign** — producer assigns a specific artist (requires `assets.requests.assign` permission)
- **Self-claim** — artist claims an OPEN request from the queue (requires `assets.requests.claim` permission)
- AI-assisted assignment planned for later

### Workflow RPCs

| RPC | Transition | Who |
|-----|-----------|-----|
| SubmitRequest | DRAFT → OPEN | Producer |
| AssignRequest | OPEN/IN_PROGRESS → IN_PROGRESS (set assignee) | Producer/Admin |
| ClaimRequest | OPEN → IN_PROGRESS (self-assign) | Artist |
| DeliverRequest | IN_PROGRESS → DELIVERED | Artist |
| ApproveRequest | DELIVERED → APPROVED | Producer |
| RequestRevision | DELIVERED → REVISION_REQUESTED → IN_PROGRESS | Producer |
| RejectRequest | DELIVERED → REJECTED | Producer |
| CancelRequest | Any → CANCELLED | Producer |
| FulfillLineItem | Uploads file for PLACEHOLDER asset | Artist |

---

## Tagging

Assets use the platform's existing tag system (TagKeys, TagValues, TagBindings). Tag bindings on assets support three origins:

| Origin | Set by | Cleared on re-ingestion |
|--------|--------|------------------------|
| USER | Manual by user | No |
| SYSTEM | Metadata extraction (EXIF camera, GPS location, etc.) | Yes |
| AI | AI auto-tagging | Yes |

When an asset's source blob is replaced, the ingestion pipeline deletes all SYSTEM and AI origin bindings and regenerates them. USER bindings are untouched.

---

## Search

Assets are searchable via PostgreSQL full-text search + pgvector semantic search. No Elasticsearch.

### Full-Text Search

A generated `tsvector` column combines:
- **Weight A:** `display_name`
- **Weight B:** `ai_description` (when implemented)
- **Weight C:** `transcription` (when implemented)

### Semantic Search (TODO)

pgvector with 768-dimensional embeddings (nomic-embed-text for dev, production provider TBD). Hybrid search combines `ts_rank` + vector distance in a single CTE.

### Filtering

AIP-160 filter expressions on: `state`, `mediaType`, `contentType`, `displayName`, `creator`, `createTime`, `expireTime`.

---

## Retention

- `expire_time` on Asset — background reaper soft-deletes expired assets
- `ttl` as INPUT_ONLY convenience (server computes `expire_time = create_time + ttl`)
- Tag-based retention policies planned as a future resource (e.g. "assets tagged `type:daily-news` expire after 7 days")
- Soft-deleted assets are purged (blobs deleted from storage) after 30-day grace period

---

## Denied Patterns

When an asset or project is soft-deleted, the control plane pushes denied access patterns to Storage Gateway agents via bidi ConfigUpdate. Agents store patterns in local SQLite and check every incoming request.

```
projects/proj1/*              — entire project deleted
projects/proj1/assets/abc/*   — single asset deleted
```

Patterns are full-replacement on each push. Agents load from SQLite on reboot, sync full state from control plane on bidi reconnect. See `docs/storage.md` for details.

---

## Rendition Generation

Renditions (thumbnails, proxies, previews) are generated **on agents**, not the server. The server never touches files.

### Flow

1. User uploads file to agent (presigned URL or gateway PUT)
2. Server sends `GenerateRenditions` command to agent via bidi
3. Agent generates renditions locally (has direct storage access)
4. Agent writes renditions to storage: `assets/{id}/v{n}/thumb_sm.jpg`, etc.
5. Agent reports `RenditionsComplete` with rendition metadata
6. Server updates `asset_renditions` table

### Agent Roles

Agents support two roles, set during Handshake:

| Role | HTTP serving | Rendition tasks | In DNS | Use case |
|------|-------------|----------------|--------|----------|
| BOTH | Yes | Yes | Yes | Default — single agent does everything |
| SERVE | Yes | No | Yes | Dedicated request serving |
| WORKER | No | Yes | No | Dedicated rendering (cheaper hardware, burst capacity) |

Default: BOTH. Split into SERVE + WORKER when rendering load impacts serving performance.

### Resource Usage

Rendition generation (ffmpeg transcoding, image processing) is CPU-intensive. WORKER agents can be:
- Additional servers in the pool dedicated to processing
- Spun up on demand for bulk imports
- Lower-spec hardware (no SSD needed, just CPU)

---

## File Serving

Assets are served through Storage Gateways with:

- **Cookie-based session auth** for reads (stable URLs, CDN-cacheable)
- **Presigned URLs** for uploads (S3 endpoints) or gateway PUT (filesystem endpoints)
- **In-memory LRU cache** for hot/small assets (≤10MB, 256MB total)
- **HTTP caching headers** — `Cache-Control: public, max-age=31536000, immutable` (versioned storage keys)
- **Range requests** — video seeking, progressive image loading
- **Conditional requests** — ETag-based 304 Not Modified

### Storage Key Format

```
assets/{asset_id}/v{version_number}/original.ext
assets/{asset_id}/v{version_number}/thumb_sm.jpg
assets/{asset_id}/v{version_number}/thumb_md.jpg
assets/{asset_id}/v{version_number}/thumb_lg.jpg
assets/{asset_id}/v{version_number}/proxy.mp4
assets/{asset_id}/v{version_number}/preview.webp
```

---

## Import

`ImportAssets` RPC scans a storage endpoint path, discovers files, and runs the ingestion pipeline for each. Useful for onboarding existing media libraries.

Options:
- `path_prefix` — scan a subdirectory instead of the whole endpoint
- `preserve_paths` — generate tags from the filesystem directory structure
- `auto_tag` — run AI auto-tagging on imported assets (default: true)

LRO metadata tracks: phase (SCANNING → INGESTING → DONE), total/processed/imported/skipped/failed file counts.
