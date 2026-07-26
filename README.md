<p align="center">
  <img src="public/CUDE.png" alt="CUDE Banner" />
  <br/><br/>
  <strong>Hybrid local/API coding agent for your terminal.</strong>
  <br/>
  <sub>Run the same agentic coding workflow on a 3B local model or Claude — CUDE adapts automatically.</sub>
</p>

---

CUDE is a terminal-based coding agent — architecturally similar to Claude Code and OpenCode — whose defining trait is **model flexibility across two very different regimes**. It can drive a 3B parameter model running locally on Ollama with 8K context, or a 200K-context Claude/GPT-4 via API, adapting its own prompting strategy, context budget, and tool-calling approach based on which tier of model is active. Configuration is a single TOML file; switching models is a `/model` command away.

> [!NOTE]
> CUDE is in **active early development**. The core agent loop, backend integrations, tool system, and TUI are functional, but features like session persistence, auto-escalation, and project indexing are stubs or partially implemented. Expect rough edges.

## Features

- **Dual-mode agent runtime** — adapts prompting, context windowing, and tool invocation strategy based on whether the active model is local (text-parsed `ACTION/INPUT` format) or API (native function-calling)
- **Multi-backend model router** — Ollama, Anthropic (Claude), OpenAI-compatible endpoints (OpenRouter, LM Studio, vLLM) from a single config file. Hot-swap with `/model` at runtime
- **Token-budget-aware context scheduler** — allocates the context window across system prompt, file context, conversation history, and tool output with different ratios per model tier (e.g. 25% system for local vs. 15% for API). Compacts older messages automatically when budget is tight
- **Agentic tool loop** — file read/write, shell execution, project search, and directory listing with explicit user approval before side-effects
- **Text-based action parser with fuzzy fallback** — for local models that can't do native tool-calling, parses `ACTION:/INPUT:` blocks from raw text output, with a fuzzy extractor for models that get the format slightly wrong
- **Dashboard TUI** — Bubble Tea-based terminal interface with a sidebar (model info, workspace, context usage bar), conversation pane, approval flow, and slash-command system
- **14 slash commands** — `/help`, `/new`, `/model`, `/compact`, `/undo`, `/sessions`, `/export`, `/editor`, `/details`, `/thinking`, `/theme`, `/cost`, `/exit`
- **4 built-in themes** — dark, light, neon (default — the purple one), mono
- **Cross-platform builds** — Makefile and GoReleaser config for Linux, macOS, and Windows (amd64 + arm64)

<!-- TODO: add screenshot/gif of the TUI in action -->

## Tech Stack

| Component | Library |
|-----------|---------|
| Language | Go 1.26+ |
| TUI framework | [Bubble Tea](https://github.com/charmbracelet/bubbletea) v1.3 + [Bubbles](https://github.com/charmbracelet/bubbles) + [Lip Gloss](https://github.com/charmbracelet/lipgloss) |
| Anthropic SDK | [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) v1.61 |
| OpenAI SDK | [openai-go](https://github.com/openai/openai-go) v1.12 (also used for LM Studio / OpenRouter) |
| Ollama SDK | [ollama/ollama](https://github.com/ollama/ollama) v0.32 |
| Config | [BurntSushi/toml](https://github.com/BurntSushi/toml) v1.6 |
| Release | [GoReleaser](https://goreleaser.com/) |

## Architecture

```
cmd/cude/main.go          ← entry point, wires everything together
internal/
├── config/                ← TOML config loader (project-local → user-global → defaults)
├── router/                ← model registry + lazy backend initialization
├── backend/               ← unified Backend interface
│   ├── anthropic/         ← Claude Messages API (streaming)
│   ├── openai/            ← OpenAI-compatible (OpenRouter, LM Studio, vLLM)
│   └── ollama/            ← Ollama chat API (streaming)
├── agent/                 ← core agentic loop
│   ├── agent.go           ← reason → act → observe cycle with event system
│   ├── parser.go          ← text-based action parser + fuzzy fallback
│   └── context.go         ← token-budget-aware context scheduler
├── tools/                 ← tool implementations
│   ├── file_read.go       ← sandboxed file reading
│   ├── file_write.go      ← file writing with approval
│   ├── shell.go           ← shell execution with approval
│   ├── project_search.go  ← grep-based project search
│   └── list_files.go      ← directory listing
├── tokens/                ← cheap character-based token estimation
├── project/               ← workspace detection (go.mod, package.json, .git, etc.)
├── session/               ← session save/load (JSON, partially wired)
├── tui/                   ← Bubble Tea model, commands, icons, rendering
└── mascot/                ← ASCII art (legacy, no longer used in main UI)
```

**Data flow:** User input → TUI → Agent.Process() → ContextScheduler.Build() → Backend.Chat() → streaming chunks → Parser.ExtractActions() → ToolExecutor → tool results fed back into history → loop until no actions or max iterations.

## Getting Started

### Prerequisites

- **Go 1.26+** (check with `go version`)
- **At least one model backend:**
  - [Ollama](https://ollama.com/) with a model pulled (e.g. `ollama pull llama3.2:3b`) for local use, _or_
  - An API key for Anthropic (`ANTHROPIC_API_KEY`) or an OpenAI-compatible provider (`OPENROUTER_API_KEY`), _or_
  - [LM Studio](https://lmstudio.ai/) running with a model loaded (exposes OpenAI-compatible endpoint on `localhost:1234`)

### Install

**From source:**

```bash
git clone https://github.com/MARCAAAAARRON/cude.git
cd cude
make build
./bin/cude
```

**Go install:**

```bash
go install github.com/MARCAAAAARRON/cude/cmd/cude@latest
```

**From GitHub Releases (Linux/macOS):**

```bash
curl -sSL https://raw.githubusercontent.com/MARCAAAAARRON/cude/main/scripts/install.sh | bash
```

### Configuration

CUDE looks for `cude.toml` in this order:

1. Explicit path via `--config /path/to/cude.toml`
2. `./cude.toml` in the current directory (project-local)
3. `~/.config/cude/cude.toml` (user-global)
4. Built-in defaults (Ollama + `llama3.2:3b` on `localhost:11434`)

**Minimal config for local-only use (Ollama):**

```toml
default_model = "llama3.2"

[models.llama3_2]
backend = "ollama"
model   = "llama3.2:3b"
endpoint = "http://localhost:11434"
context_window = 8192
tier = "local"

[agent]
approve_writes = true
approve_shell  = true
```

**Adding an API model:**

```toml
[models.claude]
backend = "anthropic"
model   = "claude-sonnet-4-20250514"
api_key = "$ANTHROPIC_API_KEY"      # reads from environment variable
context_window = 200000
tier = "api"
```

**Adding LM Studio:**

```toml
[models.lmstudio_qwen]
backend  = "openai"
model    = "qwen2.5-coder-7b"
endpoint = "http://localhost:1234/v1"
api_key  = "lm-studio"             # LM Studio ignores the key
context_window = 32768
tier = "local"
```

### Environment Variables

| Variable | Used by |
|----------|---------|
| `ANTHROPIC_API_KEY` | Anthropic backend (when `api_key = "$ANTHROPIC_API_KEY"` in config) |
| `OPENROUTER_API_KEY` | OpenRouter backend |
| `EDITOR` | `/editor` command (falls back to `notepad` on Windows, `vi` on Unix) |

## Usage

```bash
# Launch with default model from config
cude

# Override model at startup
cude --model claude

# Print version
cude --version
```

**Inside the TUI:**

| Command | Description |
|---------|-------------|
| `/help` | Show all commands |
| `/model [name]` | List available models or switch to `name` |
| `/new` | Clear conversation, start fresh session |
| `/compact` | Compress older context to free token budget |
| `/undo` | Revert the last file write |
| `/export [file]` | Save conversation to markdown |
| `/editor` | Open `$EDITOR` for multi-line input |
| `/theme <name>` | Switch theme: `dark`, `light`, `neon`, `mono` |
| `/details` | Toggle tool execution detail visibility |
| `/thinking` | Toggle reasoning block visibility |
| `/cost` | Toggle cost/latency dashboard |
| `/exit` | Quit (also: `/quit`, `/q`, `Ctrl+C`) |

**Keyboard shortcuts:**

| Key | Action |
|-----|--------|
| `Ctrl+S` | Toggle sidebar |
| `Ctrl+C` | Exit |
| `y` / `n` | Approve / deny tool actions (when prompted) |

## Project Status

### Working

- [x] Multi-backend model router (Ollama, Anthropic, OpenAI-compatible)
- [x] Core agent loop with streaming, tool calls, and approval flow
- [x] Text-based action parsing for local models (strict + fuzzy)
- [x] Token-budget context scheduler with tier-aware ratios
- [x] 5 built-in tools (file read, file write, shell, project search, list files)
- [x] Dashboard TUI with sidebar, themes, and 14 slash commands
- [x] Cross-platform build and release pipeline

### In Progress / Stubbed

- [ ] Session persistence (save/load module exists but isn't wired into the TUI)
- [ ] Auto-escalation from local → API model on consecutive failures (config exists, logic is minimal)
- [ ] `/sessions` command (depends on session manager wiring)
- [ ] Context compaction uses naive truncation — no LLM-powered summarization yet
- [ ] Cost tracking / latency display (toggle exists, data isn't collected)
- [ ] Project-aware file retrieval / lightweight RAG indexing

### Planned

- [ ] Git integration (diff viewer, branch-aware context)
- [ ] Multi-file diff previews before approval
- [ ] Plugin/extension system for custom tools
- [ ] Conversation branching

## Building & Testing

```bash
# Build for current platform
make build

# Run tests
make test

# Cross-compile for all targets
make release

# Install to $GOPATH/bin
make install
```

## Contributing

This project is in early development. Issues and pull requests are welcome at [github.com/MARCAAAAARRON/cude](https://github.com/MARCAAAAARRON/cude).

## License

<!-- TODO: Add a LICENSE file (MIT, Apache-2.0, etc.) and update this section -->

No license file found. Please add one before distributing.

## Author

**MARCAAAAARRON** — [github.com/MARCAAAAARRON](https://github.com/MARCAAAAARRON)
