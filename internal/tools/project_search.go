package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// ProjectSearchTool searches for patterns using ripgrep if available.
type ProjectSearchTool struct {
	workdir string
}

func NewProjectSearchTool(workdir string) *ProjectSearchTool {
	return &ProjectSearchTool{workdir: workdir}
}

func (t *ProjectSearchTool) Name() string { return "project_search" }

func (t *ProjectSearchTool) Description() string {
	return "Search for text patterns across the project using ripgrep. Returns matching files and line snippets."
}

func (t *ProjectSearchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search pattern (regex supported).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *ProjectSearchTool) Execute(ctx context.Context, argsRaw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if args.Query == "" {
		return "", fmt.Errorf("query is empty")
	}

	// Check if ripgrep is installed
	if _, err := exec.LookPath("rg"); err != nil {
		// Fallback to git grep if rg isn't available
		if _, err := exec.LookPath("git"); err == nil {
			return t.runCommand(ctx, "git", "grep", "-n", args.Query)
		}
		return "", fmt.Errorf("neither 'rg' (ripgrep) nor 'git grep' is installed on the system")
	}

	// Run ripgrep
	return t.runCommand(ctx, "rg", "-n", "--max-columns=150", args.Query)
}

func (t *ProjectSearchTool) runCommand(ctx context.Context, name string, args ...string) (string, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctxTimeout, name, args...)
	cmd.Dir = t.workdir

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	
	// Exit status 1 means no matches for both rg and git grep.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found.", nil
		}
		return "", fmt.Errorf("search failed: %v\nOutput: %s", err, outStr)
	}

	// Truncate if too long.
	maxLen := 4000
	if len(outStr) > maxLen {
		outStr = outStr[:maxLen] + "\n... [output truncated, too many matches]"
	}

	return outStr, nil
}
