package anthropic

import (
	"context"
	"fmt"

	anthropicapi "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/marcar/cude/internal/backend"
)

// Backend implements backend.Backend for Anthropic's Claude API.
type Backend struct {
	client *anthropicapi.Client
	model  string
	cap    backend.Capability
}

// New creates an Anthropic backend.
func New(apiKey, model string, contextWindow int) (*Backend, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic: API key is required (set ANTHROPIC_API_KEY or configure api_key)")
	}
	client := anthropicapi.NewClient(option.WithAPIKey(apiKey))

	return &Backend{
		client: &client,
		model:  model,
		cap: backend.Capability{
			Model:         model,
			Backend:       "anthropic",
			Context:       contextWindow,
			SupportsTools: true,
			SupportsJSON:  true,
			Tier:          "api",
		},
	}, nil
}

func (b *Backend) Name() string             { return "anthropic" }
func (b *Backend) Model() string            { return b.model }
func (b *Backend) Capability() backend.Capability { return b.cap }
func (b *Backend) Close() error             { return nil }

// Chat streams a response from the Anthropic Messages API.
func (b *Backend) Chat(ctx context.Context, msgs []backend.Message, tools []backend.ToolDef) (<-chan backend.StreamChunk, error) {
	// Separate system message from conversation messages.
	var systemBlocks []anthropicapi.TextBlockParam
	var apiMsgs []anthropicapi.MessageParam

	for _, m := range msgs {
		switch m.Role {
		case backend.RoleSystem:
			systemBlocks = append(systemBlocks, anthropicapi.TextBlockParam{Text: m.Content})
		case backend.RoleUser:
			apiMsgs = append(apiMsgs, anthropicapi.NewUserMessage(anthropicapi.NewTextBlock(m.Content)))
		case backend.RoleAssistant:
			apiMsgs = append(apiMsgs, anthropicapi.NewAssistantMessage(anthropicapi.NewTextBlock(m.Content)))
		case backend.RoleTool:
			apiMsgs = append(apiMsgs, anthropicapi.NewUserMessage(
				anthropicapi.NewToolResultBlock(m.ToolID, m.Content, false),
			))
		}
	}

	params := anthropicapi.MessageNewParams{
		Model:     anthropicapi.Model(b.model),
		MaxTokens: 4096,
		Messages:  apiMsgs,
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	// Convert tool definitions to Anthropic format.
	if len(tools) > 0 {
		apiTools := make([]anthropicapi.ToolUnionParam, len(tools))
		for i, t := range tools {
			var req []string
			if r, ok := t.Parameters["required"].([]string); ok {
				req = r
			}

			apiTools[i] = anthropicapi.ToolUnionParam{
				OfTool: &anthropicapi.ToolParam{
					Name:        t.Name,
					Description: param.NewOpt(t.Description),
					InputSchema: anthropicapi.ToolInputSchemaParam{
						Properties: t.Parameters["properties"],
						Required:   req,
					},
				},
			}
		}
		params.Tools = apiTools
	}

	stream := b.client.Messages.NewStreaming(ctx, params)

	ch := make(chan backend.StreamChunk, 64)

	go func() {
		defer close(ch)
		var currToolName, currToolID, currArgs string
		for stream.Next() {
			event := stream.Current()

			switch event.Type {
			case "content_block_delta":
				if event.Delta.Type == "text_delta" {
					select {
					case ch <- backend.StreamChunk{Delta: event.Delta.Text}:
					case <-ctx.Done():
						return
					}
				} else if event.Delta.Type == "input_json_delta" {
					currArgs += event.Delta.PartialJSON
				}
			case "content_block_start":
				if event.ContentBlock.Type == "tool_use" {
					currToolName = event.ContentBlock.Name
					currToolID = event.ContentBlock.ID
				}
			case "content_block_stop":
				if currToolName != "" {
					select {
					case ch <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{{
							ID:        currToolID,
							Name:      currToolName,
							Arguments: currArgs,
						}},
					}:
					case <-ctx.Done():
						return
					}
					currToolName = ""
					currToolID = ""
					currArgs = ""
				}
			case "message_stop":
				// Done
			}
		}
		if err := stream.Err(); err != nil {
			select {
			case ch <- backend.StreamChunk{Err: err, Done: true}:
			default:
			}
		}
	}()

	return ch, nil
}
