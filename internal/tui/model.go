package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	
	"github.com/marcar/cude/internal/agent"
	"github.com/marcar/cude/internal/router"
)

const (
	splashDuration = 1200 * time.Millisecond
	statusLine     = "[cude] model=%s  backend=%s  ctx=%s  %s"
)

type Model struct {
	ctx           context.Context
	cancel        context.CancelFunc
	agent         *agent.Agent
	
	viewport      viewport.Model
	textInput     textinput.Model
	
	conversation  []string
	currResponse  string
	
	status        status
	width         int
	height        int
	ready         bool
	splashUntil   time.Time
	quitting      bool
	
	// Approval state
	waitingApprove bool
	approveReq     agent.ApprovalRequest

	// Slash Commands state
	cmdRegistry        *CommandRegistry
	pendingEditorInput string
	
	// Toggles & Theme
	showToolDetails bool
	showThinking    bool
	showCost        bool
	theme           ThemeColors
	currentTheme    string
	
	// Undo
	undoStack []UndoItem
	
	// Sessions
	sessionMgr interface{ ListSessions() ([]string, error) } // Minimal stub for now

	// Layout State
	showSidebar  bool
	sidebarWidth int
}

type UndoItem struct {
	Path     string
	Original []byte
}

type ThemeColors struct {
	Primary    string
	Secondary  string
	Muted      string
	Text       string
	Error      string
	Warning    string
	Success    string
	Background string
	UserLabel  string
	BotLabel   string
}

type AgentState int

const (
	StateIdle AgentState = iota
	StateAPI
	StateLocal
)

type status struct {
	model   string
	backend string
	ctxUsed string
	state   AgentState
}

func New(ctx context.Context, a *agent.Agent) Model {
	ctx, cancel := context.WithCancel(ctx)
	
	ti := textinput.New()
	ti.Placeholder = "Ask cude anything (or /help)..."
	ti.Focus()
	
	defaultTheme := ThemeColors{
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
	}
	
	return Model{
		ctx:    ctx,
		cancel: cancel,
		agent:  a,
		
		textInput: ti,
		theme:     defaultTheme,
		currentTheme: "neon",
		
		status: status{
			model:   "loading...",
			backend: "loading...",
			ctxUsed: "0/?",
			state:   StateIdle,
		},
		splashUntil:  time.Now().Add(splashDuration),
		showSidebar:  true,
		sidebarWidth: 42,
	}
}

// SetRouter sets the router for slash commands (needs to be called from main.go)
func (m *Model) SetRouter(r *router.Router) {
	m.cmdRegistry = NewCommandRegistry(r)
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.Tick(time.Until(m.splashUntil), func(time.Time) tea.Msg { return splashDoneMsg{} }),
		tea.Tick(time.Millisecond*180, func(time.Time) tea.Msg { return tickMsg{} }),
		waitForAgent(m.agent.Events()),
	)
}

type splashDoneMsg struct{}
type tickMsg struct{}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.waitingApprove {
			switch msg.String() {
			case "y", "Y":
				m.approveReq.Response <- true
				m.waitingApprove = false
				m.conversation = append(m.conversation, IconOK+" Approved")
				m.viewport.SetContent(strings.Join(m.conversation, "\n\n"))
				m.viewport.GotoBottom()
			case "n", "N":
				m.approveReq.Response <- false
				m.waitingApprove = false
				m.conversation = append(m.conversation, IconFail+" Denied")
				m.viewport.SetContent(strings.Join(m.conversation, "\n\n"))
				m.viewport.GotoBottom()
			case "ctrl+c":
				m.quitting = true
				m.cancel()
				return m, tea.Quit
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			m.cancel()
			return m, tea.Quit
		case "ctrl+s":
			m.showSidebar = !m.showSidebar
			m.recalcLayout()
			return m, nil
		case "enter":
			val := strings.TrimSpace(m.textInput.Value())
			if val == "" {
				// Handle editor input if present
				if m.pendingEditorInput != "" {
					val = m.pendingEditorInput
					m.pendingEditorInput = ""
				}
			}

			if val != "" {
				m.textInput.SetValue("")
				
				if IsCommand(val) {
					if m.cmdRegistry != nil {
						cmdName, args := ParseCommand(val)
						if cmd, ok := m.cmdRegistry.Lookup(cmdName); ok {
							result := cmd.Handler(&m, args) // passed pointer to model
							if m.quitting {
								m.cancel()
								return m, tea.Quit
							}
							if result != "" {
								m.conversation = append(m.conversation, lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Text)).Render(result))
								m.viewport.SetContent(strings.Join(m.conversation, "\n\n"))
								m.viewport.GotoBottom()
							}
						} else {
							m.conversation = append(m.conversation, lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Error)).Render(fmt.Sprintf(IconFail+" Unknown command: /%s. Type /help for a list.", cmdName)))
							m.viewport.SetContent(strings.Join(m.conversation, "\n\n"))
							m.viewport.GotoBottom()
						}
					}
					return m, nil
				}

				m.conversation = append(m.conversation, lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.UserLabel)).Render("you: ")+val)
				m.viewport.SetContent(strings.Join(m.conversation, "\n\n"))
				m.viewport.GotoBottom()
				
				m.status.state = StateAPI // TODO: get from actual capability
				m.currResponse = ""
				
				cmds = append(cmds, startAgent(m.ctx, m.agent, val))
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()

	case splashDoneMsg:
		m.splashUntil = time.Time{}

	case tickMsg:
		cmds = append(cmds, tea.Tick(time.Millisecond*180, func(time.Time) tea.Msg { return tickMsg{} }))

	case AgentMsg:
		switch e := msg.Event.(type) {
		case agent.StreamTokenEvent:
			m.currResponse += e.Token
			// Rebuild the conversation view with the partial response.
			viewContent := strings.Join(m.conversation, "\n\n")
			if m.currResponse != "" {
				viewContent += "\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render(m.currResponse)
			}
			m.viewport.SetContent(viewContent)
			m.viewport.GotoBottom()
			
		case agent.ToolCallEvent:
			callStr := fmt.Sprintf("🔧 Running tool: %s\n%s", e.Name, e.Args)
			styled := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(callStr)
			m.conversation = append(m.conversation, styled)
			m.currResponse = "" // clear current streaming response buffer
			m.viewport.SetContent(strings.Join(m.conversation, "\n\n"))
			m.viewport.GotoBottom()

		case agent.ToolResultEvent:
			// Optionally log result, often too long.
			
		case agent.ApprovalRequest:
			m.waitingApprove = true
			m.approveReq = e
			reqStr := fmt.Sprintf("⚠️ The agent wants to execute %s.\n\n%s\n\nAllow this? (y/n)", e.Type, e.Preview)
			styled := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render(reqStr)
			m.conversation = append(m.conversation, styled)
			m.viewport.SetContent(strings.Join(m.conversation, "\n\n"))
			m.viewport.GotoBottom()
			
		case agent.StatusEvent:
			m.status.model = e.Model
			m.status.backend = e.Backend
			m.status.ctxUsed = e.CtxUsed
			
		case agent.DoneEvent:
			m.status.state = StateIdle
			if m.currResponse != "" {
				m.conversation = append(m.conversation, lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Text)).Render(m.currResponse))
				m.currResponse = ""
			}
			if e.Error != nil {
				errStr := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Error)).Render(fmt.Sprintf(IconFail+" Error: %v", e.Error))
				m.conversation = append(m.conversation, errStr)
			}
			m.viewport.SetContent(strings.Join(m.conversation, "\n\n"))
			m.viewport.GotoBottom()
		}
		
		// Continue polling for agent events
		cmds = append(cmds, waitForAgent(m.agent.Events()))
	}

	if !m.waitingApprove {
		var tiCmd tea.Cmd
		m.textInput, tiCmd = m.textInput.Update(msg)
		cmds = append(cmds, tiCmd)
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) recalcLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	// Calculate widths
	chatWidth := m.width
	if m.showSidebar {
		chatWidth = m.width - m.sidebarWidth
	}
	// Subtract borders (2 for left/right border, maybe 2 for padding)
	chatWidth -= 4 

	if chatWidth < 10 {
		chatWidth = 10
	}

	// Calculate heights
	// Input area height: 3 lines for the box
	inputHeight := 3 
	vpHeight := m.height - inputHeight - 2 // 2 for top/bottom borders of chat

	if vpHeight < 1 {
		vpHeight = 1
	}

	if !m.ready {
		m.viewport = viewport.New(chatWidth, vpHeight)
		m.ready = true
	} else {
		m.viewport.Width = chatWidth
		m.viewport.Height = vpHeight
	}
	m.textInput.Width = m.width - 4
}

func (m Model) View() string {
	if !m.ready {
		return "starting cude..."
	}
	if time.Now().Before(m.splashUntil) {
		return m.splashView()
	}
	return m.mainView()
}

func (m Model) splashView() string {
	var b strings.Builder
	b.WriteString("\n\n\n\n")
	b.WriteString(m.renderBanner())
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Muted)).Align(lipgloss.Center).Render("hybrid local/api coding agent"))
	b.WriteString("\n")
	return lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(b.String())
}

func (m Model) mainView() string {
	sidebar := ""
	if m.showSidebar {
		sidebar = m.sidebarView()
	}
	
	chat := m.chatView()
	topPart := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, chat)
	inputPart := m.inputView()
	
	return lipgloss.JoinVertical(lipgloss.Left, topPart, inputPart)
}

func (m Model) sidebarView() string {
	width := m.sidebarWidth - 2 // account for borders

	// Banner
	bannerStr := m.renderSmallBanner()
	bannerBox := lipgloss.NewStyle().Align(lipgloss.Center).Width(width).Render(bannerStr)

	// Context progress bar
	ctxBar := m.renderProgressBar(m.status.ctxUsed, width-4)

	// Build the sections
	var b strings.Builder
	b.WriteString(bannerBox + "\n")
	
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Primary)).Bold(true)
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Text))
	
	b.WriteString(titleStyle.Render("MODEL INFO") + "\n")
	b.WriteString("├─ Backend: " + valStyle.Render(m.status.backend) + "\n")
	b.WriteString("├─ Model:   " + valStyle.Render(m.status.model) + "\n")
	b.WriteString("└─ State:   " + valStyle.Render(fmt.Sprintf("%v", m.status.state)) + "\n\n")
	
	b.WriteString(titleStyle.Render("WORKSPACE") + "\n")
	b.WriteString("├─ Proj: " + valStyle.Render("cude (go)") + "\n")
	b.WriteString("└─ Git:  " + valStyle.Render("main") + "\n\n")
	
	b.WriteString(titleStyle.Render("PERFORMANCE") + "\n")
	b.WriteString("├─ Ctx: " + ctxBar + "\n")
	b.WriteString("├─ Lat: " + valStyle.Render("--") + "\n")
	b.WriteString("└─ Cost:" + valStyle.Render("--") + "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(m.sidebarWidth).
		Height(m.height - 5). // leave room for input box
		Padding(0, 1).
		Render(b.String())
}

func (m Model) chatView() string {
	chatWidth := m.width
	if m.showSidebar {
		chatWidth -= m.sidebarWidth
	}
	chatWidth -= 2 // border

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(chatWidth).
		Height(m.height - 5). // match sidebar
		Padding(0, 1).
		Render(m.viewport.View())
}

func (m Model) inputView() string {
	content := ""
	if m.waitingApprove {
		content = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Warning)).Render("Press 'y' to approve, 'n' to deny.")
	} else {
		content = "> " + m.textInput.View()
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.Primary)). // Highlighted border for input
		Width(m.width - 2). // full width minus borders
		Height(1). // auto-sized by padding/borders to 3 total lines
		Padding(0, 1).
		Render(content)
}

func (m Model) renderProgressBar(ctxUsed string, width int) string {
	// Simple stub for now. Ideally parse ctxUsed "1200/8000"
	pct := 0.2 // placeholder
	filled := int(float64(width) * pct)
	empty := width - filled
	if filled < 0 { filled = 0 }
	if empty < 0 { empty = 0 }
	
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Primary)).Render(bar)
}

func (m Model) renderBanner() string {
	banner := `
 ██████╗██╗   ██╗██████╗ ███████╗
██╔════╝██║   ██║██╔══██╗██╔════╝
██║     ██║   ██║██║  ██║█████╗  
██║     ██║   ██║██║  ██║██╔══╝  
╚██████╗╚██████╔╝██████╔╝███████╗
 ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝
`
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Primary)).Bold(true).Render(banner)
}

func (m Model) renderSmallBanner() string {
	banner := `
 ██████  ██    ██  ██████   ███████ 
 ██      ██    ██  ██   ██  ██      
 ██      ██    ██  ██   ██  ███████ 
 ██      ██    ██  ██   ██  ██      
 ██████  ████████  ██████   ███████ 
`
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Primary)).Bold(true).Render(banner)
}
