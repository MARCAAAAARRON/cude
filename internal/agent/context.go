package agent

import (
	"fmt"
	"strings"

	"github.com/MARCAAAAARRON/cude/internal/backend"
	"github.com/MARCAAAAARRON/cude/internal/tokens"
)

// ContextScheduler manages token budget allocation across the prompt
// components: system prompt, file context, conversation history, and
// tool definitions. It uses different allocation ratios for local vs. API
// model tiers and compacts history when the budget is tight.
type ContextScheduler struct {
	cap       backend.Capability
	lastUsed  int
	lastTotal int
}

// budgetRatios defines how the context window is split.
type budgetRatios struct {
	system  float64
	files   float64
	history float64
	tools   float64
}

var (
	apiRatios = budgetRatios{
		system:  0.15,
		files:   0.40,
		history: 0.35,
		tools:   0.10,
	}
	localRatios = budgetRatios{
		system:  0.25,
		files:   0.30,
		history: 0.30,
		tools:   0.15,
	}
)

// NewContextScheduler creates a scheduler for the given model capability.
func NewContextScheduler(cap backend.Capability) *ContextScheduler {
	return &ContextScheduler{
		cap:       cap,
		lastTotal: cap.Context,
	}
}

// Build constructs the final message list for the model, respecting the token
// budget. It injects the system prompt and trims/compacts history as needed.
func (cs *ContextScheduler) Build(systemPrompt string, history []backend.Message) []backend.Message {
	total := cs.cap.Context
	ratios := apiRatios
	if cs.cap.IsLocal() {
		ratios = localRatios
	}

	systemBudget := int(float64(total) * ratios.system)
	historyBudget := int(float64(total) * ratios.history)

	// Start with system prompt (truncate if over budget).
	systemTokens := tokens.Estimate(systemPrompt)
	if systemTokens > systemBudget {
		// Truncate system prompt by character ratio.
		ratio := float64(systemBudget) / float64(systemTokens)
		cutLen := int(float64(len(systemPrompt)) * ratio)
		systemPrompt = systemPrompt[:cutLen] + "\n[system prompt truncated]"
	}

	result := []backend.Message{
		{Role: backend.RoleSystem, Content: systemPrompt},
	}

	// Fit conversation history within budget.
	fittedHistory := cs.fitHistory(history, historyBudget)
	result = append(result, fittedHistory...)

	// Track usage for status bar.
	used := 0
	for _, m := range result {
		used += tokens.Estimate(m.Content)
	}
	cs.lastUsed = used
	cs.lastTotal = total

	return result
}

// fitHistory trims or compacts conversation history to fit within the token
// budget. Strategy: keep the most recent messages, summarize older ones.
func (cs *ContextScheduler) fitHistory(history []backend.Message, budget int) []backend.Message {
	if len(history) == 0 {
		return nil
	}

	// Calculate total tokens in history.
	totalTokens := 0
	msgTokens := make([]int, len(history))
	for i, m := range history {
		t := tokens.Estimate(m.Content)
		msgTokens[i] = t
		totalTokens += t
	}

	// If it all fits, return as-is.
	if totalTokens <= budget {
		return history
	}

	// Keep recent messages from the end, within budget.
	// Reserve 10% for a compaction summary of dropped messages.
	summaryBudget := budget / 10
	recentBudget := budget - summaryBudget

	kept := make([]backend.Message, 0, len(history))
	usedTokens := 0
	cutoff := len(history)

	for i := len(history) - 1; i >= 0; i-- {
		if usedTokens+msgTokens[i] > recentBudget {
			cutoff = i + 1
			break
		}
		usedTokens += msgTokens[i]
	}

	// Compact the dropped older messages into a summary.
	if cutoff > 0 {
		summary := compactMessages(history[:cutoff])
		if tokens.Estimate(summary) > summaryBudget {
			// Truncate the summary itself.
			ratio := float64(summaryBudget) / float64(tokens.Estimate(summary))
			cutLen := int(float64(len(summary)) * ratio)
			if cutLen > 0 && cutLen < len(summary) {
				summary = summary[:cutLen] + "..."
			}
		}
		kept = append(kept, backend.Message{
			Role:    backend.RoleSystem,
			Content: "[Earlier conversation summary] " + summary,
		})
	}

	kept = append(kept, history[cutoff:]...)
	return kept
}

// compactMessages generates a brief summary of messages for context compaction.
func compactMessages(msgs []backend.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		prefix := string(m.Role)
		content := m.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		b.WriteString(fmt.Sprintf("%s: %s\n", prefix, content))
	}
	return b.String()
}

// UsageSummary returns a human-readable context usage string like "2.1K/8K".
func (cs *ContextScheduler) UsageSummary() string {
	return fmt.Sprintf("%s/%s", formatTokenCount(cs.lastUsed), formatTokenCount(cs.lastTotal))
}

func formatTokenCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
