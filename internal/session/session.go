package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MARCAAAAARRON/cude/internal/backend"
)

// Session represents a saved conversation state.
type Session struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Model     string            `json:"model"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	History   []backend.Message `json:"history"`
}

// SessionInfo is a lightweight summary of a session (no history loaded).
type SessionInfo struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	MsgCount  int       `json:"msg_count"`
}

// Manager handles saving and loading sessions to disk.
type Manager struct {
	workdir    string
	sessionDir string
}

func NewManager(workdir string) *Manager {
	return &Manager{
		workdir:    workdir,
		sessionDir: filepath.Join(workdir, ".cude", "sessions"),
	}
}

// Save writes the session to disk with metadata.
func (m *Manager) Save(sess *Session) error {
	if err := os.MkdirAll(m.sessionDir, 0755); err != nil {
		return fmt.Errorf("session: create dir: %w", err)
	}

	sess.UpdatedAt = time.Now()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = sess.UpdatedAt
	}

	// Derive title from first user message if empty.
	if sess.Title == "" {
		sess.Title = deriveTitle(sess.History)
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("session: encode: %w", err)
	}

	path := filepath.Join(m.sessionDir, sess.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("session: write %s: %w", path, err)
	}

	return nil
}

// Load reads a full session from disk.
func (m *Manager) Load(id string) (*Session, error) {
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

	return &sess, nil
}

// ListSessions returns metadata for all saved sessions, sorted newest-first.
func (m *Manager) ListSessions() ([]SessionInfo, error) {
	entries, err := os.ReadDir(m.sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("session: list dir: %w", err)
	}

	var infos []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(m.sessionDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var sess Session
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}

		infos = append(infos, SessionInfo{
			ID:        sess.ID,
			Title:     sess.Title,
			Model:     sess.Model,
			CreatedAt: sess.CreatedAt,
			UpdatedAt: sess.UpdatedAt,
			MsgCount:  len(sess.History),
		})
	}

	// Sort by most recently updated first.
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].UpdatedAt.After(infos[j].UpdatedAt)
	})

	return infos, nil
}

// Delete removes a session file from disk.
func (m *Manager) Delete(id string) error {
	path := filepath.Join(m.sessionDir, id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("session: delete %s: %w", path, err)
	}
	return nil
}

// deriveTitle extracts the first user message as the session title, truncated.
func deriveTitle(history []backend.Message) string {
	for _, m := range history {
		if m.Role == backend.RoleUser && m.Content != "" {
			title := m.Content
			if len(title) > 60 {
				title = title[:57] + "..."
			}
			// Single line only.
			if idx := strings.IndexByte(title, '\n'); idx >= 0 {
				title = title[:idx]
			}
			return title
		}
	}
	return "untitled session"
}
