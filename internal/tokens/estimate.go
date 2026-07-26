package tokens

import (
	"strings"
	"unicode"
)

// Estimate returns a cheap character-based token approximation for the
// configured fallback-path use case (no tokenizer available for a
// local model). Rule of thumb: ~4 runes/token for prose; CJK and most
// emoji count heavier (each rune ≈ 1 token). Good enough for budget
// math, never for billing.
func Estimate(s string) int {
	if s == "" {
		return 0
	}
	light := 0
	heavy := 0
	for _, r := range s {
		if r > 0x2E80 { // CJK and adjacent blocks
			heavy++
		} else {
			light++
		}
	}
	toks := light/4 + heavy
	if toks < 1 {
		toks = 1
	}
	return toks
}

func JoinSep(msgs []string) string {
	return strings.Join(msgs, "\n\n")
}

// SumWordsAndPunct sanity helper for tests.
func printableASCIIRatio(s string) float64 {
	if s == "" {
		return 0
	}
	p := 0
	for _, r := range s {
		if r >= 0x20 && r < 0x7F {
			p++
		} else if unicode.IsSpace(r) {
			p++
		}
	}
	return float64(p) / float64(len([]rune(s)))
}
