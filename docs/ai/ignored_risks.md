# Ignored Risks — AI Chat Work

Running log of risks flagged during design and how they were handled. Updated as risks materialize or turn out to not apply. Tone is observational — this is a learning loop, not a blame doc.

## How this file is used

When a risk here materializes during implementation, the entry gets updated with what actually happened and gets called out in the working session. When we wrap a phase, we look back to calibrate future risk assessment.

## Columns

- **Risk** — short description of the concern
- **Context** — what feature/decision it applied to
- **Flagged** — approximate date
- **Decision** — dismissed / accepted-with-tradeoffs / implicitly-ignored
- **Status** — open / materialized / avoided / irrelevant
- **Notes** — outcome detail, lessons

## Log

| Risk | Context | Flagged | Decision | Status | Notes |
|---|---|---|---|---|---|
| Orphaned artifact versions when upstream asset version is deleted (artifact_version's `asset_version_name` is a plain string, not a real FK) | Artifact ↔ asset linkage | 2026-04 | accepted-with-tradeoffs | open | v1 has no code path creating asset-backed artifact versions, so the orphan scenario can't arise yet. Revisit when image generation or asset editing lands and asset-backed artifacts are actually produced. |
| Streaming write backpressure — slow client = gRPC bidi buffer growth | AiChat.Stream handler | 2026-04 | implicitly-ignored | open | Default grpc-go flow control accepted. No explicit buffer bound. If it bites, add a bounded channel + drop-oldest policy for text_delta events. |
| Message history token budget is approximate (len/4 heuristic, not per-model tokenizer) | Conversation history management | 2026-04 | accepted-with-tradeoffs | open | 25% safety buffer absorbs drift. Will bite if users have very long conversations where accuracy matters, or when billing/cost tracking comes online. Upgrade to real tokenizer then. |
| SSE handler self-dial lifecycle — upstream gRPC stream cleanup if SSE client disconnects mid-stream | SSE `POST /v1/ai:stream` | 2026-04 | accepted-with-tradeoffs | open | Relies on `r.Context().Done()` propagating to the gRPC `Stream` context. Need explicit test for the disconnect path. If context doesn't propagate cleanly, orphaned server goroutines pile up. |
| Auth interceptor only sets UID, not org membership. Every resource method does a DB lookup to resolve uid → (org, member) | Go BE service methods | 2026-04 | accepted-with-tradeoffs | open | Matches existing Pivox pattern. Not new for AiChat. If it becomes a perf concern, add a context-scoped cache. |
| qwen3-vl tool-call output format may not match Ollama's standard `tool_calls` schema exactly | OllamaAdapter | 2026-04 | implicitly-ignored | open | Spike at Phase 6 start. If drift exists, handle in the translation layer. Blocker only if the format differs structurally, not just field names. |
| JSONB `parts` column loses FK integrity for message parts that reference artifacts | messages table | 2026-04 | accepted-with-tradeoffs | open | Simpler than a shredded `message_parts` table. Integrity is enforced at application layer (stream handler validates artifact references before writing). Revisit if integrity bugs actually show up. |
| `deferred FK` on artifacts.latest_version_id needs `DEFERRABLE INITIALLY DEFERRED` for single-transaction insert | artifacts + artifact_versions schema | 2026-04 | accepted-with-tradeoffs | open | Explicit test required during Phase 2/3. If the migration doesn't apply cleanly, alternative is a two-transaction insert with a NULL latest_version_id on the first write. |
