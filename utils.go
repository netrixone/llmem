package main

import (
	"math"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/rekram1-node/tokenizer/tokenizer"
)

// envOr returns the value of key from the environment, or def if unset or blank.
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// ExcerptForLog returns a short excerpt of s for logging: first maxLen runes, newlines
// collapsed to space, with "..." appended if truncated. Used for MEMORY log lines.
func ExcerptForLog(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	s = cleanOneLine(s)
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "..."
}

// HardTruncateToBytes truncates s to at most maxBytes UTF-8 bytes, preserving rune boundaries.
// It ensures the result is valid UTF-8 by trimming incomplete runes at the end.
func HardTruncateToBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}

	// Preserve valid UTF-8 by trimming to rune boundaries.
	b = b[:maxBytes]
	for !utf8.Valid(b) {
		if len(b) == 0 {
			return ""
		}
		b = b[:len(b)-1]
	}
	return string(b)
}

// itoaBase10 formats n as a decimal string without using strconv.
func itoaBase10(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(buf[i:])
}

func tokenSet(text string) map[string]struct{} {
	toks := tokenize(text)
	if len(toks) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(toks))
	for _, tok := range toks {
		set[tok] = struct{}{}
	}
	return set
}

// vectorize tokenizes text and returns a bag-of-words vector and its L2 norm.
func vectorize(text string) (vec map[string]float64, norm float64) {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return map[string]float64{}, 0
	}

	vec = make(map[string]float64, len(tokens))
	for _, tok := range tokens {
		vec[tok] += 1
	}

	var sumSq float64
	for _, v := range vec {
		sumSq += v * v
	}
	return vec, math.Sqrt(sumSq)
}

// tokenize returns stemmed, lowercase English tokens with stop words removed.
// Tokens shorter than 2 characters (after stemming) are dropped.
// Hashtags (e.g. #status) are preserved without stemming.
// Used for similarity, search indexing, and label fallback.
func tokenize(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	t := tokenizer.New().SetStopWordRemoval(true)
	raw := t.TokenizeString(text)
	out := make([]string, 0, len(raw))
	for _, tok := range raw {
		if len(tok) < 2 {
			continue
		}
		// Preserve hashtags as-is (important for #status, #goal, etc.)
		if tok[0] == '#' {
			out = append(out, tok)
			continue
		}
		// Strip trailing/leading punctuation before stemming so the
		// stemmer sees clean word forms (e.g. "metrics." → "metrics" → "metric").
		tok = strings.Trim(tok, ".,;:!?\"'()[]{}")
		if len(tok) < 2 {
			continue
		}
		stemmed := stem(tok)
		if len(stemmed) >= 2 {
			out = append(out, stemmed)
		}
	}
	return out
}

// cleanOneLine trims and collapses newlines/tabs into single spaces.
func cleanOneLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Collapse newlines/tabs into single spaces.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool {
	return &b
}

// normalizeScopes normalizes a scope slice by:
// - Trimming whitespace from each scope
// - Removing empty strings
// - Deduplicating
// - Returning nil if empty (for global memories)
func normalizeScopes(scopes []string) []string {
	if scopes == nil {
		return nil
	}
	normalized := make([]string, 0)
	seen := make(map[string]struct{})
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			if _, exists := seen[scope]; !exists {
				seen[scope] = struct{}{}
				normalized = append(normalized, scope)
			}
		}
	}
	// If empty after normalization, return nil (global)
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
