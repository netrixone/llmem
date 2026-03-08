//go:build integration
// +build integration

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestTgptSummarizer_Summarize_EnforcesMaxBytes(t *testing.T) {
	if _, err := exec.LookPath("tgpt"); err != nil {
		t.Skip("tgpt not found on PATH; skipping integration test")
	}

	s := NewTgptSummarizer(TgptSummarizerOptions{
		Binary:   envOr("TGPT_BIN", "tgpt"),
		Provider: os.Getenv("TGPT_PROVIDER"),
		Model:    os.Getenv("TGPT_MODEL"),
		Key:      os.Getenv("TGPT_KEY"),
		URL:      os.Getenv("TGPT_URL"),
		Timeout:  2 * time.Minute,
	})

	// Use a long input to force summarization.
	input := strings.Repeat(
		`We discussed project scope, constraints, and next steps. The key requirement is memory chunk storage with similarity links. `,
		80,
	)

	const maxBytes = 200
	out, err := s.Summarize(input, maxBytes)
	if err != nil {
		t.Fatalf("Summarize() error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("Summarize() returned empty output")
	}
	if got := len([]byte(out)); got > maxBytes {
		t.Fatalf("Summarize() exceeded maxBytes: got %d > %d; output=%q", got, maxBytes, out)
	}
	if strings.Contains(out, "\n") || strings.Contains(out, "\r") {
		t.Fatalf("Summarize() returned multi-line output (expected single line). output=%q", out)
	}
}
