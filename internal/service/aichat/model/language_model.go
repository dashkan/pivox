package model

import (
	"context"
	"io"
)

// LanguageModel is the interface for LLM providers.
type LanguageModel interface {
	// Stream starts a streaming model call and returns a reader for events.
	Stream(ctx context.Context, req StreamRequest) (StreamReader, error)

	// Name returns a stable identifier for the underlying model
	// (e.g., "llama3.1", "claude-3-5-sonnet"). Surfaced to clients
	// via `GenerateContentResponse.Model` for billing/observability.
	Name() string
}

// StreamRequest is the input to a model streaming call.
type StreamRequest struct {
	Messages     []Message
	Tools        []ToolDefinition
	SystemPrompt string
	Temperature  float32
}

// Message represents a single message in the conversation history.
type Message struct {
	Role  string // "user" | "assistant" | "system" | "tool"
	Parts []MessagePart
}

// MessagePart represents a single part of a message.
type MessagePart struct {
	Type       string // "text" | "tool_call" | "tool_result" | "image"
	Text       string
	ToolCall   *ToolCall
	ToolResult *ToolResult
	ImageURL   string
}

// ToolCall represents a tool call from the model.
type ToolCall struct {
	ID        string
	Name      string
	InputJSON string
}

// ToolResult represents a tool execution result.
type ToolResult struct {
	CallID     string
	Name       string
	ResultJSON string
	IsError    bool
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema []byte // JSON Schema
	ServerSide  bool   // true = executed by Go, false = forwarded to client
}

// ModelEvent is a single event emitted by the model during streaming.
type ModelEvent struct {
	Kind     string // "text_delta" | "tool_call_start" | "tool_call_delta" | "tool_call_complete" | "finish" | "error"
	Text     string
	ToolCall *ToolCall
	Error    error
}

// StreamReader reads model events from a streaming response.
type StreamReader interface {
	// Next returns the next model event. Returns io.EOF when done.
	Next(ctx context.Context) (ModelEvent, error)
	// Close releases resources associated with the stream.
	Close() error
}

// Ensure io.EOF is accessible to callers.
var EOF = io.EOF
