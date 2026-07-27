package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MARCAAAAARRON/cude/internal/config"
	"github.com/MARCAAAAARRON/cude/internal/router"
	"github.com/MARCAAAAARRON/cude/internal/session"
)

// Command represents a slash command available in the TUI.
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Handler     func(m *Model, args string) string
}

// CommandRegistry holds all registered commands.
type CommandRegistry struct {
	commands map[string]*Command
	ordered  []*Command // for /help display order
}

func NewCommandRegistry(r *router.Router, sm *session.Manager) *CommandRegistry {
	cr := &CommandRegistry{
		commands: make(map[string]*Command),
	}
	cr.registerBuiltins(r, sm)
	return cr
}

func (cr *CommandRegistry) Register(cmd *Command) {
	cr.commands[cmd.Name] = cmd
	for _, alias := range cmd.Aliases {
		cr.commands[alias] = cmd
	}
	cr.ordered = append(cr.ordered, cmd)
}

func (cr *CommandRegistry) Lookup(name string) (*Command, bool) {
	cmd, ok := cr.commands[name]
	return cmd, ok
}

// IsCommand returns true if the input starts with '/'.
func IsCommand(input string) bool {
	return strings.HasPrefix(input, "/")
}

// ParseCommand splits "/cmd args" into ("cmd", "args").
func ParseCommand(input string) (string, string) {
	input = strings.TrimPrefix(input, "/")
	parts := strings.SplitN(input, " ", 2)
	cmd := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return cmd, args
}

func (cr *CommandRegistry) registerBuiltins(r *router.Router, sm *session.Manager) {
	// /help
	cr.Register(&Command{
		Name:        "help",
		Description: "Show all available commands",
		Handler: func(m *Model, args string) string {
			var b strings.Builder
			b.WriteString("╭─── cude commands ───────────────────╮\n")
			for _, cmd := range cr.ordered {
				aliases := ""
				if len(cmd.Aliases) > 0 {
					aliases = " (" + strings.Join(cmd.Aliases, ", ") + ")"
				}
				b.WriteString(fmt.Sprintf("│ /%s%s\n│   %s\n", cmd.Name, aliases, cmd.Description))
			}
			b.WriteString("│\n│ Shortcuts:\n")
			b.WriteString("│   @file — inject file context into prompt\n")
			b.WriteString("│   !cmd  — run shell command inline\n")
			b.WriteString("╰─────────────────────────────────────╯")
			return b.String()
		},
	})

	// /new, /clear
	cr.Register(&Command{
		Name:        "new",
		Aliases:     []string{"clear"},
		Description: "Start a new session (clear conversation)",
		Handler: func(m *Model, args string) string {
			// Auto-save current session before clearing.
			m.autoSaveSession()
			
			m.conversation = nil
			m.currResponse = ""
			if m.agent != nil {
				m.agent.ClearHistory()
			}
			if m.ready {
				m.viewport.SetContent("")
			}
			// Generate a new session ID for the fresh session.
			m.sessionID = uuid.New().String()[:8]
			m.status.latency = 0
			m.status.totalCost = 0
			return IconSpark + " New session started"
		},
	})

	// /model, /models
	cr.Register(&Command{
		Name:        "model",
		Aliases:     []string{"models"},
		Description: "List models or switch: /model <name>",
		Handler: func(m *Model, args string) string {
			if r == nil {
				return IconFail + " No router configured"
			}
			if args == "" {
				// List available models.
				names := r.ModelNames()
				sort.Strings(names)
				var b strings.Builder
				b.WriteString("Available models:\n")
				for _, name := range names {
					marker := "  "
					if name == m.status.model {
						marker = "▸ "
					}
					b.WriteString(fmt.Sprintf("%s%s\n", marker, name))
				}
				b.WriteString("\nUse /model <name> to switch")
				return b.String()
			}
			// Switch to the named model.
			be, err := r.GetBackend(args)
			if err != nil {
				return fmt.Sprintf(IconFail+" %v", err)
			}
			m.agent.SetBackend(be)
			m.status.model = args
			cap := be.Capability()
			m.status.backend = cap.Backend
			return fmt.Sprintf(IconOK+" Switched to model: %s (backend: %s, ctx: %d)", args, cap.Backend, cap.Context)
		},
	})

	// /compact
	cr.Register(&Command{
		Name:        "compact",
		Description: "Compact/summarize the current session context",
		Handler: func(m *Model, args string) string {
			if m.agent != nil {
				m.agent.CompactHistory()
			}
			return IconBox + " Context compacted -- older messages summarized"
		},
	})

	// /undo
	cr.Register(&Command{
		Name:        "undo",
		Description: "Undo the last file edit",
		Handler: func(m *Model, args string) string {
			if m.undoStack == nil || len(m.undoStack) == 0 {
				return "Nothing to undo"
			}
			last := m.undoStack[len(m.undoStack)-1]
			m.undoStack = m.undoStack[:len(m.undoStack)-1]
			err := os.WriteFile(last.Path, last.Original, 0644)
			if err != nil {
				return fmt.Sprintf(IconFail+" Undo failed: %v", err)
			}
			return fmt.Sprintf("↩ Reverted: %s", last.Path)
		},
	})

	// /sessions
	cr.Register(&Command{
		Name:        "sessions",
		Description: "List saved sessions or load one: /sessions <index>",
		Handler: func(m *Model, args string) string {
			if sm == nil {
				return "No session manager configured"
			}
			sessions, err := sm.ListSessions()
			if err != nil {
				return fmt.Sprintf(IconFail+" %v", err)
			}
			if len(sessions) == 0 {
				return "No saved sessions"
			}

			// If an argument is provided, try to load that session.
			if args != "" {
				// Try as 1-based index first.
				if idx, err := strconv.Atoi(args); err == nil && idx >= 1 && idx <= len(sessions) {
					sessInfo := sessions[idx-1]
					sess, err := sm.Load(sessInfo.ID)
					if err != nil || sess == nil {
						return fmt.Sprintf(IconFail+" Failed to load session: %v", err)
					}
					if m.agent != nil {
						m.agent.ClearHistory()
						for _, msg := range sess.History {
							m.agent.AppendHistory(msg)
						}
					}
					m.sessionID = sess.ID
					m.conversation = nil
					m.currResponse = ""
					// Rebuild conversation view from history.
					for _, msg := range sess.History {
						switch msg.Role {
						case "user":
							m.conversation = append(m.conversation, ChatMessage{Role: "you", Text: msg.Content, Color: m.theme.UserLabel})
						case "assistant":
							m.conversation = append(m.conversation, ChatMessage{Text: msg.Content, Color: m.theme.Text})
						}
					}
					m.refreshChat()
					return fmt.Sprintf(IconOK+" Loaded session: %s", sessInfo.Title)
				}

				// Try as session ID.
				sess, err := sm.Load(args)
				if err != nil || sess == nil {
					return fmt.Sprintf(IconFail+" Session %q not found", args)
				}
				if m.agent != nil {
					m.agent.ClearHistory()
					for _, msg := range sess.History {
						m.agent.AppendHistory(msg)
					}
				}
				m.sessionID = sess.ID
				m.conversation = nil
				m.currResponse = ""
				for _, msg := range sess.History {
					switch msg.Role {
					case "user":
						m.conversation = append(m.conversation, ChatMessage{Role: "you", Text: msg.Content, Color: m.theme.UserLabel})
					case "assistant":
						m.conversation = append(m.conversation, ChatMessage{Text: msg.Content, Color: m.theme.Text})
					}
				}
				m.refreshChat()
				return fmt.Sprintf(IconOK+" Loaded session: %s", sess.Title)
			}

			// List all sessions.
			var b strings.Builder
			b.WriteString("Saved sessions:\n")
			for i, s := range sessions {
				marker := "  "
				if s.ID == m.sessionID {
					marker = "▸ "
				}
				age := formatTimeAgo(s.UpdatedAt)
				b.WriteString(fmt.Sprintf("%s%d. [%s] %s (%d msgs, %s)\n", marker, i+1, s.ID, s.Title, s.MsgCount, age))
			}
			b.WriteString("\nUse /sessions <number> to load")
			return b.String()
		},
	})

	// /export
	cr.Register(&Command{
		Name:        "export",
		Description: "Export current session to markdown",
		Handler: func(m *Model, args string) string {
			if m.agent == nil {
				return "No active session"
			}
			history := m.agent.History()
			if len(history) == 0 {
				return "No conversation to export"
			}

			var b strings.Builder
			b.WriteString("# cude session export\n\n")
			b.WriteString(fmt.Sprintf("_Exported: %s_\n\n", time.Now().Format("2006-01-02 15:04:05")))
			for _, msg := range history {
				switch msg.Role {
				case "user":
					b.WriteString(fmt.Sprintf("## User\n\n%s\n\n", msg.Content))
				case "assistant":
					b.WriteString(fmt.Sprintf("## Assistant\n\n%s\n\n", msg.Content))
				case "tool":
					b.WriteString(fmt.Sprintf("### Tool Result\n\n```\n%s\n```\n\n", msg.Content))
				}
			}

			filename := args
			if filename == "" {
				filename = fmt.Sprintf("cude-session-%s.md", time.Now().Format("20060102-150405"))
			}
			err := os.WriteFile(filename, []byte(b.String()), 0644)
			if err != nil {
				return fmt.Sprintf(IconFail+" Export failed: %v", err)
			}
			return fmt.Sprintf(IconFile+" Session exported to %s", filename)
		},
	})

	// /editor
	cr.Register(&Command{
		Name:        "editor",
		Description: "Open $EDITOR for multi-line input",
		Handler: func(m *Model, args string) string {
			editor := os.Getenv("EDITOR")
			if editor == "" {
				if runtime.GOOS == "windows" {
					editor = "notepad"
				} else {
					editor = "vi"
				}
			}
			tmpFile := filepath.Join(os.TempDir(), "cude-input.md")
			_ = os.WriteFile(tmpFile, []byte(""), 0644)

			cmd := exec.Command(editor, tmpFile)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Run()
			if err != nil {
				return fmt.Sprintf(IconFail+" Editor failed: %v", err)
			}

			content, err := os.ReadFile(tmpFile)
			if err != nil {
				return fmt.Sprintf(IconFail+" Could not read editor output: %v", err)
			}
			_ = os.Remove(tmpFile)

			text := strings.TrimSpace(string(content))
			if text == "" {
				return "Editor input was empty"
			}

			// Store it for the caller to dispatch as a message.
			m.pendingEditorInput = text
			return "" // empty = will be dispatched as a user message
		},
	})

	// /details
	cr.Register(&Command{
		Name:        "details",
		Description: "Toggle tool execution details",
		Handler: func(m *Model, args string) string {
			m.showToolDetails = !m.showToolDetails
			if m.showToolDetails {
				return IconEye + " Tool details: ON"
			}
			return IconEye + " Tool details: OFF"
		},
	})

	// /thinking
	cr.Register(&Command{
		Name:        "thinking",
		Description: "Toggle reasoning/thinking block visibility",
		Handler: func(m *Model, args string) string {
			m.showThinking = !m.showThinking
			if m.showThinking {
				return IconBrain + " Thinking blocks: VISIBLE"
			}
			return IconBrain + " Thinking blocks: HIDDEN"
		},
	})

	// /theme, /themes
	cr.Register(&Command{
		Name:        "theme",
		Aliases:     []string{"themes"},
		Description: "Switch theme: /theme <dark|light|neon|mono>",
		Handler: func(m *Model, args string) string {
			themes := map[string]ThemeColors{
				"dark": {
					Primary:    "63",
					Secondary:  "36",
					Muted:      "240",
					Text:       "250",
					Error:      "9",
					Warning:    "220",
					Success:    "43",
					Background: "236",
					UserLabel:  "43",
					BotLabel:   "63",
				},
				"light": {
					Primary:    "27",
					Secondary:  "28",
					Muted:      "242",
					Text:       "234",
					Error:      "160",
					Warning:    "172",
					Success:    "34",
					Background: "253",
					UserLabel:  "28",
					BotLabel:   "27",
				},
				"neon": {
					Primary:    "#d500ff",
					Secondary:  "46",
					Muted:      "240",
					Text:       "255",
					Error:      "196",
					Warning:    "226",
					Success:    "46",
					Background: "232",
					UserLabel:  "46",
					BotLabel:   "#d500ff",
				},
				"mono": {
					Primary:    "255",
					Secondary:  "245",
					Muted:      "240",
					Text:       "250",
					Error:      "255",
					Warning:    "245",
					Success:    "255",
					Background: "234",
					UserLabel:  "255",
					BotLabel:   "245",
				},
			}

			if args == "" {
				var b strings.Builder
				b.WriteString("Available themes:\n")
				for name := range themes {
					marker := "  "
					if name == m.currentTheme {
						marker = "▸ "
					}
					b.WriteString(fmt.Sprintf("%s%s\n", marker, name))
				}
				b.WriteString("\nUse /theme <name> to switch")
				return b.String()
			}

			t, ok := themes[strings.ToLower(args)]
			if !ok {
				return fmt.Sprintf(IconFail+" Unknown theme %q. Available: dark, light, neon, mono", args)
			}
			m.theme = t
			m.currentTheme = strings.ToLower(args)
			return fmt.Sprintf(IconPaint+" Theme set to: %s", args)
		},
	})

	// /mode
	cr.Register(&Command{
		Name:        "mode",
		Description: "Switch mode: /mode <architect|execute>",
		Handler: func(m *Model, args string) string {
			if m.agent == nil {
				return IconFail + " No active agent"
			}
			
			mode := strings.ToLower(args)
			if mode == "" {
				return fmt.Sprintf("Current mode: %s. Use /mode <architect|execute> to switch.", m.agent.Mode())
			}
			
			if mode != "architect" && mode != "execute" {
				return fmt.Sprintf(IconFail + " Unknown mode %q. Available: architect, execute", mode)
			}
			
			m.agent.SetMode(mode)
			
			if mode == "architect" {
				return IconSpark + " Mode set to: architect (Read-only planning mode)"
			}
			return IconSpark + " Mode set to: execute (Autonomous editing mode)"
		},
	})

	// /cost
	cr.Register(&Command{
		Name:        "cost",
		Description: "Toggle cost/latency dashboard in status bar",
		Handler: func(m *Model, args string) string {
			m.showCost = !m.showCost
			if m.showCost {
				return IconCoin + " Cost dashboard: ON"
			}
			return IconCoin + " Cost dashboard: OFF"
		},
	})

	// /exit, /quit, /q
	cr.Register(&Command{
		Name:        "exit",
		Aliases:     []string{"quit", "q"},
		Description: "Exit cude",
		Handler: func(m *Model, args string) string {
			// Auto-save before exiting.
			m.autoSaveSession()
			m.quitting = true
			return ""
		},
	})
	// /add-model
	cr.Register(&Command{
		Name:        "add-model",
		Description: "Add a new model configuration interactively",
		Handler: func(m *Model, args string) string {
			if m.config == nil {
				return IconFail + " Config is not available."
			}
			m.inWizard = true
			m.wizardMode = "add-model"
			m.wizardStep = 1
			m.tempModel = config.ModelConfig{}
			m.tempName = ""
			m.addMessage("Welcome to the Add Model wizard! (Type /cancel to abort at any time)", m.theme.Warning)
			m.addMessage("What name would you like to give this model? (e.g. 'my-local-llama')", m.theme.BotLabel)
			return ""
		},
	})

	// /remove-model
	cr.Register(&Command{
		Name:        "remove-model",
		Description: "Remove a model configuration",
		Handler: func(m *Model, args string) string {
			if m.config == nil {
				return IconFail + " Config is not available."
			}
			if args != "" {
				// Remove immediately without wizard if name provided
				_, err := m.config.LookupModel(args)
				if err != nil {
					return fmt.Sprintf(IconFail + " Model '%s' not found.", args)
				}
				m.config.RemoveModel(args)
				err = m.config.Save()
				if err != nil {
					return fmt.Sprintf(IconFail + " Failed to save config: %v", err)
				}
				return fmt.Sprintf(IconOK + " Removed model '%s' and saved.", args)
			}
			
			m.inWizard = true
			m.wizardMode = "remove-model"
			m.wizardStep = 1
			m.addMessage("Welcome to the Remove Model wizard! (Type /cancel to abort)", m.theme.Warning)
			
			var b strings.Builder
			b.WriteString("Available models to remove:\n")
			for k := range m.config.Models {
				b.WriteString("  - " + k + "\n")
			}
			b.WriteString("\nWhich model would you like to remove?")
			m.addMessage(b.String(), m.theme.BotLabel)
			return ""
		},
	})
}

// formatTimeAgo returns a human-friendly relative time string like "2m ago", "3h ago", "1d ago".
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
