package openai

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	openaiapi "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/MARCAAAAARRON/cude/internal/backend"
)

// Backend implements backend.Backend for any OpenAI-compatible endpoint.
// This is the universal adapter that covers: OpenAI, LM Studio, OpenRouter,
// vLLM, TGI, and any other server that speaks the OpenAI chat completions API.
type Backend struct {
	client *openaiapi.Client
	model  string
	cap    backend.Capability
}

// New creates an OpenAI-compatible backend.
// endpoint is the base URL (e.g. "http://localhost:1234/v1" for LM Studio).
// tier should be "local" for LM Studio / local servers, "api" for cloud providers.
func New(endpoint, apiKey, model string, contextWindow int, tier string) (*Backend, error) {
	opts := []option.RequestOption{}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err == nil && (u.Path == "" || u.Path == "/") {
			// Auto-append /v1 for common local OpenAI-compatible servers (LM Studio, vLLM, etc.)
			endpoint = strings.TrimRight(endpoint, "/") + "/v1"
		}
		opts = append(opts, option.WithBaseURL(endpoint))
	}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}

	client := openaiapi.NewClient(opts...)

	supportsTools := tier == "api" // assume API models support tools; local may not

	return &Backend{
		client: &client,
		model:  model,
		cap: backend.Capability{
			Model:         model,
			Backend:       "openai",
			Context:       contextWindow,
			SupportsTools: supportsTools,
			SupportsJSON:  true,
			Tier:          tier,
		},
	}, nil
}

func (b *Backend) Name() string             { return "openai" }
func (b *Backend) Model() string            { return b.model }
func (b *Backend) Capability() backend.Capability { return b.cap }
func (b *Backend) Close() error             { return nil }

// Chat streams a response from the OpenAI-compatible endpoint.
func (b *Backend) Chat(ctx context.Context, msgs []backend.Message, tools []backend.ToolDef) (<-chan backend.StreamChunk, error) {
	apiMsgs := make([]openaiapi.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case backend.RoleSystem:
			apiMsgs = append(apiMsgs, openaiapi.SystemMessage(m.Content))
		case backend.RoleUser:
			apiMsgs = append(apiMsgs, openaiapi.UserMessage(m.Content))
		case backend.RoleAssistant:
			if len(m.ToolCalls) > 0 {
				tc := make([]openaiapi.ChatCompletionMessageToolCallParam, len(m.ToolCalls))
				for i, c := range m.ToolCalls {
					tc[i] = openaiapi.ChatCompletionMessageToolCallParam{
						ID:   c.ID,
						Type: "function",
						Function: openaiapi.ChatCompletionMessageToolCallFunctionParam{
							Name:      c.Name,
							Arguments: c.Arguments,
						},
					}
				}
				apiMsgs = append(apiMsgs, openaiapi.ChatCompletionMessageParamUnion{
					OfAssistant: &openaiapi.ChatCompletionAssistantMessageParam{
						Content:   openaiapi.ChatCompletionAssistantMessageParamContentUnion{OfString: openaiapi.String(m.Content)},
						ToolCalls: tc,
					},
				})
			} else {
				apiMsgs = append(apiMsgs, openaiapi.AssistantMessage(m.Content))
			}
		case backend.RoleTool:
			apiMsgs = append(apiMsgs, openaiapi.ToolMessage(m.ToolID, m.Content))
		}
	}

	params := openaiapi.ChatCompletionNewParams{
		Model:    b.model,
		Messages: apiMsgs,
	}

	// Attach tools if the backend supports them and we have definitions.
	if b.cap.SupportsTools && len(tools) > 0 {
		apiTools := make([]openaiapi.ChatCompletionToolParam, len(tools))
		for i, t := range tools {
			schemaBytes, _ := json.Marshal(t.Parameters)
			var schemaMap openaiapi.FunctionParameters
			json.Unmarshal(schemaBytes, &schemaMap)

			apiTools[i] = openaiapi.ChatCompletionToolParam{
				Type: "function",
				Function: openaiapi.FunctionDefinitionParam{
					Name:        t.Name,
					Description: openaiapi.String(t.Description),
					Parameters:  schemaMap,
				},
			}
		}
		params.Tools = apiTools
	}

	stream := b.client.Chat.Completions.NewStreaming(ctx, params)
	acc := openaiapi.ChatCompletionAccumulator{}

	ch := make(chan backend.StreamChunk, 64)

	go func() {
		defer close(ch)
		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)

			// Stream text delta.
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				select {
				case ch <- backend.StreamChunk{Delta: chunk.Choices[0].Delta.Content}:
				case <-ctx.Done():
					return
				}
			}

			// Check for completed tool calls.
			if tool, ok := acc.JustFinishedToolCall(); ok {
				select {
				case ch <- backend.StreamChunk{
					ToolCalls: []backend.ToolCall{{
						ID:        tool.ID,
						Name:      tool.Name,
						Arguments: tool.Arguments,
					}},
				}:
				case <-ctx.Done():
					return
				}
			}
		}

		// Final done chunk.
		finishReason := ""
		if len(acc.Choices) > 0 {
			finishReason = string(acc.Choices[0].FinishReason)
		}
		_ = finishReason

		select {
		case ch <- backend.StreamChunk{Done: true}:
		case <-ctx.Done():
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


