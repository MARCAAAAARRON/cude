package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ParsedAction represents a single tool invocation extracted from model output.
type ParsedAction struct {
	ToolName string
	ToolArgs string // JSON-encoded arguments
	ToolID   string // set by native tool-calls; generated for text-parsed actions
}

// Parser extracts tool actions from model output. It operates in two modes:
// - Native: for API models that return structured tool-calls (pass-through)
// - Text: for local models that produce ACTION/INPUT formatted text
type Parser struct {
	actionRe *regexp.Regexp
	counter  int
}

// NewParser creates a new response parser.
func NewParser() *Parser {
	return &Parser{
		// Matches ACTION: <name>\nINPUT: <json>\n---
		// The (?s) flag makes . match newlines so JSON can span multiple lines.
		actionRe: regexp.MustCompile(`(?m)^ACTION:\s*(\S+)\s*\nINPUT:\s*((?s).+?)(?:\n---|$)`),
	}
}

// ExtractActions parses text-based tool invocations from model output.
// This is used for local models that can't do native function-calling.
// Returns empty slice (not error) if no actions are found — the model is
// simply giving a conversational response.
func (p *Parser) ExtractActions(text string) ([]ParsedAction, error) {
	matches := p.actionRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		// Fall back to fuzzy extraction for local models that don't
		// follow the strict ACTION/INPUT/--- format perfectly.
		fuzzy := p.FuzzyExtract(text)
		if len(fuzzy) > 0 {
			return fuzzy, nil
		}
		return nil, nil
	}

	actions := make([]ParsedAction, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match[1])
		argsRaw := strings.TrimSpace(match[2])

		// Clean up markdown code blocks if the model wrapped the JSON.
		argsRaw = cleanMarkdownJSON(argsRaw)

		// Validate that the args look like JSON.
		if !isJSONish(argsRaw) {
			// Try to recover: wrap in a simple object.
			argsRaw = fmt.Sprintf(`{"input": %q}`, argsRaw)
		}

		p.counter++
		actions = append(actions, ParsedAction{
			ToolName: name,
			ToolArgs: argsRaw,
			ToolID:   fmt.Sprintf("text_%d", p.counter),
		})
	}
	return actions, nil
}

// FuzzyExtract attempts a more lenient extraction when strict parsing fails.
// It looks for common patterns that small models produce when trying to
// follow the ACTION/INPUT template but getting the format slightly wrong.
func (p *Parser) FuzzyExtract(text string) []ParsedAction {
	var actions []ParsedAction

	// Pattern 1: "tool: <name>" followed by a JSON block.
	toolRe := regexp.MustCompile(`(?mi)(?:tool|action|use|call):\s*(\S+)`)
	jsonRe := regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.+?\\})\\s*```")

	toolMatches := toolRe.FindAllStringSubmatch(text, -1)
	jsonMatches := jsonRe.FindAllStringSubmatch(text, -1)

	for i, tm := range toolMatches {
		args := "{}"
		if i < len(jsonMatches) {
			args = strings.TrimSpace(jsonMatches[i][1])
		}
		p.counter++
		actions = append(actions, ParsedAction{
			ToolName: strings.TrimSpace(tm[1]),
			ToolArgs: args,
			ToolID:   fmt.Sprintf("fuzzy_%d", p.counter),
		})
	}

	return actions
}

// isJSONish does a cheap check for whether a string looks like valid JSON.
func isJSONish(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return false
	}
	// Quick structural check.
	if (s[0] == '{' && s[len(s)-1] == '}') || (s[0] == '[' && s[len(s)-1] == ']') {
		var js json.RawMessage
		return json.Unmarshal([]byte(s), &js) == nil
	}
	return false
}

// cleanMarkdownJSON strips markdown backticks from a string.
func cleanMarkdownJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		idx := strings.Index(s, "\n")
		if idx != -1 {
			s = s[idx+1:]
		} else {
			s = strings.TrimPrefix(s, "```json")
			s = strings.TrimPrefix(s, "```")
		}
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}
