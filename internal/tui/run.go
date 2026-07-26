package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/MARCAAAAARRON/cude/internal/agent"
	"github.com/MARCAAAAARRON/cude/internal/router"
	"github.com/MARCAAAAARRON/cude/internal/session"
)

// Run launches the TUI with the given agent core. It blocks until the TUI exits.
func Run(ctx context.Context, a *agent.Agent, r *router.Router, sm *session.Manager) error {
	m := New(ctx, a)
	m.SetSessionManager(sm)
	m.SetRouter(r)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
