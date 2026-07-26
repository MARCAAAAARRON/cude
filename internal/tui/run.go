package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/MARCAAAAARRON/cude/internal/agent"
	"github.com/MARCAAAAARRON/cude/internal/router"
)

// Run launches the TUI with the given agent core. It blocks until the TUI exits.
func Run(ctx context.Context, a *agent.Agent, r *router.Router) error {
	m := New(ctx, a)
	m.SetRouter(r)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
