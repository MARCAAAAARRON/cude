package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigValid(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DefaultModel == "" {
		t.Fatal("DefaultModel must not be empty")
	}
	if len(cfg.Models) == 0 {
		t.Fatal("must have at least one model defined")
	}
	if cfg.Agent.MaxIterations < 1 {
		t.Fatal("MaxIterations must be >= 1")
	}
}

func TestResolvedAPIKeyLiteral(t *testing.T) {
	m := ModelConfig{APIKey: "sk-abc123"}
	if got := m.ResolvedAPIKey(); got != "sk-abc123" {
		t.Fatalf("want literal key, got %q", got)
	}
}

func TestResolvedAPIKeyEnvVar(t *testing.T) {
	os.Setenv("CUDE_TEST_KEY", "from-env")
	defer os.Unsetenv("CUDE_TEST_KEY")
	m := ModelConfig{APIKey: "$CUDE_TEST_KEY"}
	if got := m.ResolvedAPIKey(); got != "from-env" {
		t.Fatalf("want 'from-env', got %q", got)
	}
}

func TestIsLocal(t *testing.T) {
	cases := []struct {
		tier string
		want bool
	}{
		{"local", true},
		{"Local", true},
		{"LOCAL", true},
		{"api", false},
		{"", false},
	}
	for _, c := range cases {
		if got := (ModelConfig{Tier: c.tier}).IsLocal(); got != c.want {
			t.Errorf("IsLocal(%q)=%v want %v", c.tier, got, c.want)
		}
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cude.toml")
	content := `
default_model = "test-model"
default_backend = "openai"

[models.test-model]
backend = "openai"
model = "gpt-4"
endpoint = "http://localhost:8080/v1"
api_key = "test-key"
context_window = 128000
tier = "api"

[agent]
max_iterations = 10
approve_writes = false
approve_shell = false
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "test-model" {
		t.Fatalf("want 'test-model', got %q", cfg.DefaultModel)
	}
	m, err := cfg.LookupModel("test-model")
	if err != nil {
		t.Fatal(err)
	}
	if m.ContextWindow != 128000 {
		t.Fatalf("want 128000, got %d", m.ContextWindow)
	}
	if cfg.Agent.MaxIterations != 10 {
		t.Fatalf("want 10 iterations, got %d", cfg.Agent.MaxIterations)
	}
}

func TestLookupModelNotFound(t *testing.T) {
	cfg := DefaultConfig()
	_, err := cfg.LookupModel("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}
