package main

import "strings"

// Summarizer is a pluggable component used to shrink oversized memories
// down to <= MaxMemoryBytes. The default implementation is a simple
// Truncater; callers may provide a smarter implementation (e.g. tgpt).
type Summarizer interface {
	Summarize(text string, maxBytes int) (string, error)
}

// Truncater is the default Summarizer implementation. It performs
// deterministic truncation to enforce MaxMemoryBytes limits and builds
// a simple fallback label from the first few tokens of text.
type Truncater struct{}

// NewTruncater returns a new default Summarizer implementation.
// It is kept as a helper to decouple callers from the concrete type.
func NewTruncater() *Truncater {
	return &Truncater{}
}

// Summarize returns text unchanged if within maxBytes; otherwise truncates with a suffix.
func (t *Truncater) Summarize(text string, maxBytes int) (string, error) {
	text = strings.TrimSpace(text)
	if len([]byte(text)) <= maxBytes {
		return text, nil
	}
	// Cheap placeholder: hard truncate with a suffix.
	const suffix = "…"
	if maxBytes <= len([]byte(suffix)) {
		return HardTruncateToBytes(text, maxBytes), nil
	}
	tr := HardTruncateToBytes(text, maxBytes-len([]byte(suffix)))
	return strings.TrimSpace(tr) + suffix, nil
}
