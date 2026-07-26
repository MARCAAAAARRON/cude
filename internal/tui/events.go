package tui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcar/cude/internal/agent"
)

// AgentMsg is a Bubble Tea wrapper for agent events.
type AgentMsg struct {
	Event agent.Event
}

// waitForAgent returns a tea.Cmd that blocks until an event is available on
// the agent's event channel, then returns it as an AgentMsg.
func waitForAgent(sub <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-sub
		if !ok {
			return nil
		}
		return AgentMsg{Event: e}
	}
}

// startAgent starts the agent processing a user message in the background.
func startAgent(ctx context.Context, a *agent.Agent, msg string) tea.Cmd {
	return func() tea.Msg {
		a.Process(ctx, msg)
		return nil // Process runs asynchronously, events arrive via channel
	}
}
