package backend

import "context"

// Capability describes what a backend/model can do, used by the agent core
// and router to adapt behavior (prompt construction, tool-calling strategy).
type Capability struct {
	Model         string  `json:"model"`
	Backend       string  `json:"backend"`
	Context       int     `json:"context"`
	SupportsTools bool    `json:"supports_tools"`
	SupportsJSON  bool    `json:"supports_json"`
	TPS           float64 `json:"tps"`
	Tier          string  `json:"tier"` // "local" or "api"
}

// IsLocal returns true if this is a local-tier model.
func (c Capability) IsLocal() bool {
	return c.Tier == "local"
}

// Role identifies the sender of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a single turn in a conversation.
type Message struct {
	Role      Role       `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolID    string     `json:"tool_id,omitempty"` // set when Role == RoleTool
}

// ToolCall represents a function/tool invocation requested by the model.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded arguments
}

// ToolDef describes a tool that can be invoked by the model. Used to
// construct tool schemas for API models and prompt descriptions for local models.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
}

// StreamChunk is a single piece of a streaming response.
type StreamChunk struct {
	Delta     string     // Text fragment
	ToolCalls []ToolCall // Partial or complete tool calls
	Done      bool       // True on the final chunk
	Err       error      // Non-nil if the stream encountered an error
}

// Backend is the unified interface for all model providers. The agent core
// talks exclusively through this interface; it never knows whether it's
// driving Ollama, Anthropic, OpenAI, LM Studio, or anything else.
type Backend interface {
	// Name returns the backend type identifier (e.g. "ollama", "anthropic").
	Name() string

	// Model returns the model identifier currently in use.
	Model() string

	// Chat sends a conversation and streams back the response. The returned
	// channel will be closed when the response is complete.
	// tools may be nil if no tool definitions should be sent.
	Chat(ctx context.Context, msgs []Message, tools []ToolDef) (<-chan StreamChunk, error)

	// Capability reports what this backend/model can do.
	Capability() Capability

	// Close releases any resources held by the backend.
	Close() error
}
