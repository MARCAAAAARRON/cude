package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/MARCAAAAARRON/cude/internal/backend"
	"github.com/MARCAAAAARRON/cude/internal/config"
)

var (
	ErrMaxIterations = errors.New("agent: maximum iterations reached without completing task")
	ErrNoBackend     = errors.New("agent: no backend configured")
)

// Event is emitted by the agent to notify the TUI of state changes.
type Event interface{ agentEvent() }

// StreamTokenEvent carries a text fragment from the model.
type StreamTokenEvent struct{ Token string }

// ToolCallEvent indicates the agent is invoking a tool.
type ToolCallEvent struct {
	Name string
	Args string
}

// ToolResultEvent carries the result of a tool invocation.
type ToolResultEvent struct {
	Name   string
	Result string
}

// ApprovalRequest asks the TUI for user confirmation before a side-effect.
type ApprovalRequest struct {
	Type     string // "file_write" or "shell"
	Preview  string // diff or command to display
	Response chan bool
}

// DoneEvent signals the agent has finished processing a request.
type DoneEvent struct{ Error error }

// StatusEvent updates the status bar.
type StatusEvent struct {
	Model   string
	Backend string
	CtxUsed string
}

// EscalationEvent signals that the agent wants to switch to a stronger model.
type EscalationEvent struct {
	TargetModel string
	Reason      string
}

func (StreamTokenEvent) agentEvent()  {}
func (ToolCallEvent) agentEvent()     {}
func (ToolResultEvent) agentEvent()   {}
func (ApprovalRequest) agentEvent()   {}
func (DoneEvent) agentEvent()         {}
func (StatusEvent) agentEvent()       {}
func (EscalationEvent) agentEvent()   {}

// ToolExecutor is the interface for executing tools.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args string) (string, error)
	Definitions() []backend.ToolDef
}

// Agent is the core agentic loop — it reasons, acts, and observes in a cycle
// until the task is complete or limits are reached.
type Agent struct {
	cfg      config.AgentConfig
	be       backend.Backend
	tools    ToolExecutor
	parser   *Parser
	ctxSched *ContextScheduler
	history  []backend.Message
	events   chan Event
	mu       sync.Mutex

	// Escalation tracking.
	consecutiveFailures int
}

// New creates an Agent with the given backend and tools.
func New(cfg config.AgentConfig, be backend.Backend, tools ToolExecutor) *Agent {
	return &Agent{
		cfg:      cfg,
		be:       be,
		tools:    tools,
		parser:   NewParser(),
		ctxSched: NewContextScheduler(be.Capability()),
		history:  make([]backend.Message, 0, 64),
		events:   make(chan Event, 128),
	}
}

// Events returns the channel the TUI should read from.
func (a *Agent) Events() <-chan Event { return a.events }

// SetBackend swaps the model/backend (e.g. on escalation or user /model command).
func (a *Agent) SetBackend(be backend.Backend) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.be = be
	a.ctxSched = NewContextScheduler(be.Capability())
	a.consecutiveFailures = 0
}

// Process handles a user message asynchronously. The caller should read
// events from Events() to receive streaming output and status updates.
func (a *Agent) Process(ctx context.Context, userMsg string) {
	go func() {
		err := a.runLoop(ctx, userMsg)
		a.emit(DoneEvent{Error: err})
	}()
}

// History returns a copy of the current message history.
func (a *Agent) History() []backend.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	cp := make([]backend.Message, len(a.history))
	copy(cp, a.history)
	return cp
}

// ClearHistory wipes the current conversation history.
func (a *Agent) ClearHistory() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = make([]backend.Message, 0, 64)
}

// AppendHistory adds a message to the history (used when restoring a saved session).
func (a *Agent) AppendHistory(msg backend.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = append(a.history, msg)
}

// CompactHistory summarizes or removes older messages to save context space.
func (a *Agent) CompactHistory() {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Naive compaction for now: keep system and last 4 messages.
	if len(a.history) > 4 {
		a.history = a.history[len(a.history)-4:]
	}
}

// SystemPrompt returns the system prompt, adjusted for model tier.
func (a *Agent) SystemPrompt() string {
	cap := a.be.Capability()
	if cap.IsLocal() {
		return localSystemPrompt(a.tools.Definitions())
	}
	return apiSystemPrompt()
}

func (a *Agent) runLoop(ctx context.Context, userMsg string) error {
	a.mu.Lock()
	a.history = append(a.history, backend.Message{Role: backend.RoleUser, Content: userMsg})
	a.mu.Unlock()

	for iteration := 0; iteration < a.cfg.MaxIterations; iteration++ {
		// 1. Build the prompt with context budget.
		a.mu.Lock()
		systemMsg := a.SystemPrompt()
		prompt := a.ctxSched.Build(systemMsg, a.history)
		cap := a.be.Capability()
		a.mu.Unlock()

		a.emit(StatusEvent{
			Model:   cap.Model,
			Backend: cap.Backend,
			CtxUsed: a.ctxSched.UsageSummary(),
		})

		// 2. Stream from model.
		var tools []backend.ToolDef
		if cap.SupportsTools {
			tools = a.tools.Definitions()
		}

		ch, err := a.be.Chat(ctx, prompt, tools)
		if err != nil {
			return fmt.Errorf("agent: chat failed: %w", err)
		}

		// 3. Collect response.
		var fullResponse strings.Builder
		var toolCalls []backend.ToolCall

		for chunk := range ch {
			if chunk.Err != nil {
				return fmt.Errorf("agent: stream error: %w", chunk.Err)
			}
			if chunk.Delta != "" {
				fullResponse.WriteString(chunk.Delta)
				a.emit(StreamTokenEvent{Token: chunk.Delta})
			}
			if len(chunk.ToolCalls) > 0 {
				toolCalls = append(toolCalls, chunk.ToolCalls...)
			}
		}

		responseText := fullResponse.String()

		// 4. Parse for actions (dual-mode: native tool-calls or text-parsed).
		var actions []ParsedAction
		if len(toolCalls) > 0 {
			// API model returned native tool calls.
			for _, tc := range toolCalls {
				actions = append(actions, ParsedAction{
					ToolName: tc.Name,
					ToolArgs: tc.Arguments,
					ToolID:   tc.ID,
				})
			}
			a.consecutiveFailures = 0
		} else if cap.IsLocal() {
			// Try text-based parsing for local models.
			actions, err = a.parser.ExtractActions(responseText)
			if err != nil {
				a.consecutiveFailures++
				if a.shouldEscalate() {
					a.emit(EscalationEvent{
						TargetModel: a.cfg.EscalateTarget,
						Reason:      fmt.Sprintf("Local model failed %d times consecutively", a.consecutiveFailures),
					})
					// Return so the TUI can handle the switch; the request
					// will be retried on the new backend.
					return nil
				} else {
					a.emit(StreamTokenEvent{Token: "\n\n⚠ Local model is struggling. Consider switching to an API model with /model.\n"})
				}
			} else {
				a.consecutiveFailures = 0
			}
		}

		// 5. Record assistant message.
		a.mu.Lock()
		assistantMsg := backend.Message{Role: backend.RoleAssistant, Content: responseText}
		if len(toolCalls) > 0 {
			assistantMsg.ToolCalls = toolCalls
		}
		a.history = append(a.history, assistantMsg)
		a.mu.Unlock()

		// 6. If no actions → the model gave a final answer.
		if len(actions) == 0 {
			return nil
		}

		// 7. Execute each tool action.
		for _, action := range actions {
			a.emit(ToolCallEvent{Name: action.ToolName, Args: action.ToolArgs})

			// Check if approval is needed.
			if a.needsApproval(action.ToolName) {
				approved := a.requestApproval(action)
				if !approved {
					a.mu.Lock()
					a.history = append(a.history, backend.Message{
						Role:    backend.RoleTool,
						Content: "User denied this action.",
						ToolID:  action.ToolID,
					})
					a.mu.Unlock()
					continue
				}
			}

			result, execErr := a.tools.Execute(ctx, action.ToolName, action.ToolArgs)
			if execErr != nil {
				result = fmt.Sprintf("Error: %v", execErr)
			}

			a.emit(ToolResultEvent{Name: action.ToolName, Result: result})

			a.mu.Lock()
			a.history = append(a.history, backend.Message{
				Role:    backend.RoleTool,
				Content: result,
				ToolID:  action.ToolID,
			})
			a.mu.Unlock()
		}
	}

	return ErrMaxIterations
}

func (a *Agent) needsApproval(toolName string) bool {
	switch toolName {
	case "file_write", "diff_apply":
		return a.cfg.ApproveWrites
	case "shell_exec":
		return a.cfg.ApproveShell
	default:
		return false
	}
}

func (a *Agent) requestApproval(action ParsedAction) bool {
	resp := make(chan bool, 1)
	a.emit(ApprovalRequest{
		Type:     action.ToolName,
		Preview:  action.ToolArgs,
		Response: resp,
	})
	return <-resp
}

func (a *Agent) shouldEscalate() bool {
	return a.cfg.AutoEscalate && a.consecutiveFailures >= a.cfg.EscalateThreshold
}

func (a *Agent) emit(e Event) {
	select {
	case a.events <- e:
	default:
		// Drop event if channel is full (non-blocking for agent loop).
	}
}

// localSystemPrompt generates a system prompt that embeds tool descriptions
// in text format for models that don't support native function-calling.
func localSystemPrompt(tools []backend.ToolDef) string {
	var b strings.Builder
	b.WriteString("You are cude, a coding assistant that helps users read, edit, and manage code.\n\n")
	b.WriteString("When you need to perform an action, respond with EXACTLY this format:\n")
	b.WriteString("ACTION: <tool_name>\n")
	b.WriteString("INPUT: <json_arguments>\n")
	b.WriteString("---\n\n")
	b.WriteString("EXAMPLE — creating a new file:\n")
	b.WriteString("ACTION: file_write\n")
	b.WriteString("INPUT: {\"path\": \"hello.py\", \"search\": \"\", \"replace\": \"print('Hello!')\"}\n")
	b.WriteString("---\n\n")
	b.WriteString("Available tools:\n\n")
	for _, t := range tools {
		b.WriteString(fmt.Sprintf("## %s\n%s\n", t.Name, t.Description))
		if t.Parameters != nil {
			paramJSON, err := json.MarshalIndent(t.Parameters, "", "  ")
			if err == nil {
				b.WriteString(fmt.Sprintf("Parameters: %s\n", string(paramJSON)))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("IMPORTANT RULES:\n")
	b.WriteString("1. Always include ALL required parameters. For file_write, you MUST include \"path\".\n")
	b.WriteString("2. Always use the ACTION/INPUT/--- format when you need to read or modify files.\n")
	b.WriteString("3. Never guess file contents — read first, then edit.\n")
	b.WriteString("4. If you don't need a tool, just respond with your answer directly.\n")
	return b.String()
}

func apiSystemPrompt() string {
	return "You are cude, a coding assistant that helps users read, edit, and manage code. " +
		"Use the provided tools to interact with the filesystem and shell. " +
		"Always read files before editing them. Use diff-based edits, never rewrite entire files. " +
		"Ask for clarification when the user's request is ambiguous."
}
