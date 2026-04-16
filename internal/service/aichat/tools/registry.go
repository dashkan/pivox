package tools

import (
	"context"

	"github.com/dashkan/pivox/internal/service/aichat/model"
)

// Tool is the interface for a server-side tool.
type Tool interface {
	// Definition returns the tool's metadata and JSON Schema.
	Definition() model.ToolDefinition
	// Execute runs the tool with the given JSON input and returns JSON output.
	Execute(ctx context.Context, inputJSON string) (string, error)
}

// Registry holds the set of server-side tools available to the model.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.tools[t.Definition().Name] = t
}

// Get returns a tool by name, or nil if not found.
func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

// IsServerTool returns true if the named tool is registered server-side.
func (r *Registry) IsServerTool(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// ToDefinitions returns all registered tool definitions for the model call.
func (r *Registry) ToDefinitions() []model.ToolDefinition {
	defs := make([]model.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}
