package mascot

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type BackendState int

const (
	StateLocal BackendState = iota
	StateAPI
	StateIdle
)

var stateColors = map[BackendState]lipgloss.Color{
	StateLocal: lipgloss.Color("63"),
	StateAPI:   lipgloss.Color("36"),
	StateIdle:  lipgloss.Color("245"),
}

func Render(state BackendState) string {
	c := stateColors[state]
	style := lipgloss.NewStyle().Foreground(c)
	var b strings.Builder
	for _, l := range Lines() {
		b.WriteString(style.Render(l))
		b.WriteByte('\n')
	}
	return b.String()
}

// Thinking returns a frame of the mascot where the brackets shift,
// used as the streaming indicator. phase 0..2 cycles three frames.
func Thinking(state BackendState, phase int) string {
	c := stateColors[state]
	style := lipgloss.NewStyle().Foreground(c)
	frames := []string{
		`|-- --|
_-_| |||_-
| |_---| |`,
		`||-- --|
_-_| |||_-
| |_---| |`,
		`|-- --||
_-_| |||_-
| |_---| |`,
	}
	f := frames[phase%len(frames)]
	return style.Render(f)
}

// AnimatePhase maps a time.Time to a 0..2 phase so callers can drive the
// animation off the TUI tick without owning their own counter.
func AnimatePhase(t time.Time) int {
	return int(t.UnixNano()/int64(180*time.Millisecond)) % 3
}
