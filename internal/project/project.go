package project

import (
	"os"
	"path/filepath"
)

// Project represents the detected context of the current workspace.
type Project struct {
	Root    string
	Type    string // e.g. "go", "node", "python", "unknown"
}

// Detect attempts to find the root of the project by looking for markers
// like .git, go.mod, package.json, etc. It traverses up from the given
// startDir.
func Detect(startDir string) (*Project, error) {
	curr := startDir
	for {
		// Check for markers.
		if _, err := os.Stat(filepath.Join(curr, ".git")); err == nil {
			return &Project{Root: curr, Type: detectType(curr)}, nil
		}
		if _, err := os.Stat(filepath.Join(curr, "go.mod")); err == nil {
			return &Project{Root: curr, Type: "go"}, nil
		}
		if _, err := os.Stat(filepath.Join(curr, "package.json")); err == nil {
			return &Project{Root: curr, Type: "node"}, nil
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			// Hit filesystem root.
			break
		}
		curr = parent
	}

	// Fallback to startDir if no markers found.
	return &Project{Root: startDir, Type: "unknown"}, nil
}

func detectType(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return "go"
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		return "node"
	}
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		return "python"
	}
	if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
		return "rust"
	}
	return "unknown"
}
