package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcar/cude/internal/backend"
)

// Session represents a saved conversation state.
type Session struct {
	ID      string            `json:"id"`
	History []backend.Message `json:"history"`
}

// Manager handles saving and loading sessions to disk.
type Manager struct {
	workdir string
	sessionDir string
}

func NewManager(workdir string) *Manager {
	return &Manager{
		workdir:    workdir,
		sessionDir: filepath.Join(workdir, ".cude", "sessions"),
	}
}

// Save writes the session history to disk.
func (m *Manager) Save(id string, history []backend.Message) error {
	if err := os.MkdirAll(m.sessionDir, 0755); err != nil {
		return fmt.Errorf("session: create dir: %w", err)
	}

	sess := Session{
		ID:      id,
		History: history,
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("session: encode: %w", err)
	}

	path := filepath.Join(m.sessionDir, id+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("session: write %s: %w", path, err)
	}

	return nil
}

// Load reads a session history from disk.
func (m *Manager) Load(id string) ([]backend.Message, error) {
	path := filepath.Join(m.sessionDir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Not found is okay, just start fresh
		}
		return nil, fmt.Errorf("session: read %s: %w", path, err)
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("session: decode %s: %w", path, err)
	}

	return sess.History, nil
}
