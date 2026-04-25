package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/ollama/ollama/api"
)

// OllamaAdapter implements LanguageModel using Ollama's Chat API.
type OllamaAdapter struct {
	client *api.Client
	model  string
}

// NewOllamaAdapter creates an Ollama adapter for the given base URL and model.
func NewOllamaAdapter(baseURL, modelName string) (*OllamaAdapter, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse ollama URL: %w", err)
	}
	client := api.NewClient(u, http.DefaultClient)
	return &OllamaAdapter{client: client, model: modelName}, nil
}

// Name returns the configured model identifier (e.g. "llama3.1").
func (o *OllamaAdapter) Name() string { return o.model }

func (o *OllamaAdapter) Stream(ctx context.Context, req StreamRequest) (StreamReader, error) {
	ollamaReq := &api.ChatRequest{
		Model:    o.model,
		Messages: toOllamaMessages(req.Messages, req.SystemPrompt),
		Tools:    toOllamaTools(req.Tools),
	}

	ch := make(chan ModelEvent, 16)
	reader := &channelReader{ch: ch}

	go func() {
		defer close(ch)
		err := o.client.Chat(ctx, ollamaReq, func(resp api.ChatResponse) error {
			events := translateResponse(resp)
			for _, ev := range events {
				select {
				case ch <- ev:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
		if err != nil {
			select {
			case ch <- ModelEvent{Kind: "error", Error: err}:
			case <-ctx.Done():
			}
		}
	}()

	return reader, nil
}

func toOllamaMessages(msgs []Message, systemPrompt string) []api.Message {
	var out []api.Message
	if systemPrompt != "" {
		out = append(out, api.Message{Role: "system", Content: systemPrompt})
	}
	for _, m := range msgs {
		ollamaMsg := api.Message{Role: m.Role}
		for _, p := range m.Parts {
			switch p.Type {
			case "text":
				ollamaMsg.Content += p.Text
			case "tool_call":
				if p.ToolCall != nil {
					var args api.ToolCallFunctionArguments
					_ = json.Unmarshal([]byte(p.ToolCall.InputJSON), &args)
					ollamaMsg.ToolCalls = append(ollamaMsg.ToolCalls, api.ToolCall{
						ID:       p.ToolCall.ID,
						Function: api.ToolCallFunction{Name: p.ToolCall.Name, Arguments: args},
					})
				}
			case "tool_result":
				if p.ToolResult != nil {
					ollamaMsg.Content = p.ToolResult.ResultJSON
					ollamaMsg.ToolCallID = p.ToolResult.CallID
					ollamaMsg.ToolName = p.ToolResult.Name
				}
			}
		}
		out = append(out, ollamaMsg)
	}
	return out
}

func toOllamaTools(defs []ToolDefinition) api.Tools {
	tools := make(api.Tools, 0, len(defs))
	for _, d := range defs {
		var params api.ToolFunctionParameters
		_ = json.Unmarshal(d.InputSchema, &params)
		tools = append(tools, api.Tool{
			Type: "function",
			Function: api.ToolFunction{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  params,
			},
		})
	}
	return tools
}

func translateResponse(resp api.ChatResponse) []ModelEvent {
	var events []ModelEvent

	// Text content
	if resp.Message.Content != "" {
		events = append(events, ModelEvent{
			Kind: "text_delta",
			Text: resp.Message.Content,
		})
	}

	// Tool calls
	for _, tc := range resp.Message.ToolCalls {
		argsJSON, _ := json.Marshal(tc.Function.Arguments)
		events = append(events, ModelEvent{
			Kind: "tool_call_complete",
			ToolCall: &ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				InputJSON: string(argsJSON),
			},
		})
	}

	// Done
	if resp.Done {
		events = append(events, ModelEvent{Kind: "finish"})
	}

	return events
}

// channelReader wraps a channel of ModelEvents into a StreamReader.
type channelReader struct {
	ch   <-chan ModelEvent
	once sync.Once
}

func (r *channelReader) Next(ctx context.Context) (ModelEvent, error) {
	select {
	case <-ctx.Done():
		return ModelEvent{}, ctx.Err()
	case ev, ok := <-r.ch:
		if !ok {
			return ModelEvent{}, io.EOF
		}
		if ev.Kind == "error" && ev.Error != nil {
			return ModelEvent{}, ev.Error
		}
		return ev, nil
	}
}

func (r *channelReader) Close() error {
	// Drain the channel to unblock the goroutine.
	r.once.Do(func() {
		go func() {
			for range r.ch {
			}
		}()
	})
	return nil
}
