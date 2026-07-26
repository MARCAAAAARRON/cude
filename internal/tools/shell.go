package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ShellTool executes shell commands.
type ShellTool struct {
	workdir string
}

func NewShellTool(workdir string) *ShellTool {
	return &ShellTool{workdir: workdir}
}

func (t *ShellTool) Name() string { return "shell_exec" }

func (t *ShellTool) Description() string {
	return "Execute a shell command. Use for building, testing, linting, or running scripts. Does not support interactive commands."
}

func (t *ShellTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to run.",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ShellTool) Execute(ctx context.Context, argsRaw json.RawMessage) (string, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	cmdStr := strings.TrimSpace(args.Command)
	if cmdStr == "" {
		return "", fmt.Errorf("command is empty")
	}

	// Basic safety allowlist/blocklist check could go here if configured.
	
	// Timeout to prevent hanging.
	ctxTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctxTimeout, "cmd", "/c", cmdStr)
	} else {
		cmd = exec.CommandContext(ctxTimeout, "bash", "-c", cmdStr)
	}
	cmd.Dir = t.workdir

	output, err := cmd.CombinedOutput()
	outStr := string(output)

	// Truncate if output is too long to save context tokens.
	maxLen := 4000
	if len(outStr) > maxLen {
		outStr = outStr[:maxLen] + "\n... [output truncated]"
	}

	if err != nil {
		if ctxTimeout.Err() == context.DeadlineExceeded {
			return fmt.Sprintf("Command timed out after 30s.\nOutput so far:\n%s", outStr), nil
		}
		return fmt.Sprintf("Command failed with error: %v\nOutput:\n%s", err, outStr), nil
	}

	if outStr == "" {
		outStr = "(Command executed successfully with no output)"
	}

	return outStr, nil
}
