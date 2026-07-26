package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MARCAAAAARRON/cude/internal/backend"
)

// Tool is the interface all individual tools must implement.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// Registry manages a collection of tools.
type Registry struct {
	tools map[string]Tool
	defs  []backend.ToolDef
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
	r.defs = append(r.defs, backend.ToolDef{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.Schema(),
	})
}

// Definitions returns the tool definitions for backend registration.
func (r *Registry) Definitions() []backend.ToolDef {
	return r.defs
}

// Execute looks up a tool by name and executes it with the given JSON args.
func (r *Registry) Execute(ctx context.Context, name string, args string) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		// Offer helpful suggestion if they misspelled it.
		var names []string
		for k := range r.tools {
			names = append(names, k)
		}
		return "", fmt.Errorf("tool %q not found. Available tools: %s", name, strings.Join(names, ", "))
	}
	return t.Execute(ctx, json.RawMessage(args))
}
