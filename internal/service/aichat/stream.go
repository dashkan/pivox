package aichat

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/aichat/model"
)

const defaultModelContextBudget = 22500
const defaultMaxHistoryRows = 500

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

	if req.GetConversation() == "" {
		convName, err := s.ensureConversationForStream(ctx, req.GetParent())
		if err != nil {
			return err
		}
		req.Conversation = convName
	}

	_, _, _, err := s.runGenerate(ctx, req, func(ev *aiv1.ServerEvent) error {
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

// ensureConversationForStream creates a fresh Conversation row under
// the caller's user-in-org parent and returns its resource name. The
// permission interceptor (registered via the proto's
// `pivox.permission.v1.required_permission = "ai.chat.stream"` option;
// see internal/server/generated_registry.go) has already gated on the
// parent, so by the time this runs the caller is authorized to create
// conversations under this user.
//
// Org resolution + conversation insert run inside a single transaction
// per CLAUDE.md's tx rule — both touch `qtx`, and the FK from
// `ai_conversations.org_id` to `organizations.id` would surface a
// TOCTOU delete-then-insert as a 23503 mapped to NotFound rather than
// the typed `apierr.HandleResourceError` for Organization that the
// closure produces explicitly.
func (s *Server) ensureConversationForStream(ctx context.Context, parent string) (string, error) {
	orgName, pathUser, err := parseConversationParent(parent)
	if err != nil {
		return "", apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}
	callerUserID := server.MustPivoxUserID(ctx)
	if pathUser != callerUserID {
		return "", apierr.PermissionDenied("conversations may only be created under the caller's own user-uuid")
	}
	convSlug, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (string, error) {
		org, err := qtx.GetOrganizationByName(ctx, orgName)
		if err != nil {
			return "", apierr.HandleResourceError(err, "Organization", fmt.Sprintf("organizations/%s", orgName))
		}
		row, err := qtx.CreateConversation(ctx, db.CreateConversationParams{
			OrgID:     org.ID,
			Name:      uuid.New().String()[:12],
			CreatedBy: callerUserID,
		})
		if err != nil {
			return "", apierr.HandleResourceError(err, "Conversation", "")
		}
		return row.Name, nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("organizations/%s/users/%s/conversations/%s", orgName, pathUser, convSlug), nil
}

// GenerateContent is the unary counterpart to `StreamGenerateContent`.
// Runs the same generation flow but accumulates the response into a
// single `Message` and returns it. Use this for one-shot completions
// (title summarization, classification, etc.) and stateful turns
// where the caller doesn't need streaming.
func (s *Server) GenerateContent(ctx context.Context, req *aiv1.GenerateContentRequest) (*aiv1.GenerateContentResponse, error) {
	msg, usage, modelName, err := s.runGenerate(ctx, req, nil)
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
// Flow:
//
//  1. Validate the request and resolve org/conversation context.
//  2. If a `conversation` is set, persist the inbound `messages` to
//     the DB and load the full conversation history from there.
//     Otherwise (stateless), use the inbound `messages` as-is and
//     skip persistence.
//  3. Call the language model with the assembled context.
//  4. Stream the response: emit events via `emit` (when set) and
//     accumulate text into the returned `Message`.
//  5. If stateful, persist the assistant response.
//
// Returns the assistant `Message` (with name set when persisted),
// token usage, and the model identifier.
func (s *Server) runGenerate(
	ctx context.Context,
	req *aiv1.GenerateContentRequest,
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

	// Stateful when conversation is set; stateless otherwise.
	var conv *db.AiConversation
	var convPathUser uuid.UUID
	if convRef := req.GetConversation(); convRef != "" {
		convOrgName, pathUser, convName, err := parseConversationName(convRef)
		if err != nil {
			return nil, nil, "", apierr.InvalidArgument(apierr.FieldViolation("conversation", err.Error()))
		}
		if convOrgName != orgName {
			return nil, nil, "", apierr.BadRequest("conversation's organization does not match request parent")
		}
		// Generation is creator-only — no `*All` bypass. An admin
		// auditing another user's chats does not generate new turns
		// on their behalf.
		row, err := s.resolveConversation(ctx, convOrgName, pathUser, convName, "")
		if err != nil {
			return nil, nil, "", err
		}
		conv = &row
		convPathUser = pathUser

		// Persist each inbound message in order. The most common
		// case is a single user turn, but the request shape allows
		// multiple (e.g. catching up after a tool round trip).
		//
		// The whole batch runs in a single tx so a failure mid-loop
		// rolls back any messages already inserted in this call —
		// otherwise messages 1..k-1 commit, message k fails, the RPC
		// returns an error, and the conversation is left with phantom
		// rows the client has no name to retry against. Lock the
		// conversation row once at the start so concurrent persists
		// (e.g. another tool round-trip racing this one) queue.
		// Persist only the LAST inbound message. useChat sends the
		// entire UI conversation history on every turn (its default
		// transport behavior); the server already has every turn
		// before the last via prior calls' persistence. Looping over
		// all inbound messages here would re-insert duplicates of
		// the prior turns and explode the conversation by N every
		// round. Clients that strip the history client-side (the
		// Pivox transport does this once `conversation` is set) end
		// up with len(inbound)==1 anyway, so this branch is just
		// defense in depth.
		if err := db.RunInTxVoid(ctx, s.pool, func(qtx db.Querier) error {
			if _, err := qtx.GetConversationByIDForUpdate(ctx, conv.ID); err != nil {
				slog.ErrorContext(ctx, "lock conversation failed", "conversation_id", conv.ID, "error", err)
				return apierr.Internal("lock conversation")
			}
			inbound := req.GetMessages()
			if len(inbound) == 0 {
				return nil
			}
			last := inbound[len(inbound)-1]
			params, err := buildInputMessageParams(conv.ID, last)
			if err != nil {
				return err
			}
			return persistMessageOnQtx(ctx, qtx, conv.ID, params)
		}); err != nil {
			return nil, nil, "", err
		}
	}

	// Build the model context. Stateful: load history from DB
	// (already includes the just-persisted inbound messages).
	// Stateless: use inbound messages directly.
	var history []model.Message
	if conv != nil {
		h, err := s.loadModelHistory(ctx, conv.ID)
		if err != nil {
			slog.ErrorContext(ctx, "load history failed", "conversation_id", conv.ID, "error", err)
			return nil, nil, "", apierr.Internal("failed to load history")
		}
		history = h
	} else {
		h, err := inputMessagesToModel(req.GetMessages())
		if err != nil {
			return nil, nil, "", err
		}
		history = h
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
	// time). Only emitted when stateful; the unary GenerateContent
	// path passes nil for `emit` so this is naturally skipped for
	// non-streaming consumers, and stateless one-shots that don't
	// set up a conv have nothing useful to emit here.
	assistantMsgID := uuid.New().String()[:12]
	if emit != nil {
		startEvent := &aiv1.Start{MessageId: assistantMsgID}
		if conv != nil {
			startEvent.MessageMetadata = &aiv1.ChatMessageMetadata{
				Conversation: fmt.Sprintf(
					"organizations/%s/users/%s/conversations/%s",
					orgName, convPathUser, conv.Name,
				),
			}
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

	// Call the model.
	modelReq := model.StreamRequest{
		Messages:     history,
		Tools:        s.tools.ToDefinitions(),
		SystemPrompt: systemPrompt,
	}
	reader, err := s.model.Stream(ctx, modelReq)
	if err != nil {
		if emit != nil {
			_ = s.sendStreamErrorEmit(emit, err)
		}
		slog.ErrorContext(ctx, "model stream failed", "error", err)
		return nil, nil, "", apierr.Internal("model stream")
	}
	defer func() {
		// Best-effort close on the model stream reader; the model
		// client returns "already closed" errors here on shutdown
		// races which aren't actionable.
		_ = reader.Close()
	}()

	// Pump model events; accumulate text for the unary return path
	// and for persistence.
	var assistantText strings.Builder
	for {
		evt, err := reader.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			if emit != nil {
				_ = s.sendStreamErrorEmit(emit, err)
			}
			return nil, nil, "", err
		}

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

	// Persist assistant response when stateful. Separate tx from the
	// inbound batch — model.Stream just ran (potentially seconds-to-
	// minutes), so we don't want a tx held across that. Lock once,
	// persist once.
	//
	// marshalParts runs OUTSIDE the tx closure per CLAUDE.md's
	// "tx closures must be DB-only" rule — marshaling is pure and a
	// retry of the closure shouldn't re-marshal. Its error must be
	// checked before opening the tx; the prior `_ :=` swallow meant
	// a marshal failure persisted `nil` into the parts column
	// silently.
	if conv != nil {
		assistantPartsJSON, err := marshalParts(assistantParts)
		if err != nil {
			slog.ErrorContext(ctx, "marshal assistant parts failed", "conversation_id", conv.ID, "error", err)
			return nil, nil, "", apierr.Internal("marshal assistant parts")
		}
		if err := db.RunInTxVoid(ctx, s.pool, func(qtx db.Querier) error {
			if _, err := qtx.GetConversationByIDForUpdate(ctx, conv.ID); err != nil {
				slog.ErrorContext(ctx, "lock conversation failed", "conversation_id", conv.ID, "error", err)
				return apierr.Internal("lock conversation")
			}
			return persistMessageOnQtx(ctx, qtx, conv.ID, db.CreateMessageParams{
				ConversationID: conv.ID,
				Name:           assistantMsgID,
				Role:           "assistant",
				Parts:          assistantPartsJSON,
				TokenCount:     int32(estimateTokens(assistantText.String())),
			})
		}); err != nil {
			slog.ErrorContext(ctx, "persist assistant message failed", "conversation_id", conv.ID, "error", err)
			return nil, nil, "", apierr.Internal("persist assistant message")
		}

		// Full AIP-122 resource name. We have orgName resolved
		// upstream from `req.GetParent()`.
		assistantMsg.Name = buildMessageName(orgName, convPathUser, conv.Name, assistantMsgID)
	}

	usage := &aiv1.TokenUsage{
		InputTokens:  estimateInputTokens(history, systemPrompt),
		OutputTokens: int32(estimateTokens(assistantText.String())),
	}
	return assistantMsg, usage, s.model.Name(), nil
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
		return db.CreateMessageParams{}, apierr.Internal("failed to marshal parts")
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
		return apierr.Internal("failed to get sequence")
	}
	params.Sequence = int64(nextSeq)
	if _, err := qtx.CreateMessage(ctx, params); err != nil {
		slog.ErrorContext(ctx, "persist message failed", "conversation_id", convID, "error", err)
		return apierr.Internal("failed to persist message")
	}
	if err := qtx.IncrementConversationMessageCount(ctx, convID); err != nil {
		slog.ErrorContext(ctx, "increment message count failed", "conversation_id", convID, "error", err)
		return apierr.Internal("increment message count")
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
		return "", apierr.Internal("unexpected role reached persistence (validator should have rejected)")
	}
}

// inputMessagesToModel converts a list of proto InputMessages to the
// internal model layer's representation, used by the stateless path.
// Cross-field constraints (tool-role must include a tool-* part with
// state=output-available) are enforced at the validation interceptor
// via the buf-validate CEL rules on InputMessage and MessagePart.
func inputMessagesToModel(in []*aiv1.InputMessage) ([]model.Message, error) {
	out := make([]model.Message, 0, len(in))
	for _, m := range in {
		role, err := dbRoleForInputMessage(m.GetRole())
		if err != nil {
			slog.Error("unexpected role reached model conversion", "role", m.GetRole(), "error", err)
			return nil, err
		}
		mm := model.Message{Role: role}
		for _, p := range m.GetParts() {
			if mp, ok := protoPartToModel(p); ok {
				mm.Parts = append(mm.Parts, mp)
			}
		}
		out = append(out, mm)
	}
	return out, nil
}

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
