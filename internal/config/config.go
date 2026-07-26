package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds the complete application configuration.
type Config struct {
	DefaultModel   string                `toml:"default_model"`
	DefaultBackend string                `toml:"default_backend"`
	Models         map[string]ModelConfig `toml:"models"`
	Agent          AgentConfig            `toml:"agent"`
	TUI            TUIConfig              `toml:"tui"`

	LoadedPath     string                 `toml:"-"`
}

// ModelConfig describes a single model endpoint.
type ModelConfig struct {
	Backend       string `toml:"backend"`        // "ollama", "anthropic", "openai"
	Model         string `toml:"model"`           // model name/identifier
	Endpoint      string `toml:"endpoint"`        // API endpoint URL
	APIKey        string `toml:"api_key"`          // literal key or "$ENV_VAR" reference
	ContextWindow int    `toml:"context_window"`   // max context tokens
	Tier          string `toml:"tier"`             // "local" or "api"
	SupportsTools *bool  `toml:"supports_tools"`   // nil = auto-detect
	SupportsJSON  *bool  `toml:"supports_json"`    // nil = auto-detect
}

// AgentConfig controls the agentic loop behavior.
type AgentConfig struct {
	MaxIterations     int  `toml:"max_iterations"`
	AutoEscalate      bool `toml:"auto_escalate"`
	EscalateThreshold int  `toml:"escalate_threshold"`
	ApproveWrites     bool `toml:"approve_writes"`
	ApproveShell      bool `toml:"approve_shell"`
}

// TUIConfig controls TUI behavior.
type TUIConfig struct {
	SplashDurationMs int  `toml:"splash_duration_ms"`
	ShowCost         bool `toml:"show_cost"`
}

// IsLocal returns true if this model is configured as a local runtime.
func (m ModelConfig) IsLocal() bool {
	return strings.EqualFold(m.Tier, "local")
}

// ResolvedAPIKey expands environment variable references in the api_key field.
// If the key starts with "$", it is treated as an env var name.
func (m ModelConfig) ResolvedAPIKey() string {
	key := m.APIKey
	if strings.HasPrefix(key, `"`) && strings.HasSuffix(key, `"`) {
		key = strings.Trim(key, `"`)
	}
	if strings.HasPrefix(key, `'`) && strings.HasSuffix(key, `'`) {
		key = strings.Trim(key, `'`)
	}
	if strings.HasPrefix(key, "$") {
		return os.Getenv(key[1:])
	}
	return key
}

// DefaultConfig returns sensible defaults when no config file is found.
func DefaultConfig() Config {
	return Config{
		DefaultModel:   "llama3.2",
		DefaultBackend: "ollama",
		Models: map[string]ModelConfig{
			"llama3.2": {
				Backend:       "ollama",
				Model:         "llama3.2:3b",
				Endpoint:      "http://localhost:11434",
				ContextWindow: 8192,
				Tier:          "local",
			},
		},
		Agent: AgentConfig{
			MaxIterations:     25,
			AutoEscalate:      false,
			EscalateThreshold: 3,
			ApproveWrites:     true,
			ApproveShell:      true,
		},
		TUI: TUIConfig{
			SplashDurationMs: 1200,
			ShowCost:         true,
		},
	}
}

// Load searches for a config file in standard locations and returns the
// parsed Config. Search order:
//  1. Explicit path (if non-empty)
//  2. ./cude.toml (project-local)
//  3. ~/.config/cude/cude.toml (user-global)
//
// If no file is found, returns DefaultConfig with no error.
func Load(explicitPath string) (Config, error) {
	candidates := []string{}
	if explicitPath != "" {
		candidates = append(candidates, explicitPath)
	}
	candidates = append(candidates, "cude.toml")
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "cude", "cude.toml"))
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return loadFromFile(path)
		}
	}
	
	cfg := DefaultConfig()
	if home, err := os.UserHomeDir(); err == nil {
		cfg.LoadedPath = filepath.Join(home, ".config", "cude", "cude.toml")
	} else {
		cfg.LoadedPath = "cude.toml"
	}
	return cfg, nil
}

func loadFromFile(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}
	cfg.LoadedPath = path
	return cfg, nil
}

// LookupModel finds the ModelConfig by the name key in the Models map.
// Returns an error if the name does not exist.
func (c Config) LookupModel(name string) (ModelConfig, error) {
	m, ok := c.Models[name]
	if !ok {
		return ModelConfig{}, fmt.Errorf("config: model %q not found (available: %s)", name, c.modelNames())
	}
	return m, nil
}

func (c Config) modelNames() string {
	names := make([]string, 0, len(c.Models))
	for k := range c.Models {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}

// Save writes the current configuration to LoadedPath.
func (c *Config) Save() error {
	if c.LoadedPath == "" {
		return fmt.Errorf("no config path available to save")
	}
	dir := filepath.Dir(c.LoadedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(c.LoadedPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

// AddOrUpdateModel adds or updates a model configuration.
func (c *Config) AddOrUpdateModel(name string, m ModelConfig) {
	if c.Models == nil {
		c.Models = make(map[string]ModelConfig)
	}
	c.Models[name] = m
}

// RemoveModel removes a model by name.
func (c *Config) RemoveModel(name string) {
	if c.Models != nil {
		delete(c.Models, name)
	}
}
