package agent

import (
	"testing"
)

func TestExtractActions_Strict(t *testing.T) {
	p := NewParser()
	text := `I'll read that file for you.

ACTION: file_read
INPUT: {"path": "/src/main.go", "start_line": 1, "end_line": 50}
---

Let me check the contents.`

	actions, err := p.ExtractActions(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(actions))
	}
	if actions[0].ToolName != "file_read" {
		t.Fatalf("want tool 'file_read', got %q", actions[0].ToolName)
	}
	if actions[0].ToolArgs == "" {
		t.Fatal("args should not be empty")
	}
}

func TestExtractActions_MultipleActions(t *testing.T) {
	p := NewParser()
	text := `ACTION: file_read
INPUT: {"path": "a.go"}
---
ACTION: file_read
INPUT: {"path": "b.go"}
---`

	actions, err := p.ExtractActions(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 {
		t.Fatalf("want 2 actions, got %d", len(actions))
	}
}

func TestExtractActions_NoActions(t *testing.T) {
	p := NewParser()
	text := "Here is my answer: the function returns an integer."
	actions, err := p.ExtractActions(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("want 0 actions for plain text, got %d", len(actions))
	}
}

func TestExtractActions_MalformedJSON_Recovery(t *testing.T) {
	p := NewParser()
	text := `ACTION: file_read
INPUT: not valid json
---`

	actions, err := p.ExtractActions(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("want 1 action with recovered args, got %d", len(actions))
	}
	// Should have been wrapped in {"input": "..."}
	if actions[0].ToolArgs == "not valid json" {
		t.Fatal("expected args to be wrapped, got raw string")
	}
}

func TestFuzzyExtract(t *testing.T) {
	p := NewParser()
	text := "I think I should use the tool: file_read\n\n```json\n{\"path\": \"main.go\"}\n```"
	actions := p.FuzzyExtract(text)
	if len(actions) != 1 {
		t.Fatalf("want 1 fuzzy action, got %d", len(actions))
	}
	if actions[0].ToolName != "file_read" {
		t.Fatalf("want 'file_read', got %q", actions[0].ToolName)
	}
}

func TestIsJSONish(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`{"key": "val"}`, true},
		{`[1, 2, 3]`, true},
		{`not json`, false},
		{``, false},
		{`{broken`, false},
	}
	for _, c := range cases {
		if got := isJSONish(c.in); got != c.want {
			t.Errorf("isJSONish(%q)=%v want %v", c.in, got, c.want)
		}
	}
}
