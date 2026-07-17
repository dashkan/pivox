package aichat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/aichat/model"
)

const defaultModelContextBudget = 22500
const defaultMaxHistoryRows = 500

// Conversation-lease parameters. The lease prevents two concurrent
// streams against the same conversation (cross-tab, cross-session)
// and stops mid-stream Delete/Update from racing with assistant
// persistence. See ai_conversations.sql for the acquire/heartbeat/
// release queries.
//
// Progress is measured in BYTES RECEIVED from the upstream model
// (events arriving from `s.model.Stream`), not SSE-output liveness.
// A stalled upstream that keeps the TCP socket alive doesn't count
// as progress; only actual model events do.
//
// Cascade:
//
//   - `streamStallAbortThreshold` (60s): no upstream event for this
//     long → heartbeat cancels the stream context and returns. This
//     is the deterministic "stuck stream" detector. Generous enough
//     to cover reasoning-model first-token latency and short tool
//     round-trips.
//   - `leaseStaleExtensionThreshold` (30s): no upstream event for
//     this long → heartbeat stops issuing extension UPDATEs. The
//     lease then expires naturally as defense-in-depth: even if the
//     active abort logic above somehow doesn't fire, the lease won't
//     hold the conversation past its TTL.
//   - `leaseHeartbeatInterval` (10s): how often the heartbeat
//     evaluates progress. TTL/3 by convention.
//   - SQL-side TTL (30s in ai_conversations.sql): pure process-crash
//     recovery SLO. Equal to staleExtensionThreshold so a fully
//     missed extension cycle expires the lease promptly.
const (
	leaseHeartbeatInterval       = 10 * time.Second
	leaseStaleExtensionThreshold = 30 * time.Second
	streamStallAbortThreshold    = 60 * time.Second
	leaseReleaseTimeout          = 5 * time.Second
)

// StreamGenerateContent is the server-streaming variant of
// `GenerateContent`. Same request shape; emits the response as a
// sequence of `ServerEvent`s.
//
// Stateful by default: if the caller doesn't supply a `conversation`,
// the server creates one and emits its resource name in the first
// `Start` chunk's `messageMetadata.conversation`. The client captures
// the name and threads it through subsequent turns so the server
// loads history from its own DB instead of trusting client-supplied
// history. Stateless one-shots (title summarization, intent
// classification) go through unary GenerateContent instead, which
// preserves the explicit `conversation`-empty contract.
func (s *Server) StreamGenerateContent(req *aiv1.GenerateContentRequest, stream grpc.ServerStreamingServer[aiv1.ServerEvent]) error {
	ctx := stream.Context()

	// autoCreated is true only when this call minted the conversation
	// (empty `conversation`) — it tells runGenerate to skip its
	// inbound-persist step, since createConversationWithFirstMessage already
	// persisted the first message atomically with the conversation.
	autoCreated := false
	if req.GetConversation() == "" {
		convName, err := s.createConversationWithFirstMessage(ctx, req.GetParent(), lastInboundMessage(req))
		if err != nil {
			return err
		}
		req.Conversation = convName
		autoCreated = true
	}

	// A generation failure leaves the conversation persisted with the user's
	// first message and no assistant reply — the same state as a failed turn
	// on a pre-existing conversation. The user's message stays put; the client
	// surfaces the error and retries in place. Nothing is discarded.
	_, _, _, err := s.runGenerate(ctx, req, autoCreated, func(ev *aiv1.ServerEvent) error {
		return stream.Send(ev)
	})
	if err != nil {
		return err
	}
	// Emit `finish` as the terminal lifecycle event. `finishReason`
	// is "stop" for the normal-completion path; tool-loop and
	// length-cap variants will set their own reasons once the upstream
	// model layer surfaces them.
	return stream.Send(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_Finish{Finish: &aiv1.Finish{FinishReason: "stop"}},
	})
}

// createConversationWithFirstMessage mints a fresh Conversation under
// the caller's user-in-org parent AND persists its first (user) message
// in the SAME transaction, returning the conversation's resource name.
// This is the whole point of the lifecycle rework: a conversation is
// never committed empty — it and its first message land atomically, so a
// failure anywhere in the tx leaves no row at all (no orphaned empty
// conversation, which the old two-tx design produced when generation
// failed between committing the empty conversation and persisting the
// first message).
//
// Once committed, the conversation is durable regardless of what the
// subsequent generation does. A generation failure leaves it persisted
// with just the user's first message and no assistant reply — the same
// state as a failed turn on a pre-existing conversation. The user's
// message stays put; the client shows the error and retries in place.
// Nothing reaps it.
//
// The permission interceptor (registered via the proto's
// `pivox.permission.v1.required_permission = "ai.chat.stream"` option;
// see internal/server/generated_registry.go) has already gated on the
// parent, so by the time this runs the caller is authorized to create
// conversations under this user.
//
// Org resolution + conversation insert + first-message insert run inside
// a single transaction per CLAUDE.md's tx rule — all touch `qtx`, and the
// FK from `ai_conversations.org_id` to `organizations.id` would surface a
// TOCTOU delete-then-insert as a 23503 mapped to NotFound rather than the
// typed `apierr.HandleResourceError` for Organization the closure produces
// explicitly. The closure is DB-only; message-part marshaling (the one
// pure, replay-unsafe-to-repeat-for-effect step) happens outside via
// buildInputMessageParams so its role/marshal errors surface before the tx
// opens.
func (s *Server) createConversationWithFirstMessage(ctx context.Context, parent string, firstMsg *aiv1.InputMessage) (string, error) {
	orgName, pathUser, err := parseConversationParent(parent)
	if err != nil {
		return "", apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}
	callerUserID := server.MustUserID(ctx)
	if pathUser != callerUserID {
		return "", apierr.PermissionDenied("conversations may only be created under the caller's own user-uuid")
	}
	if firstMsg == nil {
		// Unreachable under the validator chain (messages.min_items=1); a
		// loud Internal beats minting an empty conversation.
		return "", apierr.Internal(nil, "invariant: create conversation called with no first message")
	}
	// ConversationID is filled in inside the tx once CreateConversation
	// returns the new row's id (persistMessageOnQtx then computes Sequence).
	msgParams, err := buildInputMessageParams(uuid.Nil, firstMsg)
	if err != nil {
		return "", err
	}
	row, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (db.AiConversation, error) {
		org, err := qtx.GetOrganizationByName(ctx, orgName)
		if err != nil {
			return db.AiConversation{}, apierr.HandleResourceError(err, "Organization", fmt.Sprintf("organizations/%s", orgName))
		}
		conv, err := qtx.CreateConversation(ctx, db.CreateConversationParams{
			OrgID:     org.ID,
			Name:      uuid.New().String()[:12],
			CreatedBy: callerUserID,
		})
		if err != nil {
			return db.AiConversation{}, apierr.HandleResourceError(err, "Conversation", "")
		}
		// The conversation row is brand-new and uncommitted, so no other tx
		// can see or persist against it — persistMessageOnQtx's usual
		// FOR-UPDATE-lock precondition is trivially satisfied (there is no
		// concurrent persist to serialize against). Sequence resolves to 1.
		msgParams.ConversationID = conv.ID
		if err := persistMessageOnQtx(ctx, qtx, conv.ID, msgParams); err != nil {
			return db.AiConversation{}, err
		}
		return conv, nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("organizations/%s/users/%s/conversations/%s", orgName, pathUser, row.Name), nil
}

// lastInboundMessage returns the last message in the request — the turn
// being submitted. useChat resends the whole UI history each turn, but the
// Pivox transport strips it once `conversation` is set, so on the
// auto-create path (empty `conversation`) this is the single first message.
// Returns nil only when messages is empty (unreachable under the
// min_items=1 validator); createConversationWithFirstMessage rejects nil.
func lastInboundMessage(req *aiv1.GenerateContentRequest) *aiv1.InputMessage {
	msgs := req.GetMessages()
	if len(msgs) == 0 {
		return nil
	}
	return msgs[len(msgs)-1]
}

// GenerateContent is the unary counterpart to `StreamGenerateContent`.
// Runs the same generation flow but accumulates the response into a
// single `Message` and returns it. Always stateful: like
// `StreamGenerateContent`, an empty `conversation` triggers
// auto-create so a chat-shaped consumer doesn't have to choreograph
// a `CreateConversation` + `GenerateContent` pair.
//
// Stateless internal one-shots (title summarization, intent
// classification) should call `s.model.Stream` directly — see
// `summarizeTranscript` for the pattern — rather than go through
// this RPC and discard the auto-created row.
func (s *Server) GenerateContent(ctx context.Context, req *aiv1.GenerateContentRequest) (*aiv1.GenerateContentResponse, error) {
	autoCreated := false
	if req.GetConversation() == "" {
		convName, err := s.createConversationWithFirstMessage(ctx, req.GetParent(), lastInboundMessage(req))
		if err != nil {
			return nil, err
		}
		req.Conversation = convName
		autoCreated = true
	}
	// On failure the conversation is left persisted with the user's first
	// message and no assistant reply — symmetric with a failed turn on a
	// pre-existing conversation. Nothing is discarded.
	msg, usage, modelName, err := s.runGenerate(ctx, req, autoCreated, nil)
	if err != nil {
		return nil, err
	}
	return &aiv1.GenerateContentResponse{
		Message: msg,
		Usage:   usage,
		Model:   modelName,
	}, nil
}

// runGenerate is the shared core for `StreamGenerateContent` and
// `GenerateContent`. The `emit` callback, when non-nil, is invoked
// for each `ServerEvent` produced during generation; pass nil to
// suppress event emission (the unary path collects the assistant
// text into the returned `Message` directly).
//
// `req.GetConversation()` is always non-empty by the time this runs
// — both callers auto-create via `createConversationWithFirstMessage`
// when the client doesn't supply one. Stateless internal one-shots
// (title summarization, etc.) call `s.model.Stream` directly via
// `summarizeTranscript` rather than enter this path.
//
// `skipInboundPersist` is true on the auto-create path: the first
// user message was already persisted atomically with the conversation
// in `createConversationWithFirstMessage`, so Tx A is skipped to avoid
// double-persisting it. On the resume path (existing conversation) it
// is false and Tx A persists the inbound turn as before.
//
// Flow:
//
//  1. Validate the request and resolve org/conversation context.
//  2. Acquire the per-conversation lease (Postgres advisory-ish
//     UPDATE; sliding TTL extended by a heartbeat goroutine until
//     the stream finishes).
//  3. Persist the inbound user/tool turn (Tx A; skipped when the
//     caller already persisted it during auto-create) and load the
//     full prior transcript.
//  4. Call the language model with the assembled context.
//  5. Stream the response: emit events via `emit` (when set) and
//     accumulate text into the returned `Message`.
//  6. Persist the assistant response (lease-guarded inside Tx B).
//
// Returns the assistant `Message` (with name set after persist),
// token usage, and the model identifier.
func (s *Server) runGenerate(
	ctx context.Context,
	req *aiv1.GenerateContentRequest,
	skipInboundPersist bool,
	emit func(*aiv1.ServerEvent) error,
) (*aiv1.Message, *aiv1.TokenUsage, string, error) {
	// Field-shape validation (parent non-empty, messages.min_items=1,
	// InputMessage.role not in {ASSISTANT, SYSTEM}, tool-role has a
	// tool_result with tool_call_id) is enforced by the protovalidate
	// interceptor — by the time this runs, the request is well-formed.
	// AuthInterceptor + MembershipInterceptor + PermissionInterceptor
	// have all gated on identity by this point; no need for a
	// belt-and-suspenders assertion here.

	// Validate the parent org is well-formed and exists. Cross-org
	// tenancy itself is enforced upstream by the permission
	// interceptor checking `ai.chat.stream` against this org —
	// `parseOrgScope` only does syntactic extraction and `resolveOrg`
	// only verifies the org row exists, neither of which gates on
	// caller membership. parseOrgScope accepts any path that starts
	// with `organizations/{org}/...` so this same parent shape works
	// for both Phase-7 user-rooted paths and the bare org parent.
	orgName, err := parseOrgScope(req.GetParent())
	if err != nil {
		return nil, nil, "", apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}
	if _, err := s.resolveOrg(ctx, orgName); err != nil {
		return nil, nil, "", err
	}

	// Conversation is always non-empty by the time runGenerate runs —
	// both GenerateContent and StreamGenerateContent auto-create via
	// createConversationWithFirstMessage when the caller doesn't supply
	// one. Stateless one-shots bypass this path entirely (they call
	// s.model.Stream directly; see summarizeTranscript).
	convOrgName, convPathUser, convName, err := parseConversationName(req.GetConversation())
	if err != nil {
		return nil, nil, "", apierr.InvalidArgument(apierr.FieldViolation("conversation", err.Error()))
	}
	if convOrgName != orgName {
		return nil, nil, "", apierr.BadRequest("conversation's organization does not match request parent")
	}
	// Generation is creator-only — no `*All` bypass. An admin auditing
	// another user's chats does not generate new turns on their behalf.
	convRow, err := s.resolveConversation(ctx, convOrgName, convPathUser, convName, "")
	if err != nil {
		return nil, nil, "", err
	}
	conv := &convRow

	// Acquire the conversation lease. Until this call returns
	// successfully no other stream can submit a turn against this
	// conversation, and Delete/Update on this conversation will be
	// rejected with FailedPrecondition. Concurrent acquire from
	// another tab returns 0 rows (pgx.ErrNoRows) — surface as
	// Aborted, mapped to "conflict, please retry" on the SSE wire.
	sessionUID := uuid.New()
	if _, err := s.queries.AcquireConversationLease(ctx, db.AcquireConversationLeaseParams{
		ID:         conv.ID,
		LockHolder: convert.PgUUID(sessionUID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, "", apierr.Aborted("Conversation", req.GetConversation(), "ACTIVE_STREAM")
		}
		slog.ErrorContext(ctx, "acquire lease failed", "conversation_id", conv.ID, "error", err)
		return nil, nil, "", apierr.Internal(err, "acquire conversation lease")
	}

	// streamCtx is a child of ctx that the heartbeat goroutine can
	// cancel independently — that's how a stalled upstream (>60s no
	// bytes) aborts the stream from outside the model.Stream pump.
	// All subsequent calls into the model layer use streamCtx, not
	// ctx, so heartbeat-triggered cancellation reaches them.
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	// lastEventNanos tracks the wall-clock time of the most recent
	// upstream event from `s.model.Stream` (text deltas, tool calls,
	// finish events — any event that proves the model is still
	// producing). Stored as Unix nanos in an atomic so the heartbeat
	// goroutine and the model-pump goroutine can read/write without a
	// mutex. Initialized to "now" so the first heartbeat tick (10s
	// from now) doesn't immediately trip the stall threshold while
	// the model is still establishing its connection.
	var lastEventNanos atomic.Int64
	lastEventNanos.Store(time.Now().UnixNano())

	// Defer ORDER (LIFO):
	//   1. defer release (registered second; runs LAST)
	//   2. defer hbCancel (registered first; runs SECOND from last)
	//
	// Heartbeat must stop issuing UPDATEs before release runs; otherwise
	// they race (correct semantics, wasted UPDATEs + log noise).
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer func() {
		// Release on detached background ctx: by the time this runs
		// the request ctx is typically Done (client disconnect, stall
		// abort), so a ctx-bound query would fail immediately.
		relCtx, relCancel := context.WithTimeout(context.Background(), leaseReleaseTimeout)
		defer relCancel()
		if err := s.queries.ReleaseConversationLease(relCtx, db.ReleaseConversationLeaseParams{
			ID:         conv.ID,
			LockHolder: convert.PgUUID(sessionUID),
		}); err != nil {
			slog.WarnContext(relCtx, "release conversation lease failed; lease will expire via TTL",
				"conversation_id", conv.ID, "error", err)
		}
	}()
	defer hbCancel()
	go s.runConversationLeaseHeartbeat(hbCtx, streamCancel, conv.ID, sessionUID, &lastEventNanos)

	// Persist the inbound user/tool turn (Tx A) unless it was already
	// persisted atomically with the conversation on the auto-create path.
	if !skipInboundPersist {
		if err := s.persistInboundTurn(ctx, req, conv.ID, sessionUID); err != nil {
			return nil, nil, "", err
		}
	}

	// History always loads from DB now (state-of-conversation is the
	// source of truth, not the inbound message list).
	history, err := s.loadModelHistory(ctx, conv.ID)
	if err != nil {
		slog.ErrorContext(ctx, "load history failed", "conversation_id", conv.ID, "error", err)
		return nil, nil, "", apierr.Internal(err, "failed to load history")
	}

	// Resolve system instruction: per-call override wins; otherwise
	// fall back to the server default. (Stored conversation-level
	// system instruction is a future addition.)
	systemPrompt := req.GetSystemInstruction()
	if systemPrompt == "" {
		systemPrompt = s.defaultSystemPrompt()
	}

	// Emit `start` carrying the conversation resource name in
	// `messageMetadata` so the client can persist the handle for
	// follow-up turns (and skip re-sending the entire history every
	// time). Only emitted when streaming; unary GenerateContent passes
	// nil for `emit` so the entire block is skipped.
	assistantMsgID := uuid.New().String()[:12]
	if emit != nil {
		startEvent := &aiv1.Start{
			MessageId: assistantMsgID,
			MessageMetadata: &aiv1.ChatMessageMetadata{
				Conversation: fmt.Sprintf(
					"organizations/%s/users/%s/conversations/%s",
					orgName, convPathUser, conv.Name,
				),
			},
		}
		if err := emit(&aiv1.ServerEvent{
			Event: &aiv1.ServerEvent_Start{Start: startEvent},
		}); err != nil {
			return nil, nil, "", err
		}
		if err := emit(&aiv1.ServerEvent{
			Event: &aiv1.ServerEvent_TextStart{TextStart: &aiv1.TextStart{Id: assistantMsgID}},
		}); err != nil {
			return nil, nil, "", err
		}
	}

	// Call the model. streamCtx (not ctx) so the heartbeat can cancel
	// a stalled upstream stream via streamCancel().
	modelReq := model.StreamRequest{
		Messages:     history,
		Tools:        s.tools.ToDefinitions(),
		SystemPrompt: systemPrompt,
	}
	reader, err := s.model.Stream(streamCtx, modelReq)
	if err != nil {
		if emit != nil {
			_ = s.sendStreamErrorEmit(emit, err)
		}
		slog.ErrorContext(ctx, "model stream failed", "error", err)
		return nil, nil, "", apierr.Internal(err, "model stream")
	}
	defer func() {
		// Best-effort close on the model stream reader; the model
		// client returns "already closed" errors here on shutdown
		// races which aren't actionable.
		_ = reader.Close()
	}()

	// Pump model events; accumulate text for the unary return path
	// and for persistence. Every event received here counts as
	// upstream progress for the heartbeat's bytes-based liveness
	// check — record the timestamp before any per-event handling so
	// a slow downstream emit doesn't undercount upstream activity.
	var assistantText strings.Builder
	for {
		evt, err := reader.Next(streamCtx)
		if err == io.EOF {
			break
		}
		if err != nil {
			if emit != nil {
				_ = s.sendStreamErrorEmit(emit, err)
			}
			return nil, nil, "", err
		}
		lastEventNanos.Store(time.Now().UnixNano())

		switch evt.Kind {
		case "text_delta":
			assistantText.WriteString(evt.Text)
			if emit != nil {
				if err := emit(&aiv1.ServerEvent{
					Event: &aiv1.ServerEvent_TextDelta{TextDelta: &aiv1.TextDelta{
						Id:    assistantMsgID,
						Delta: evt.Text,
					}},
				}); err != nil {
					// emit failures here mean the client stream is
					// already dead (broken pipe / cancellation) —
					// trying to send StreamError back through the
					// same channel would also fail. Surface the
					// error to the caller; gRPC's transport layer
					// reports the disconnection to its peer via
					// trailers.
					return nil, nil, "", err
				}
			}

		case "tool_call_complete":
			if emit != nil {
				if err := s.emitToolCall(ctx, emit, evt.ToolCall); err != nil {
					return nil, nil, "", err
				}
			}

		case "finish":
			// Handled after the loop.
		}
	}

	if emit != nil {
		if err := emit(&aiv1.ServerEvent{
			Event: &aiv1.ServerEvent_TextEnd{TextEnd: &aiv1.TextEnd{Id: assistantMsgID}},
		}); err != nil {
			return nil, nil, "", err
		}
	}

	// Build the assistant message proto. Vercel UIMessagePart shape:
	// `{type: "text", text: "..."}` — flat, discriminated by `type`.
	assistantParts := []*aiv1.MessagePart{
		{Type: "text", Text: assistantText.String(), State: "done"},
	}
	assistantMsg := &aiv1.Message{
		Role:  "assistant",
		Parts: assistantParts,
	}

	// Persist assistant response. Separate tx from the inbound batch —
	// model.Stream just ran (potentially seconds-to-minutes), so we
	// don't want a tx held across that. Lock once, persist once.
	//
	// marshalParts runs OUTSIDE the tx closure per CLAUDE.md's
	// "tx closures must be DB-only" rule — marshaling is pure and a
	// retry of the closure shouldn't re-marshal. Its error must be
	// checked before opening the tx; the prior `_ :=` swallow meant
	// a marshal failure persisted `nil` into the parts column
	// silently.
	assistantPartsJSON, err := marshalParts(assistantParts)
	if err != nil {
		slog.ErrorContext(ctx, "marshal assistant parts failed", "conversation_id", conv.ID, "error", err)
		return nil, nil, "", apierr.Internal(err, "marshal assistant parts")
	}
	if err := db.RunInTxVoid(ctx, s.pool, func(qtx db.Querier) error {
		row, err := qtx.GetConversationByIDForUpdate(ctx, conv.ID)
		if err != nil {
			slog.ErrorContext(ctx, "lock conversation failed", "conversation_id", conv.ID, "error", err)
			return apierr.Internal(err, "lock conversation")
		}
		// Invariant check — same shape as Tx A. By the model's
		// design exactly one lease exists per conversation at a
		// time; the heartbeat is responsible for canceling the
		// stream ctx if it ever loses the lease (stall, expired
		// UPDATE, DB error). Reaching this Tx with a mismatched
		// holder means heartbeat didn't cancel — i.e. a code bug.
		// Internal + slog.Error so it surfaces in alerts.
		if !row.LockHolder.Valid || row.LockHolder.Bytes != sessionUID {
			slog.ErrorContext(ctx, "INVARIANT: Tx B reached without holding lease",
				"conversation_id", conv.ID,
				"session_uid", sessionUID,
				"row_holder_valid", row.LockHolder.Valid,
				"row_holder", uuid.UUID(row.LockHolder.Bytes).String(),
				"row_expires_at", row.LockExpiresAt)
			return apierr.Internal(nil, "lease invariant violation")
		}
		return persistMessageOnQtx(ctx, qtx, conv.ID, db.CreateMessageParams{
			ConversationID: conv.ID,
			Name:           assistantMsgID,
			Role:           "assistant",
			Parts:          assistantPartsJSON,
			TokenCount:     int32(estimateTokens(assistantText.String())),
		})
	}); err != nil {
		// Inner sites already slog'd specifics; this is the handler's
		// failure-path summary. Collapse to a generic Internal so we
		// don't leak driver detail across the gRPC trailer.
		slog.ErrorContext(ctx, "persist assistant message failed", "conversation_id", conv.ID, "error", err)
		return nil, nil, "", apierr.Internal(err, "persist assistant message")
	}

	// Full AIP-122 resource name.
	assistantMsg.Name = buildMessageName(orgName, convPathUser, conv.Name, assistantMsgID)

	usage := &aiv1.TokenUsage{
		InputTokens:  estimateInputTokens(history, systemPrompt),
		OutputTokens: int32(estimateTokens(assistantText.String())),
	}
	return assistantMsg, usage, s.model.Name(), nil
}

// persistInboundTurn persists the LAST inbound message (the submitted
// turn) under the conversation's row lock in its own transaction (Tx A).
// useChat sends the entire UI conversation history on every turn (its
// default transport behavior); the server already has every prior turn
// via prior calls' persistence. Looping over all inbound messages would
// re-insert duplicates of the prior turns and explode the conversation by
// N every round. The Pivox transport strips history client-side once
// `conversation` is set so len(inbound)==1 in practice; taking the last is
// defense in depth.
//
// Only the resume path (existing conversation) calls this. The auto-create
// path persists its single first message inside
// createConversationWithFirstMessage instead, and passes
// skipInboundPersist=true so runGenerate does not call this.
func (s *Server) persistInboundTurn(ctx context.Context, req *aiv1.GenerateContentRequest, convID, sessionUID uuid.UUID) error {
	return db.RunInTxVoid(ctx, s.pool, func(qtx db.Querier) error {
		row, err := qtx.GetConversationByIDForUpdate(ctx, convID)
		if err != nil {
			slog.ErrorContext(ctx, "lock conversation failed", "conversation_id", convID, "error", err)
			return apierr.Internal(err, "lock conversation")
		}
		// Invariant check. Acquire happened microseconds ago so the
		// holder MUST be us. The model permits exactly one active
		// lease per conversation at any moment — acquire rejects
		// overlapping sessions outright (ACTIVE_STREAM Aborted). If
		// this row doesn't match our sessionUID, something has
		// violated the schema invariant (heartbeat goroutine died
		// without aborting the stream, manual SQL, replication
		// anomaly). Internal, not Aborted: this is a bug to file,
		// not a retryable race.
		if !row.LockHolder.Valid || row.LockHolder.Bytes != sessionUID {
			slog.ErrorContext(ctx, "INVARIANT: Tx A reached without holding lease",
				"conversation_id", convID,
				"session_uid", sessionUID,
				"row_holder_valid", row.LockHolder.Valid,
				"row_holder", uuid.UUID(row.LockHolder.Bytes).String(),
				"row_expires_at", row.LockExpiresAt)
			return apierr.Internal(nil, "lease invariant violation")
		}
		inbound := req.GetMessages()
		if len(inbound) == 0 {
			// Unreachable under the validator chain (the proto's
			// `repeated.min_items=1` on GenerateContentRequest.messages
			// + the unary/stream validation interceptors). If it
			// fires anyway — interceptor bypassed, internal caller,
			// validator drift — surface loudly. Silent return-nil
			// would commit the tx as a no-op and let the assistant
			// persist alone (a "user said nothing → assistant
			// replied" row pair).
			return apierr.Internal(nil, "invariant: runGenerate called with no inbound messages")
		}
		last := inbound[len(inbound)-1]
		params, err := buildInputMessageParams(convID, last)
		if err != nil {
			return err
		}
		return persistMessageOnQtx(ctx, qtx, convID, params)
	})
}

// buildInputMessageParams converts an InputMessage proto into the
// CreateMessage params shape, leaving Sequence unset (the tx-bound
// caller fills it under the conversation's row lock). Pure function;
// no DB access. The validation interceptor has already enforced
// shape invariants (non-nil, non-empty parts, role is USER/TOOL,
// tool-role has a tool_result part with tool_call_id) by the time
// this runs.
func buildInputMessageParams(convID uuid.UUID, in *aiv1.InputMessage) (db.CreateMessageParams, error) {
	role, err := dbRoleForInputMessage(in.GetRole())
	if err != nil {
		slog.Error("unexpected role reached persistence", "role", in.GetRole(), "error", err)
		return db.CreateMessageParams{}, err
	}
	parts := in.GetParts()
	logText := extractText(parts)
	partsJSON, err := marshalParts(parts)
	if err != nil {
		slog.Error("marshal parts failed", "error", err)
		return db.CreateMessageParams{}, apierr.Internal(err, "failed to marshal parts")
	}
	return db.CreateMessageParams{
		ConversationID: convID,
		Name:           uuid.New().String()[:12],
		Role:           role,
		Parts:          partsJSON,
		TokenCount:     int32(estimateTokens(logText)),
	}, nil
}

// persistMessageOnQtx writes a single message under an existing tx.
//
// Caller MUST already hold a FOR UPDATE lock on the conversation row
// inside the same tx (acquired via GetConversationByIDForUpdate).
// This helper assumes the lock is held and computes the next
// sequence number under it; mixing this with a non-tx-bound Querier
// or skipping the prior lock defeats the race protection.
//
// Why the lock matters: GetNextSequenceForConversation is
// MAX(sequence)+1. Two concurrent persists could each read the same
// N before either commits, then both insert with sequence=N —
// violating UNIQUE(conversation_id, sequence) and surfacing as a
// 23505 to whichever loses the race. The lock forces concurrent
// persists to queue, so each computes a fresh sequence.
//
// Why we surface IncrementConversationMessageCount errors (the
// pre-tx code dropped them via `_ = ...`): inside the tx a failed
// increment rolls back the message create, so the caller can't
// observe a created message paired with a stale message_count.
//
// Sequence field on params is ignored; we set it inside.
func persistMessageOnQtx(ctx context.Context, qtx db.Querier, convID uuid.UUID, params db.CreateMessageParams) error {
	nextSeq, err := qtx.GetNextSequenceForConversation(ctx, convID)
	if err != nil {
		slog.ErrorContext(ctx, "get sequence failed", "conversation_id", convID, "error", err)
		return apierr.Internal(err, "failed to get sequence")
	}
	params.Sequence = int64(nextSeq)
	if _, err := qtx.CreateMessage(ctx, params); err != nil {
		slog.ErrorContext(ctx, "persist message failed", "conversation_id", convID, "error", err)
		return apierr.Internal(err, "failed to persist message")
	}
	if err := qtx.IncrementConversationMessageCount(ctx, convID); err != nil {
		slog.ErrorContext(ctx, "increment message count failed", "conversation_id", convID, "error", err)
		return apierr.Internal(err, "increment message count")
	}
	return nil
}

// dbRoleForInputMessage maps a wire-shaped InputMessage role string
// to the canonical DB role.
//
// With the stream validation interceptor in place
// (internal/server/validate_stream_interceptor.go) the buf-validate
// `in: ["user", "assistant", "system", "tool"]` rule on
// InputMessage.role rejects malformed roles at the gRPC boundary.
// This helper is the last-line internal check: if an unrecognized
// role reaches here, the validator failed, the proto schema drifted,
// or a caller bypassed the gRPC stack. Loud Internal failure beats
// silent coercion-to-"user" (the prior behavior masked client bugs
// as data corruption — assistant turns persisted as user-role rows).
func dbRoleForInputMessage(r string) (string, error) {
	switch r {
	case "user", "assistant", "system", "tool":
		return r, nil
	default:
		return "", apierr.Internal(nil, "unexpected role reached persistence (validator should have rejected)")
	}
}

// runConversationLeaseHeartbeat is the authoritative liveness
// monitor for an in-flight stream. Runs as a goroutine; exits on
// ctx.Done() (caller teardown) or after firing streamCancel().
//
// Three responsibilities, in priority order:
//
//  1. **Stall detection.** No upstream model event in
//     `streamStallAbortThreshold` (60s) → call streamCancel() and
//     exit. This is what makes the lease "bytes-based": a model
//     that hangs with a live TCP socket still triggers abort.
//
//  2. **Defense-in-depth lease aging.** No event in
//     `leaseStaleExtensionThreshold` (30s) → skip the extension
//     UPDATE for this tick. The lease expires naturally per the
//     SQL-side TTL even if (1) never fires.
//
//  3. **Lease-lost detection.** Extension UPDATE returns 0 rows
//     (someone else holds it, or our own lease passed the SQL
//     guard's `lock_expires_at > now()` window) → call
//     streamCancel() and exit.
func (s *Server) runConversationLeaseHeartbeat(
	ctx context.Context,
	streamCancel context.CancelFunc,
	convID, sessionUID uuid.UUID,
	lastEventNanos *atomic.Int64,
) {
	// Silent goroutine death (panic with no recover) would leave the
	// stream running without progress monitoring — no abort on stall,
	// no observable failure mode. Surface the panic loudly via slog so
	// it shows up in alerts; the stream's normal cleanup path still
	// runs (caller's defer release on function return).
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "heartbeat goroutine panic",
				"conversation_id", convID,
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()
	ticker := time.NewTicker(leaseHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			elapsed := time.Since(time.Unix(0, lastEventNanos.Load()))

			// (1) Stall — abort the stream and exit.
			if elapsed > streamStallAbortThreshold {
				slog.WarnContext(ctx, "stream stalled past abort threshold; canceling stream",
					"conversation_id", convID,
					"elapsed_seconds", elapsed.Seconds(),
					"threshold_seconds", streamStallAbortThreshold.Seconds())
				streamCancel()
				return
			}

			// (2) Stale — skip extension; loop continues so we can
			// re-evaluate after another tick (bytes may resume).
			if elapsed > leaseStaleExtensionThreshold {
				continue
			}

			// (3) Healthy — extend the lease.
			_, err := s.queries.HeartbeatConversationLease(ctx, db.HeartbeatConversationLeaseParams{
				ID:         convID,
				LockHolder: convert.PgUUID(sessionUID),
			})
			if err == nil {
				continue
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			if errors.Is(err, pgx.ErrNoRows) {
				// SQL guard returns 0 rows when we no longer hold a
				// non-expired lease. Treat as terminal: cancel the
				// stream so the persist Tx doesn't run against a
				// row we don't own.
				slog.WarnContext(ctx, "lease lost during heartbeat; canceling stream",
					"conversation_id", convID,
					"elapsed_seconds", elapsed.Seconds())
				streamCancel()
				return
			}
			// Transient DB error — log and keep ticking. The next
			// tick will reattempt; if the failure persists past the
			// stale threshold we'll fall into (2)/(1) above and
			// abort cleanly.
			slog.ErrorContext(ctx, "lease heartbeat failed", "conversation_id", convID, "error", err)
		}
	}
}

// Removed: `inputMessagesToModel` and the stateless-branch caller.
// Reintroducing it would let callers bypass persistence — the
// stateless RPC path no longer exists.

// protoPartToModel converts a proto MessagePart (Vercel-shaped flat
// part with a `type` discriminator) into the model layer's
// MessagePart (Pivox-internal text/tool_call/tool_result shape).
//
// Returns `ok=false` for variants the model layer doesn't yet
// understand (source-*, file, data-*, step-start, dynamic-tool's
// `input-streaming` state). Callers should skip those silently —
// the model still gets the rest of the turn.
func protoPartToModel(p *aiv1.MessagePart) (model.MessagePart, bool) {
	switch t := p.GetType(); {
	case t == "text":
		return model.MessagePart{Type: "text", Text: p.GetText()}, true
	case t == "reasoning":
		// Model layer doesn't (yet) distinguish reasoning from text;
		// fold into a text part so the content reaches the LLM.
		return model.MessagePart{Type: "text", Text: p.GetText()}, true
	case strings.HasPrefix(t, "tool-") || t == "dynamic-tool":
		toolName := p.GetToolName()
		if toolName == "" && strings.HasPrefix(t, "tool-") {
			toolName = strings.TrimPrefix(t, "tool-")
		}
		switch p.GetState() {
		case "input-available":
			return model.MessagePart{
				Type: "tool_call",
				ToolCall: &model.ToolCall{
					ID:        p.GetToolCallId(),
					Name:      toolName,
					InputJSON: structToJSON(p.GetInput()),
				},
			}, true
		case "output-available":
			return model.MessagePart{
				Type: "tool_result",
				ToolResult: &model.ToolResult{
					CallID:     p.GetToolCallId(),
					Name:       toolName,
					ResultJSON: structToJSON(p.GetOutput()),
				},
			}, true
		case "output-error":
			return model.MessagePart{
				Type: "tool_result",
				ToolResult: &model.ToolResult{
					CallID:     p.GetToolCallId(),
					Name:       toolName,
					ResultJSON: p.GetErrorText(),
					IsError:    true,
				},
			}, true
		}
	}
	return model.MessagePart{}, false
}

// structToJSON renders a structpb.Struct as its JSON string form
// (matching the model layer's `InputJSON` / `ResultJSON` contract).
// Nil → empty string, no error path — the model handles either.
func structToJSON(s *structpb.Struct) string {
	if s == nil {
		return ""
	}
	b, err := protojson.Marshal(s)
	if err != nil {
		return ""
	}
	return string(b)
}

// emitToolCall emits the ToolInputAvailable event and, for server-side
// tools, also runs the tool and emits its output. Takes the parent
// `ctx` so server-side tool execution inherits the call's deadline,
// cancellation, and authenticated UID — without this the tool would
// outlive client disconnects (resource leak) and run unauthenticated.
//
// The proto carries tool input/output as `google.protobuf.Struct`
// (structured JSON, not strings) so consumers don't double-parse and
// the SSE adapter emits a native JSON object on the wire. The
// upstream model layer hands us JSON-encoded strings, so this
// helper decodes them once at the proto boundary.
func (s *Server) emitToolCall(ctx context.Context, emit func(*aiv1.ServerEvent) error, tc *model.ToolCall) error {
	if tc == nil {
		return nil
	}
	isServer := s.tools.IsServerTool(tc.Name)
	inputStruct, err := jsonObjectToStruct(tc.InputJSON)
	if err != nil {
		return s.sendStreamErrorEmit(emit, err)
	}
	if err := emit(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_ToolInputAvailable{ToolInputAvailable: &aiv1.ToolInputAvailable{
			ToolCallId:       tc.ID,
			ToolName:         tc.Name,
			Input:            inputStruct,
			ProviderExecuted: isServer,
		}},
	}); err != nil {
		return err
	}
	if !isServer {
		// Client executes; the round-trip happens via a follow-up
		// StreamGenerateContent call carrying the tool result.
		return nil
	}
	tool := s.tools.Get(tc.Name)
	result, execErr := tool.Execute(ctx, tc.InputJSON)
	if execErr != nil {
		return emit(&aiv1.ServerEvent{
			Event: &aiv1.ServerEvent_ToolOutputError{ToolOutputError: &aiv1.ToolOutputError{
				ToolCallId: tc.ID,
				ErrorText:  execErr.Error(),
			}},
		})
	}
	outputStruct, err := jsonObjectToStruct(result)
	if err != nil {
		return s.sendStreamErrorEmit(emit, err)
	}
	return emit(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_ToolOutputAvailable{ToolOutputAvailable: &aiv1.ToolOutputAvailable{
			ToolCallId: tc.ID,
			Output:     outputStruct,
		}},
	})
}

// jsonObjectToStruct decodes a JSON-encoded string into a
// google.protobuf.Struct. Empty input returns nil (the SSE wire will
// elide the field). Non-object JSON (a bare string, number, or
// array) is wrapped in a single-key envelope so the value still
// reaches the client.
func jsonObjectToStruct(s string) (*structpb.Struct, error) {
	if s == "" {
		return nil, nil
	}
	out := &structpb.Struct{}
	if err := protojson.Unmarshal([]byte(s), out); err == nil {
		return out, nil
	}
	// Fallback: parse as a free-form Value and box it under "value".
	v := &structpb.Value{}
	if err := protojson.Unmarshal([]byte(s), v); err != nil {
		return nil, err
	}
	return &structpb.Struct{Fields: map[string]*structpb.Value{"value": v}}, nil
}

func (s *Server) sendStreamErrorEmit(emit func(*aiv1.ServerEvent) error, err error) error {
	// `Error` carries a single error_text field; we surface the
	// status message when err is a status, otherwise the raw text.
	// The full Status (code + details) is lost on the wire — that's
	// intentional, the Vercel chunk schema only carries an error
	// string. Internal callers that need richer error data should
	// rely on the gRPC trailer error returned from runGenerate.
	msg := err.Error()
	if st, ok := status.FromError(err); ok {
		msg = st.Message()
	}
	return emit(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_Error{Error: &aiv1.Error{
			ErrorText: msg,
		}},
	})
}

// loadModelHistory fetches recent messages and truncates to fit the token budget.
func (s *Server) loadModelHistory(ctx context.Context, convID uuid.UUID) ([]model.Message, error) {
	rows, err := s.queries.ListMessagesNewestFirst(ctx, db.ListMessagesNewestFirstParams{
		ConversationID: convID,
		Limit:          defaultMaxHistoryRows,
	})
	if err != nil {
		return nil, err
	}

	// Walk newest→oldest accumulating tokens, stop when budget exceeded.
	budget := defaultModelContextBudget
	var kept []db.AiMessage
	running := 0
	for _, row := range rows {
		running += int(row.TokenCount)
		if running > budget {
			break
		}
		kept = append(kept, row)
	}

	// Reverse to chronological order.
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	msgs := make([]model.Message, 0, len(kept))
	for _, row := range kept {
		msgs = append(msgs, dbMessageToModel(row))
	}
	return msgs, nil
}

func dbMessageToModel(row db.AiMessage) model.Message {
	m := model.Message{Role: row.Role}

	parts, _ := unmarshalParts(row.Parts)
	for _, p := range parts {
		if mp, ok := protoPartToModel(p); ok {
			m.Parts = append(m.Parts, mp)
		}
	}

	// Fallback: if no structured parts, use the raw text heuristic.
	if len(m.Parts) == 0 && len(row.Parts) > 2 {
		m.Parts = append(m.Parts, model.MessagePart{
			Type: "text",
			Text: string(row.Parts),
		})
	}

	return m
}

func (s *Server) defaultSystemPrompt() string {
	return "You are a helpful AI assistant in Pivox."
}

// extractText concatenates all text parts from a list of message parts.
func extractText(parts []*aiv1.MessagePart) string {
	var sb strings.Builder
	for _, p := range parts {
		if p.GetType() == "text" {
			sb.WriteString(p.GetText())
		}
	}
	return sb.String()
}

// estimateInputTokens approximates the prompt-side token count from the
// model history plus system prompt. Coarse but enough for billing/UX
// observability.
// estimateInputTokens approximates prompt-side tokens across all
// part types. Tool-heavy turns (where the conversation is mostly
// JSON tool calls + results, with little prose) would otherwise
// under-report by ~10x because text-only counting misses the JSON
// payloads that the model actually consumes. Coarse but correct
// enough for billing/observability.
func estimateInputTokens(history []model.Message, systemPrompt string) int32 {
	total := estimateTokens(systemPrompt)
	for _, m := range history {
		for _, p := range m.Parts {
			switch p.Type {
			case "text":
				total += estimateTokens(p.Text)
			case "tool_call":
				if p.ToolCall != nil {
					total += estimateTokens(p.ToolCall.Name) + estimateTokens(p.ToolCall.InputJSON)
				}
			case "tool_result":
				if p.ToolResult != nil {
					total += estimateTokens(p.ToolResult.Name) + estimateTokens(p.ToolResult.ResultJSON)
				}
			}
		}
	}
	return int32(total)
}
