package aichat

import (
	"context"
	"io"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/aichat/model"
)

const defaultModelContextBudget = 22500
const defaultMaxHistoryRows = 500

func (s *Server) Stream(stream aiv1.AiChat_StreamServer) error {
	ctx := stream.Context()
	uid := server.MustAuthenticatedUID(ctx)
	s.logger.Info("Stream: connection opened", "uid", uid)

	// 1. Wait for first ClientEvent — must be UserMessage.
	firstMsg, err := stream.Recv()
	if err != nil {
		return err
	}
	userMsg := firstMsg.GetMessage()
	if userMsg == nil {
		return status.Error(codes.InvalidArgument, "first event must be a user message")
	}

	// 2. Load conversation, verify ownership.
	orgName, convName, err := parseConversationName(userMsg.GetConversation())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid conversation: %v", err)
	}

	conv, err := s.resolveConversation(ctx, orgName, convName)
	if err != nil {
		return err
	}
	if conv.CreatorUid != uid {
		return status.Error(codes.PermissionDenied, "conversation belongs to another user")
	}

	// 3. Get next sequence number and persist the user message.
	nextSeq, err := s.queries.GetNextSequenceForConversation(ctx, conv.ID)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get sequence: %v", err)
	}

	userText := extractText(userMsg.GetParts())
	userPartsJSON, err := marshalParts(userMsg.GetParts())
	if err != nil {
		return status.Errorf(codes.Internal, "failed to marshal parts: %v", err)
	}

	_, err = s.queries.CreateMessage(ctx, db.CreateMessageParams{
		ConversationID: conv.ID,
		Name:           uuid.New().String()[:12],
		Role:           "user",
		Parts:          userPartsJSON,
		Sequence:       int64(nextSeq),
		TokenCount:     int32(estimateTokens(userText)),
	})
	if err != nil {
		return status.Errorf(codes.Internal, "failed to persist user message: %v", err)
	}
	_ = s.queries.IncrementConversationMessageCount(ctx, conv.ID)

	// 4. Load conversation history for the model call.
	history, err := s.loadModelHistory(ctx, conv.ID)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to load history: %v", err)
	}

	s.logger.Info("Stream: user message received",
		"conv", conv.Name,
		"user_text", userText,
		"seq", nextSeq)

	// 5. Emit TextStart for the assistant response.
	assistantMsgID := uuid.New().String()[:12]
	if err := stream.Send(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_TextStart{TextStart: &aiv1.TextStart{MessageId: assistantMsgID}},
	}); err != nil {
		return err
	}

	// 6. Call the model.
	s.logger.Info("Stream: calling model", "history_len", len(history))
	modelReq := model.StreamRequest{
		Messages:     history,
		Tools:        s.tools.ToDefinitions(),
		SystemPrompt: s.defaultSystemPrompt(),
	}
	reader, err := s.model.Stream(ctx, modelReq)
	if err != nil {
		s.logger.Error("Stream: model.Stream failed", "error", err)
		return s.sendStreamError(stream, err)
	}
	s.logger.Info("Stream: model stream opened, pumping events")
	defer reader.Close()

	// 7. Pump model events → ServerEvents.
	var assistantText strings.Builder
	for {
		evt, err := reader.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return s.sendStreamError(stream, err)
		}

		switch evt.Kind {
		case "text_delta":
			assistantText.WriteString(evt.Text)
			s.logger.Debug("Stream: text_delta", "len", len(evt.Text))
			if err := stream.Send(&aiv1.ServerEvent{
				Event: &aiv1.ServerEvent_TextDelta{TextDelta: &aiv1.TextDelta{Delta: evt.Text}},
			}); err != nil {
				return err
			}

		case "tool_call_complete":
			if err := s.handleToolCall(stream, evt.ToolCall); err != nil {
				return err
			}

		case "finish":
			// Handled after the loop.
		}
	}

	s.logger.Info("Stream: model done",
		"assistant_text_len", assistantText.Len(),
		"conv", conv.Name)

	// 8. Emit TextEnd, persist assistant message, emit Done.
	if err := stream.Send(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_TextEnd{TextEnd: &aiv1.TextEnd{}},
	}); err != nil {
		return err
	}

	assistantSeq := int64(nextSeq) + 1
	assistantPartsJSON, _ := marshalParts([]*aiv1.MessagePart{
		{Part: &aiv1.MessagePart_Text{Text: &aiv1.TextPart{Text: assistantText.String()}}},
	})
	_, err = s.queries.CreateMessage(ctx, db.CreateMessageParams{
		ConversationID: conv.ID,
		Name:           assistantMsgID,
		Role:           "assistant",
		Parts:          assistantPartsJSON,
		Sequence:       assistantSeq,
		TokenCount:     int32(estimateTokens(assistantText.String())),
	})
	if err != nil {
		s.logger.Error("failed to persist assistant message", "error", err)
	}
	_ = s.queries.IncrementConversationMessageCount(ctx, conv.ID)

	s.logger.Info("Stream: sending Done", "conv", conv.Name)
	return stream.Send(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_Done{Done: &aiv1.Done{}},
	})
}

// handleToolCall dispatches a tool call to the server registry or forwards to the client.
func (s *Server) handleToolCall(stream aiv1.AiChat_StreamServer, tc *model.ToolCall) error {
	if tc == nil {
		return nil
	}

	isServer := s.tools.IsServerTool(tc.Name)
	if err := stream.Send(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_ToolInputAvailable{ToolInputAvailable: &aiv1.ToolInputAvailable{
			ToolCallId: tc.ID,
			Tool:       tc.Name,
			InputJson:  tc.InputJSON,
			ServerSide: isServer,
		}},
	}); err != nil {
		return err
	}

	if isServer {
		tool := s.tools.Get(tc.Name)
		result, execErr := tool.Execute(stream.Context(), tc.InputJSON)
		if execErr != nil {
			return stream.Send(&aiv1.ServerEvent{
				Event: &aiv1.ServerEvent_ToolError{ToolError: &aiv1.ToolError{
					ToolCallId:   tc.ID,
					ErrorMessage: execErr.Error(),
				}},
			})
		}
		return stream.Send(&aiv1.ServerEvent{
			Event: &aiv1.ServerEvent_ToolOutputAvailable{ToolOutputAvailable: &aiv1.ToolOutputAvailable{
				ToolCallId: tc.ID,
				ResultJson: result,
			}},
		})
	}

	// Client-side tool: the ToolInputAvailable was already sent.
	// Client executes the tool locally and sends a ToolOutput in a new stream.
	return nil
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
	var kept []db.Message
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

	// Convert to model messages.
	msgs := make([]model.Message, 0, len(kept))
	for _, row := range kept {
		msgs = append(msgs, dbMessageToModel(row))
	}
	return msgs, nil
}

func dbMessageToModel(row db.Message) model.Message {
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

func (s *Server) sendStreamError(stream aiv1.AiChat_StreamServer, err error) error {
	st, ok := status.FromError(err)
	if !ok {
		st = status.New(codes.Internal, err.Error())
	}
	_ = stream.Send(&aiv1.ServerEvent{
		Event: &aiv1.ServerEvent_StreamError{StreamError: &aiv1.StreamError{
			Status: st.Proto(),
		}},
	})
	return err
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
