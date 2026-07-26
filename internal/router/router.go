package router

import (
	"fmt"
	"log"

	"github.com/marcar/cude/internal/backend"
	"github.com/marcar/cude/internal/backend/anthropic"
	"github.com/marcar/cude/internal/backend/ollama"
	"github.com/marcar/cude/internal/backend/openai"
	"github.com/marcar/cude/internal/config"
)

// Router manages backend instances and handles fallback/escalation logic.
type Router struct {
	cfg      config.Config
	backends map[string]backend.Backend
}

// New creates a router and initializes the default backend.
func New(cfg config.Config) (*Router, error) {
	r := &Router{
		cfg:      cfg,
		backends: make(map[string]backend.Backend),
	}

	// Try to initialize the default model.
	_, err := r.GetBackend(cfg.DefaultModel)
	if err != nil {
		log.Printf("Warning: failed to initialize default model %q: %v", cfg.DefaultModel, err)
		// Don't fail completely; they might change it via /model later.
	}

	return r, nil
}

// GetBackend returns an initialized backend for the given model name, creating
// it if necessary.
func (r *Router) GetBackend(modelName string) (backend.Backend, error) {
	if b, ok := r.backends[modelName]; ok {
		return b, nil
	}

	mcfg, err := r.cfg.LookupModel(modelName)
	if err != nil {
		return nil, err
	}

	var b backend.Backend
	switch mcfg.Backend {
	case "ollama":
		b, err = ollama.New(mcfg.Endpoint, mcfg.Model, mcfg.ContextWindow)
	case "anthropic":
		b, err = anthropic.New(mcfg.ResolvedAPIKey(), mcfg.Model, mcfg.ContextWindow)
	case "openai":
		b, err = openai.New(mcfg.Endpoint, mcfg.ResolvedAPIKey(), mcfg.Model, mcfg.ContextWindow, mcfg.Tier)
	default:
		return nil, fmt.Errorf("router: unknown backend type %q for model %q", mcfg.Backend, modelName)
	}

	if err != nil {
		return nil, fmt.Errorf("router: initialize model %q: %w", modelName, err)
	}

	r.backends[modelName] = b
	return b, nil
}

// ModelNames returns a list of all configured model names.
func (r *Router) ModelNames() []string {
	var names []string
	for name := range r.cfg.Models {
		names = append(names, name)
	}
	return names
}

// Close gracefully shuts down all initialized backends.
func (r *Router) Close() error {
	for name, b := range r.backends {
		if err := b.Close(); err != nil {
			log.Printf("Warning: error closing backend %q: %v", name, err)
		}
	}
	return nil
}
