package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ListFilesTool lists files in a directory.
type ListFilesTool struct {
	workdir string
}

func NewListFilesTool(workdir string) *ListFilesTool {
	return &ListFilesTool{workdir: workdir}
}

func (t *ListFilesTool) Name() string { return "list_files" }

func (t *ListFilesTool) Description() string {
	return "List files and directories in a specific path. Useful for exploring the project structure."
}

func (t *ListFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path relative to the project root. Use '.' for the root.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ListFilesTool) Execute(ctx context.Context, argsRaw json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	target := filepath.Join(t.workdir, args.Path)
	
	entries, err := os.ReadDir(target)
	if err != nil {
		return "", fmt.Errorf("failed to list directory %s: %w", args.Path, err)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Contents of %s:\n", args.Path))
	
	for _, e := range entries {
		// Basic skip for common noisy dirs
		if e.IsDir() && (e.Name() == ".git" || e.Name() == "node_modules") {
			b.WriteString(fmt.Sprintf("%s/ (skipped)\n", e.Name()))
			continue
		}
		
		if e.IsDir() {
			b.WriteString(fmt.Sprintf("%s/\n", e.Name()))
		} else {
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			b.WriteString(fmt.Sprintf("%s (%d bytes)\n", e.Name(), size))
		}
	}

	return b.String(), nil
}
