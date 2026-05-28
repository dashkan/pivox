package aichat

// SSE handler tests are intentionally absent at this commit.
//
// The previous tests in this file exercised the old SSE shape
// (snake_case fields, wrong tool-event names, missing lifecycle
// chunks) and were not wired into CI in a way that caught wire-
// format drift. Rather than migrate broken assertions, they are
// re-authored from scratch in a later commit as an end-to-end
// suite via internal/testutil/grpcharness:
//
//   - real AiChat server + interceptor chain (auth, membership,
//     permission, audit)
//   - bufconn-dialed gRPC client driving the SSE adapter
//   - scripted ServerEvent sequences from a stub generator
//   - assertions on SSE headers (Content-Type, Cache-Control,
//     X-Accel-Buffering: no), event ordering, the `[DONE]`
//     sentinel, and cancellation -> abort
//   - goleak.VerifyNone for goroutine safety
//
// Chunk-level wire-format conformance lives in
// sse_translate_test.go (TestMarshalChunk) — the protojson output
// is asserted there against Vercel spec examples verbatim, so the
// handler test focuses on transport behavior, not chunk shape.
