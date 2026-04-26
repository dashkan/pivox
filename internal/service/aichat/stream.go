package aichat

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
// sequence of `ServerEvent`s. The unary `GenerateContent` below
// shares the same core via `runGenerate`.
func (s *Server) StreamGenerateContent(req *aiv1.GenerateContentRequest, stream grpc.ServerStreamingServer[aiv1.ServerEvent]) error {
	ctx := stream.Context()
	_, _, _, err := s.runGenerate(ctx, req, func(ev *aiv1.ServerEvent) error {
		return stream.Send(ev)
	})
	if err != nil {
		return err
	}
	return stream.Send(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_Done{Done: &aiv1.Done{}},
	})
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
	if req == nil {
		return nil, nil, "", apierr.BadRequest("request is nil")
	}
	if req.GetParent() == "" {
		return nil, nil, "", apierr.BadRequest("parent is required")
	}
	if len(req.GetMessages()) == 0 {
		return nil, nil, "", apierr.BadRequest("messages must not be empty")
	}

	uid := server.MustAuthenticatedUID(ctx)

	// Validate the parent org regardless of stateful/stateless —
	// without this the stateless path would happily run inference
	// for any org name a caller invents, including ones they don't
	// belong to. resolveOrg returns NotFound for unknown names and
	// is the closest membership signal this service has today
	// (per-user org-membership IAM is a future addition).
	orgName, err := parseConversationParent(req.GetParent())
	if err != nil {
		return nil, nil, "", apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}
	if _, err := s.resolveOrg(ctx, orgName); err != nil {
		return nil, nil, "", err
	}

	// Stateful when conversation is set; stateless otherwise.
	var conv *db.AiConversation
	if convRef := req.GetConversation(); convRef != "" {
		convOrgName, convName, err := parseConversationName(convRef)
		if err != nil {
			return nil, nil, "", apierr.InvalidArgument(apierr.FieldViolation("conversation", err.Error()))
		}
		// Cross-check: conversation's org must match the request's
		// parent. Without this a caller could pass parent=A and
		// conversation under org B and write into B.
		if convOrgName != orgName {
			return nil, nil, "", apierr.BadRequest("conversation's organization does not match request parent")
		}
		row, err := s.resolveConversation(ctx, convOrgName, convName, uid)
		if err != nil {
			return nil, nil, "", err
		}
		conv = &row

		// Persist each inbound message in order. The most common
		// case is a single user turn, but the request shape allows
		// multiple (e.g. catching up after a tool round trip).
		for _, m := range req.GetMessages() {
			if err := s.persistInputMessage(ctx, conv.ID, m); err != nil {
				return nil, nil, "", err
			}
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

	// Emit TextStart up front so streaming clients can show a
	// placeholder bubble immediately, before the first delta lands.
	assistantMsgID := uuid.New().String()[:12]
	if emit != nil {
		if err := emit(&aiv1.ServerEvent{
			Event: &aiv1.ServerEvent_TextStart{TextStart: &aiv1.TextStart{MessageId: assistantMsgID}},
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
	defer reader.Close()

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
					Event: &aiv1.ServerEvent_TextDelta{TextDelta: &aiv1.TextDelta{Delta: evt.Text}},
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
			Event: &aiv1.ServerEvent_TextEnd{TextEnd: &aiv1.TextEnd{}},
		}); err != nil {
			return nil, nil, "", err
		}
	}

	// Build the assistant message proto.
	assistantParts := []*aiv1.MessagePart{
		{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: assistantText.String()}}},
	}
	assistantMsg := &aiv1.Message{
		Role:  aiv1.Role_ASSISTANT,
		Parts: assistantParts,
	}

	// Persist assistant response when stateful.
	if conv != nil {
		nextSeq, err := s.queries.GetNextSequenceForConversation(ctx, conv.ID)
		if err != nil {
			slog.ErrorContext(ctx, "get sequence failed", "conversation_id", conv.ID, "error", err)
			return nil, nil, "", apierr.Internal("get sequence")
		}
		assistantPartsJSON, _ := marshalParts(assistantParts)
		_, err = s.queries.CreateMessage(ctx, db.CreateMessageParams{
			ConversationID: conv.ID,
			Name:           assistantMsgID,
			Role:           "assistant",
			Parts:          assistantPartsJSON,
			Sequence:       int64(nextSeq),
			TokenCount:     int32(estimateTokens(assistantText.String())),
		})
		if err != nil {
			slog.ErrorContext(ctx, "persist assistant message failed", "conversation_id", conv.ID, "error", err)
			return nil, nil, "", apierr.Internal("persist assistant message")
		}
		_ = s.queries.IncrementConversationMessageCount(ctx, conv.ID)

		// Full AIP-122 resource name. We have orgName resolved
		// upstream from `req.GetParent()`.
		assistantMsg.Name = buildMessageName(orgName, conv.Name, assistantMsgID)
	}

	usage := &aiv1.TokenUsage{
		InputTokens:  estimateInputTokens(history, systemPrompt),
		OutputTokens: int32(estimateTokens(assistantText.String())),
	}
	return assistantMsg, usage, s.model.Name(), nil
}

// persistInputMessage writes a single InputMessage to the conversation
// history, picking a sequence number and counting tokens.
func (s *Server) persistInputMessage(ctx context.Context, convID uuid.UUID, in *aiv1.InputMessage) error {
	if in == nil {
		return apierr.BadRequest("input message is nil")
	}
	if len(in.GetParts()) == 0 {
		return apierr.BadRequest("input message has no parts")
	}

	role, err := dbRoleForInputMessage(in.GetRole())
	if err != nil {
		return err
	}
	parts := in.GetParts()
	if err := validateInputParts(role, parts); err != nil {
		return err
	}
	logText := extractText(parts)

	nextSeq, err := s.queries.GetNextSequenceForConversation(ctx, convID)
	if err != nil {
		slog.ErrorContext(ctx, "get sequence failed", "conversation_id", convID, "error", err)
		return apierr.Internal("failed to get sequence")
	}

	partsJSON, err := marshalParts(parts)
	if err != nil {
		slog.ErrorContext(ctx, "marshal parts failed", "error", err)
		return apierr.Internal("failed to marshal parts")
	}

	_, err = s.queries.CreateMessage(ctx, db.CreateMessageParams{
		ConversationID: convID,
		Name:           uuid.New().String()[:12],
		Role:           role,
		Parts:          partsJSON,
		Sequence:       int64(nextSeq),
		TokenCount:     int32(estimateTokens(logText)),
	})
	if err != nil {
		slog.ErrorContext(ctx, "persist message failed", "conversation_id", convID, "error", err)
		return apierr.Internal("failed to persist message")
	}
	_ = s.queries.IncrementConversationMessageCount(ctx, convID)
	return nil
}

// dbRoleForInputMessage maps a proto Role to the string the DB layer
// expects. Only USER and TOOL are valid on input:
//
//   - ASSISTANT turns are server-produced (via the model) and must
//     never be client-supplied — accepting one would let a caller
//     inject fake assistant context to manipulate later turns.
//   - SYSTEM instructions arrive via `system_instruction` on the
//     request, not as a message in the array.
//   - ROLE_UNSPECIFIED is treated as USER for forward-compat with
//     older clients that didn't set the field.
//
// Anything else fails fast with InvalidArgument.
func dbRoleForInputMessage(r aiv1.Role) (string, error) {
	switch r {
	case aiv1.Role_TOOL:
		return "tool", nil
	case aiv1.Role_USER, aiv1.Role_ROLE_UNSPECIFIED:
		return "user", nil
	case aiv1.Role_ASSISTANT:
		return "", apierr.BadRequest("assistant role is not valid on input messages — assistant turns are server-produced")
	case aiv1.Role_SYSTEM:
		return "", apierr.BadRequest("system role is not valid on input messages — use system_instruction on the request instead")
	default:
		return "", apierr.BadRequest(fmt.Sprintf("unsupported role: %v", r))
	}
}

// validateInputParts enforces semantic constraints we can't express
// in the proto schema. Currently: TOOL-role messages must carry at
// least one ToolResultPart, and every ToolResultPart must have a
// non-empty tool_call_id (otherwise the model has no way to match
// the result to its earlier tool call).
func validateInputParts(role string, parts []*aiv1.MessagePart) error {
	if role != "tool" {
		return nil
	}
	sawToolResult := false
	for _, p := range parts {
		tr := p.GetToolResult()
		if tr == nil {
			continue
		}
		sawToolResult = true
		if strings.TrimSpace(tr.GetToolCallId()) == "" {
			return apierr.BadRequest("tool result part missing tool_call_id")
		}
	}
	if !sawToolResult {
		return apierr.BadRequest("tool-role message must include at least one tool_result part")
	}
	return nil
}

// inputMessagesToModel converts a list of proto InputMessages to the
// internal model layer's representation, used by the stateless path.
// Returns InvalidArgument if any role is invalid (assistant/system on
// input is rejected — see `dbRoleForInputMessage` for rationale) or
// if a TOOL-role message is malformed.
func inputMessagesToModel(in []*aiv1.InputMessage) ([]model.Message, error) {
	out := make([]model.Message, 0, len(in))
	for _, m := range in {
		role, err := dbRoleForInputMessage(m.GetRole())
		if err != nil {
			return nil, err
		}
		if err := validateInputParts(role, m.GetParts()); err != nil {
			return nil, err
		}
		mm := model.Message{Role: role}
		for _, p := range m.GetParts() {
			switch {
			case p.GetText() != nil:
				mm.Parts = append(mm.Parts, model.MessagePart{
					Type: "text",
					Text: p.GetText().GetText(),
				})
			case p.GetToolCall() != nil:
				tc := p.GetToolCall()
				mm.Parts = append(mm.Parts, model.MessagePart{
					Type: "tool_call",
					ToolCall: &model.ToolCall{
						ID:        tc.GetToolCallId(),
						Name:      tc.GetTool(),
						InputJSON: tc.GetInputJson(),
					},
				})
			case p.GetToolResult() != nil:
				tr := p.GetToolResult()
				mm.Parts = append(mm.Parts, model.MessagePart{
					Type: "tool_result",
					ToolResult: &model.ToolResult{
						CallID:     tr.GetToolCallId(),
						Name:       tr.GetTool(),
						ResultJSON: tr.GetResultJson(),
						IsError:    tr.GetIsError(),
					},
				})
			}
		}
		out = append(out, mm)
	}
	return out, nil
}

// emitToolCall emits the ToolInputAvailable event and, for server-side
// tools, also runs the tool and emits its output. Takes the parent
// `ctx` so server-side tool execution inherits the call's deadline,
// cancellation, and authenticated UID — without this the tool would
// outlive client disconnects (resource leak) and run unauthenticated.
func (s *Server) emitToolCall(ctx context.Context, emit func(*aiv1.ServerEvent) error, tc *model.ToolCall) error {
	if tc == nil {
		return nil
	}
	isServer := s.tools.IsServerTool(tc.Name)
	if err := emit(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_ToolInputAvailable{ToolInputAvailable: &aiv1.ToolInputAvailable{
			ToolCallId: tc.ID,
			Tool:       tc.Name,
			InputJson:  tc.InputJSON,
			ServerSide: isServer,
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
			Event: &aiv1.ServerEvent_ToolError{ToolError: &aiv1.ToolError{
				ToolCallId:   tc.ID,
				ErrorMessage: execErr.Error(),
			}},
		})
	}
	return emit(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_ToolOutputAvailable{ToolOutputAvailable: &aiv1.ToolOutputAvailable{
			ToolCallId: tc.ID,
			ResultJson: result,
		}},
	})
}

func (s *Server) sendStreamErrorEmit(emit func(*aiv1.ServerEvent) error, err error) error {
	// Building a Status proto for embedding in a StreamError event,
	// not returning an RPC-level error — so apierr's generic-message
	// helpers don't apply. The client wants the actual error text to
	// surface in the streamed event for display; if `err` already
	// carries a status (e.g. from a downstream RPC), preserve it.
	st, ok := status.FromError(err)
	if !ok {
		st = status.New(codes.Internal, err.Error())
	}
	return emit(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_StreamError{StreamError: &aiv1.StreamError{
			Status: st.Proto(),
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
		switch {
		case p.GetText() != nil:
			m.Parts = append(m.Parts, model.MessagePart{
				Type: "text",
				Text: p.GetText().GetText(),
			})
		case p.GetToolCall() != nil:
			tc := p.GetToolCall()
			m.Parts = append(m.Parts, model.MessagePart{
				Type: "tool_call",
				ToolCall: &model.ToolCall{
					ID:        tc.GetToolCallId(),
					Name:      tc.GetTool(),
					InputJSON: tc.GetInputJson(),
				},
			})
		case p.GetToolResult() != nil:
			tr := p.GetToolResult()
			m.Parts = append(m.Parts, model.MessagePart{
				Type: "tool_result",
				ToolResult: &model.ToolResult{
					CallID:     tr.GetToolCallId(),
					Name:       tr.GetTool(),
					ResultJSON: tr.GetResultJson(),
					IsError:    tr.GetIsError(),
				},
			})
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
		if tp := p.GetText(); tp != nil {
			sb.WriteString(tp.GetText())
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
