package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileReadTool reads the contents of a file, optionally restricted by lines.
type FileReadTool struct {
	workdir string
}

func NewFileReadTool(workdir string) *FileReadTool {
	return &FileReadTool{workdir: workdir}
}

func (t *FileReadTool) Name() string { return "file_read" }

func (t *FileReadTool) Description() string {
	return "Read the contents of a file. Use start_line and end_line to read a specific range."
}

func (t *FileReadTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file relative to the project root.",
			},
			"start_line": map[string]any{
				"type":        "integer",
				"description": "1-indexed start line (inclusive). Optional.",
			},
			"end_line": map[string]any{
				"type":        "integer",
				"description": "1-indexed end line (inclusive). Optional.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *FileReadTool) Execute(ctx context.Context, argsRaw json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	fullPath := filepath.Join(t.workdir, args.Path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	start := args.StartLine
	end := args.EndLine

	if start < 1 {
		start = 1
	}
	if end < start || end > totalLines {
		end = totalLines
	}

	if start > totalLines {
		return fmt.Errorf("start_line %d is past end of file (%d lines)", start, totalLines).Error(), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("--- File: %s (lines %d-%d of %d) ---\n", args.Path, start, end, totalLines))
	
	// Add line numbers for context, makes diff editing much easier
	for i := start - 1; i < end; i++ {
		b.WriteString(fmt.Sprintf("%d: %s\n", i+1, lines[i]))
	}

	return b.String(), nil
}
