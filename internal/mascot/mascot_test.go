package mascot

import (
	"strings"
	"testing"
)

func TestLinesLength(t *testing.T) {
	if got := len(Lines()); got != 3 {
		t.Fatalf("want 3 lines, got %d", got)
	}
}

func TestRawPreserved(t *testing.T) {
	if Raw == "" {
		t.Fatal("Raw should be non-empty")
	}
	if strings.Contains(Raw, "\r") {
		t.Fatal("Raw must not contain CR (would break cross-terminal rendering)")
	}
}

func TestThinkingCyclesFrames(t *testing.T) {
	a := Thinking(StateLocal, 0)
	b := Thinking(StateLocal, 1)
	c := Thinking(StateLocal, 2)
	d := Thinking(StateLocal, 3)
	if a == b || b == c {
		t.Fatal("expected distinct frames across phases 0,1,2")
	}
	if a != d {
		t.Fatal("expected phase 3 to wrap to phase 0")
	}
}

func TestRenderContainsRawLine(t *testing.T) {
	got := Render(StateIdle)
	if !strings.Contains(got, `|-- --|`) {
		t.Fatalf("Render output lost mascot art: %q", got)
	}
}
