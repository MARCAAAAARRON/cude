package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	
	"github.com/MARCAAAAARRON/cude/internal/agent"
	"github.com/MARCAAAAARRON/cude/internal/config"
	"github.com/MARCAAAAARRON/cude/internal/project"
	"github.com/MARCAAAAARRON/cude/internal/router"
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
	
	conversation  []ChatMessage
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
	chatWidth    int

	// Wizard State
	inWizard   bool
	wizardStep int
	wizardMode string
	tempModel  config.ModelConfig
	tempName   string
	
	config     *config.Config
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

func (s AgentState) String() string {
	switch s {
	case StateAPI:
		return "Thinking (API)..."
	case StateLocal:
		return "Thinking (Local)..."
	default:
		return "Idle"
	}
}

type ChatMessage struct {
	Role  string
	Text  string
	Color string
}

type status struct {
	model        string
	backend      string
	ctxUsed      string
	state        AgentState
	projName     string
	gitBranch    string
	reqStartTime time.Time
	latency      time.Duration
}

func New(ctx context.Context, a *agent.Agent) Model {
	ctx, cancel := context.WithCancel(ctx)
	
	ti := textinput.New()
	ti.Placeholder = "Ask cude anything (or /help)..."
	ti.Focus()
	
	defaultTheme := ThemeColors{
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
	}
	
	projName := "unknown"
	gitBranch := "-"
	
	if cwd, err := os.Getwd(); err == nil {
		if p, err := project.Detect(cwd); err == nil {
			projName = filepath.Base(p.Root)
			if p.Type != "" && p.Type != "unknown" {
				projName += fmt.Sprintf(" (%s)", p.Type)
			}
		}
	}
	
	if out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		gitBranch = strings.TrimSpace(string(out))
	}

	return Model{
		ctx:    ctx,
		cancel: cancel,
		agent:  a,
		
		textInput: ti,
		theme:     defaultTheme,
		currentTheme: "mono",
		
		status: status{
			model:        "loading...",
			backend:      "loading...",
			ctxUsed:      "0/?",
			state:        StateIdle,
			projName:     projName,
			gitBranch:    gitBranch,
		},
		splashUntil:  time.Now().Add(splashDuration),
		showSidebar:  true,
		sidebarWidth: 42,
	}
}

// SetRouter sets the router for slash commands (needs to be called from main.go)
func (m *Model) SetRouter(r *router.Router) {
	m.cmdRegistry = NewCommandRegistry(r)
	if r != nil {
		m.config = r.Config()
	}
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
				m.addMessage(IconOK+" Approved", m.theme.Success)
			case "n", "N":
				m.approveReq.Response <- false
				m.waitingApprove = false
				m.addMessage(IconFail+" Denied", m.theme.Error)
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
				
				if m.inWizard {
					m.handleWizardInput(val)
					return m, nil
				}
				
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
								m.addMessage(result, m.theme.Text)
							}
						} else {
							m.addMessage(fmt.Sprintf(IconFail+" Unknown command: /%s. Type /help for a list.", cmdName), m.theme.Error)
						}
					}
					return m, nil
				}

				m.conversation = append(m.conversation, ChatMessage{Role: "you", Text: val, Color: m.theme.UserLabel})
				m.refreshChat()
				
				m.status.state = StateAPI // TODO: get from actual capability
				m.status.reqStartTime = time.Now()
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
			m.refreshChat()
			
		case agent.ToolCallEvent:
			callStr := fmt.Sprintf("🔧 Running tool: %s\n%s", e.Name, e.Args)
			m.conversation = append(m.conversation, ChatMessage{Text: callStr, Color: "240"})
			m.currResponse = "" // clear current streaming response buffer
			m.refreshChat()

		case agent.ToolResultEvent:
			// Optionally log result, often too long.
			
		case agent.ApprovalRequest:
			m.waitingApprove = true
			m.approveReq = e
			reqStr := fmt.Sprintf("⚠️ The agent wants to execute %s.\n\n%s\n\nAllow this? (y/n)", e.Type, e.Preview)
			m.conversation = append(m.conversation, ChatMessage{Text: reqStr, Color: "220"})
			m.refreshChat()
			
		case agent.StatusEvent:
			m.status.model = e.Model
			m.status.backend = e.Backend
			m.status.ctxUsed = e.CtxUsed
			
		case agent.DoneEvent:
			m.status.state = StateIdle
			if m.currResponse != "" {
				m.conversation = append(m.conversation, ChatMessage{Text: m.currResponse, Color: m.theme.Text})
				m.currResponse = ""
			}
			if e.Error != nil {
				errStr := fmt.Sprintf(IconFail+" Error: %v", e.Error)
				m.conversation = append(m.conversation, ChatMessage{Text: errStr, Color: m.theme.Error})
			}
			m.refreshChat()
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

	// Dynamic sidebar sizing
	if m.width < 60 {
		m.showSidebar = false
	} else {
		// Calculate 30% of width
		targetWidth := int(float64(m.width) * 0.30)
		if targetWidth < 38 {
			targetWidth = 38
		} else if targetWidth > 45 {
			targetWidth = 45
		}
		m.sidebarWidth = targetWidth
	}

	// Calculate widths
	m.chatWidth = m.width
	if m.showSidebar {
		m.chatWidth = m.width - m.sidebarWidth
	}
	// Subtract borders (2 for left/right border, maybe 2 for padding)
	m.chatWidth -= 4 

	if m.chatWidth < 10 {
		m.chatWidth = 10
	}

	// Calculate heights
	// Input area height: 3 lines for the box
	inputHeight := 3 
	vpHeight := m.height - inputHeight - 2 // 2 for top/bottom borders of chat

	if vpHeight < 1 {
		vpHeight = 1
	}

	if !m.ready {
		m.viewport = viewport.New(m.chatWidth, vpHeight)
		m.ready = true
	} else {
		m.viewport.Width = m.chatWidth
		m.viewport.Height = vpHeight
	}
	m.textInput.Width = m.width - 4
	
	m.refreshChat()
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
	bannerStr := m.renderSmallBanner(width)
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
	pct := 0.0
	parts := strings.Split(ctxUsed, "/")
	if len(parts) == 2 {
		used, _ := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		total, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if total > 0 {
			pct = used / total
		}
	}
	if pct > 1.0 { pct = 1.0 }
	
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

func (m Model) renderSmallBanner(width int) string {
	banner := `
 ██████  ██    ██  ██████   ███████ 
 ██      ██    ██  ██   ██  ██      
 ██      ██    ██  ██   ██  ███████ 
 ██      ██    ██  ██   ██  ██      
 ██████  ████████  ██████   ███████ 
`
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Primary)).Bold(true).Render(banner)
}

func (m *Model) refreshChat() {
	var rendered []string
	
	style := lipgloss.NewStyle().Width(m.chatWidth)
	
	for _, msg := range m.conversation {
		text := msg.Text
		if msg.Role != "" {
			text = msg.Role + ": " + text
		}
		styledMsg := lipgloss.NewStyle().Foreground(lipgloss.Color(msg.Color)).Render(text)
		wrapped := style.Render(styledMsg)
		rendered = append(rendered, wrapped)
	}
	
	if m.currResponse != "" {
		streaming := lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render(m.currResponse)
		rendered = append(rendered, style.Render(streaming))
	}
	
	m.viewport.SetContent(strings.Join(rendered, "\n\n"))
	m.viewport.GotoBottom()
}

func (m *Model) addMessage(text string, color string) {
	m.conversation = append(m.conversation, ChatMessage{Text: text, Color: color})
	m.refreshChat()
}

func (m *Model) handleWizardInput(input string) {
	if strings.ToLower(input) == "/cancel" {
		m.inWizard = false
		m.addMessage(IconFail + " Wizard cancelled.", m.theme.Warning)
		return
	}

	m.addMessage("> "+input, m.theme.UserLabel)

	if m.wizardMode == "add-model" {
		m.handleAddModelWizard(input)
	} else if m.wizardMode == "remove-model" {
		m.handleRemoveModelWizard(input)
	}
}

func (m *Model) handleAddModelWizard(input string) {
	switch m.wizardStep {
	case 1:
		m.tempName = input
		m.wizardStep = 2
		m.addMessage(fmt.Sprintf("Great, we will call it '%s'.\nWhat backend does it use? (options: ollama, anthropic, openai)", m.tempName), m.theme.BotLabel)
	case 2:
		input = strings.ToLower(input)
		if input != "ollama" && input != "anthropic" && input != "openai" {
			m.addMessage("Invalid backend. Please enter 'ollama', 'anthropic', or 'openai':", m.theme.Error)
			return
		}
		m.tempModel.Backend = input
		m.wizardStep = 3
		if input == "ollama" {
			m.addMessage("What is the model identifier? (e.g., 'llama3.2:3b')", m.theme.BotLabel)
		} else if input == "anthropic" {
			m.addMessage("What is the model identifier? (e.g., 'claude-3-5-sonnet-20240620')", m.theme.BotLabel)
		} else {
			m.addMessage("What is the model identifier? (e.g., 'gpt-4o' or your local model name)", m.theme.BotLabel)
		}
	case 3:
		m.tempModel.Model = input
		m.wizardStep = 4
		if m.tempModel.Backend == "ollama" || m.tempModel.Backend == "openai" {
			defEndpoint := "http://localhost:11434"
			if m.tempModel.Backend == "openai" {
				defEndpoint = "https://api.openai.com/v1"
			}
			m.addMessage(fmt.Sprintf("What is the API endpoint?\n(Leave empty for default: %s)\n(Tip: For local servers like LM Studio or vLLM, ensure you include /v1, e.g. http://localhost:1234/v1)", defEndpoint), m.theme.BotLabel)
		} else {
			m.wizardStep = 5
			m.addMessage("What is the API key? (You can use e.g. '$ANTHROPIC_API_KEY')", m.theme.BotLabel)
		}
	case 4:
		if input == "" {
			if m.tempModel.Backend == "ollama" {
				m.tempModel.Endpoint = "http://localhost:11434"
			} else {
				m.tempModel.Endpoint = "https://api.openai.com/v1"
			}
		} else {
			m.tempModel.Endpoint = input
		}
		
		if m.tempModel.Backend == "ollama" {
			m.wizardStep = 6
			m.addMessage("What is the context window size? (e.g. 8192)", m.theme.BotLabel)
		} else {
			m.wizardStep = 5
			m.addMessage("What is the API key? (You can use e.g. '$OPENAI_API_KEY')", m.theme.BotLabel)
		}
	case 5:
		m.tempModel.APIKey = input
		m.wizardStep = 6
		m.addMessage("What is the context window size? (e.g. 8192 or 128000)", m.theme.BotLabel)
	case 6:
		var ctxWin int
		_, err := fmt.Sscanf(input, "%d", &ctxWin)
		if err != nil || ctxWin <= 0 {
			m.addMessage("Please enter a valid positive number:", m.theme.Error)
			return
		}
		m.tempModel.ContextWindow = ctxWin
		m.wizardStep = 7
		
		if m.tempModel.Backend == "ollama" {
			m.tempModel.Tier = "local"
			m.finishAddModel()
		} else {
			m.addMessage("Is this a 'local' or 'api' tier model? (api tier enables tool calling functions)", m.theme.BotLabel)
		}
	case 7:
		input = strings.ToLower(input)
		if input != "local" && input != "api" {
			m.addMessage("Please enter 'local' or 'api':", m.theme.Error)
			return
		}
		m.tempModel.Tier = input
		m.finishAddModel()
	}
}

func (m *Model) finishAddModel() {
	if m.config != nil {
		m.config.AddOrUpdateModel(m.tempName, m.tempModel)
		err := m.config.Save()
		if err != nil {
			m.addMessage(fmt.Sprintf(IconFail + " Failed to save config: %v", err), m.theme.Error)
		} else {
			m.addMessage(fmt.Sprintf(IconOK + " Saved model '%s' to %s", m.tempName, m.config.LoadedPath), m.theme.Success)
		}
	} else {
		m.addMessage(IconFail + " Could not save: config reference is nil.", m.theme.Error)
	}
	m.inWizard = false
}

func (m *Model) handleRemoveModelWizard(input string) {
	if m.config == nil {
		m.addMessage(IconFail + " Could not remove: config reference is nil.", m.theme.Error)
		m.inWizard = false
		return
	}
	
	if input == "" {
		m.addMessage("Operation cancelled.", m.theme.Warning)
		m.inWizard = false
		return
	}
	
	_, err := m.config.LookupModel(input)
	if err != nil {
		m.addMessage(fmt.Sprintf("Model '%s' not found. Cancelled.", input), m.theme.Error)
		m.inWizard = false
		return
	}
	
	m.config.RemoveModel(input)
	err = m.config.Save()
	if err != nil {
		m.addMessage(fmt.Sprintf(IconFail + " Failed to save config: %v", err), m.theme.Error)
	} else {
		m.addMessage(fmt.Sprintf(IconOK + " Removed model '%s' and saved %s", input, m.config.LoadedPath), m.theme.Success)
	}
	m.inWizard = false
}
