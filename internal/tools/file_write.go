package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileWriteTool allows editing files using search/replace blocks.
// We avoid full-file rewrites to save tokens and prevent catastrophic truncation.
type FileWriteTool struct {
	workdir string
}

func NewFileWriteTool(workdir string) *FileWriteTool {
	return &FileWriteTool{workdir: workdir}
}

func (t *FileWriteTool) Name() string { return "file_write" }

func (t *FileWriteTool) Description() string {
	return "Edit a file using search and replace. Provide the exact text to match in 'search', and the new text in 'replace'. To create a new file, leave 'search' empty and put the content in 'replace'. To append to an existing file, leave 'search' empty."
}

func (t *FileWriteTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to edit or create.",
			},
			"search": map[string]any{
				"type":        "string",
				"description": "Exact text to find in the file. Must match completely including indentation. Leave empty for new files or to append to an existing file.",
			},
			"replace": map[string]any{
				"type":        "string",
				"description": "Text to replace the search block with.",
			},
		},
		"required": []string{"path", "replace"},
	}
}

func (t *FileWriteTool) Execute(ctx context.Context, argsRaw json.RawMessage) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Search  string `json:"search"`
		Replace string `json:"replace"`
	}
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if args.Path == "" {
		return "", fmt.Errorf("missing required 'path' argument. You must specify the file path to create or edit")
	}

	fullPath := filepath.Join(t.workdir, args.Path)
	
	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Create new file if it doesn't exist and search is empty.
	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) && args.Search == "" {
			err = os.WriteFile(fullPath, []byte(args.Replace), 0644)
			if err != nil {
				return "", fmt.Errorf("failed to create file: %w", err)
			}
			return fmt.Sprintf("Successfully created file %s", args.Path), nil
		}
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	if args.Search == "" {
		newContent := string(content)
		if !strings.HasSuffix(newContent, "\n") && !strings.HasSuffix(newContent, "\r\n") {
			if strings.Contains(newContent, "\r\n") {
				newContent += "\r\n"
			} else {
				newContent += "\n"
			}
		}
		newContent += args.Replace

		backupPath := fullPath + ".bak"
		_ = os.WriteFile(backupPath, content, 0644)

		if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("failed to append to file: %w", err)
		}

		return fmt.Sprintf("Successfully appended to file %s", args.Path), nil
	}

	contentStr := string(content)

	// Detect the file's line ending style so we can preserve it.
	useCRLF := strings.Contains(contentStr, "\r\n")

	// Normalize both content and search/replace to \n for matching.
	// This is critical on Windows: files have \r\n but models always output \n.
	normalizedContent := strings.ReplaceAll(contentStr, "\r\n", "\n")
	normalizedSearch := strings.ReplaceAll(args.Search, "\r\n", "\n")
	normalizedReplace := strings.ReplaceAll(args.Replace, "\r\n", "\n")

	// Count occurrences.
	count := strings.Count(normalizedContent, normalizedSearch)
	if count == 0 {
		return "", fmt.Errorf("search text not found in file. Ensure exact match including whitespace/indentation")
	}
	if count > 1 {
		return "", fmt.Errorf("search text found %d times, it must be unique. Provide more context", count)
	}

	// Apply replacement on normalized content.
	newContent := strings.Replace(normalizedContent, normalizedSearch, normalizedReplace, 1)

	// Restore original line ending style.
	if useCRLF {
		newContent = strings.ReplaceAll(newContent, "\n", "\r\n")
	}

	// Create backup before writing.
	backupPath := fullPath + ".bak"
	_ = os.WriteFile(backupPath, content, 0644)

	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Successfully updated file %s", args.Path), nil
}
