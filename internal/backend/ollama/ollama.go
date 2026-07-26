package ollama

import (
	"context"
	"fmt"

	ollamaapi "github.com/ollama/ollama/api"

	"github.com/MARCAAAAARRON/cude/internal/backend"
)

// Backend implements backend.Backend for Ollama.
type Backend struct {
	client *ollamaapi.Client
	model  string
	cap    backend.Capability
}

// New creates an Ollama backend. endpoint is the Ollama server URL
// (default "http://localhost:11434"). model is the Ollama model tag.
func New(endpoint, model string, contextWindow int) (*Backend, error) {
	client, err := ollamaapi.ClientFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("ollama client init: %w", err)
	}

	b := &Backend{
		client: client,
		model:  model,
		cap: backend.Capability{
			Model:         model,
			Backend:       "ollama",
			Context:       contextWindow,
			SupportsTools: false, // conservative default; can be probed later
			SupportsJSON:  false,
			Tier:          "local",
		},
	}
	return b, nil
}

func (b *Backend) Name() string             { return "ollama" }
func (b *Backend) Model() string            { return b.model }
func (b *Backend) Capability() backend.Capability { return b.cap }
func (b *Backend) Close() error             { return nil }

// Chat streams a response from Ollama. tool definitions are not natively
// supported by most Ollama models, so they are injected into the system prompt
// by the agent's parser layer.
func (b *Backend) Chat(ctx context.Context, msgs []backend.Message, _ []backend.ToolDef) (<-chan backend.StreamChunk, error) {
	ollamaMsgs := make([]ollamaapi.Message, len(msgs))
	for i, m := range msgs {
		ollamaMsgs[i] = ollamaapi.Message{
			Role:    string(m.Role),
			Content: m.Content,
		}
	}

	req := &ollamaapi.ChatRequest{
		Model:    b.model,
		Messages: ollamaMsgs,
	}

	ch := make(chan backend.StreamChunk, 64)

	go func() {
		defer close(ch)
		err := b.client.Chat(ctx, req, func(resp ollamaapi.ChatResponse) error {
			chunk := backend.StreamChunk{
				Delta: resp.Message.Content,
				Done:  resp.Done,
			}
			if resp.Done && resp.EvalCount > 0 && resp.EvalDuration > 0 {
				tps := float64(resp.EvalCount) / resp.EvalDuration.Seconds()
				b.cap.TPS = tps
			}
			select {
			case ch <- chunk:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		})
		if err != nil {
			select {
			case ch <- backend.StreamChunk{Err: err, Done: true}:
			default:
			}
		}
	}()

	return ch, nil
}
