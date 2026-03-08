package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// TgptSummarizer implements Summarizer by calling the `tgpt` CLI.
//
// It uses `-q -w` and passes the text on stdin (so large inputs don't hit argv limits).
// You can configure provider/model/key/url/preprompt as needed; unset fields are omitted.
type TgptSummarizer struct {
	Binary    string
	Provider  string
	Model     string
	Key       string
	URL       string
	Preprompt string

	// Timeout applies per tgpt invocation.
	Timeout time.Duration

	// Env overrides (optional). These are added on top of the current environment.
	Env map[string]string
}

// TgptSummarizerOptions configures the tgpt binary and optional provider/model/key/url.
type TgptSummarizerOptions struct {
	Binary    string
	Provider  string
	Model     string
	Key       string
	URL       string
	Preprompt string
	Timeout   time.Duration
	Env       map[string]string
}

// NewTgptSummarizer builds a Summarizer that shells out to the tgpt CLI.
func NewTgptSummarizer(opts TgptSummarizerOptions) *TgptSummarizer {
	bin := strings.TrimSpace(opts.Binary)
	if bin == "" {
		bin = "tgpt"
	}
	to := opts.Timeout
	if to == 0 {
		to = 45 * time.Second
	}
	env := make(map[string]string, len(opts.Env))
	for k, v := range opts.Env {
		env[k] = v
	}
	return &TgptSummarizer{
		Binary:    bin,
		Provider:  strings.TrimSpace(opts.Provider),
		Model:     strings.TrimSpace(opts.Model),
		Key:       strings.TrimSpace(opts.Key),
		URL:       strings.TrimSpace(opts.URL),
		Preprompt: strings.TrimSpace(opts.Preprompt),
		Timeout:   to,
		Env:       env,
	}
}

// Summarize shortens text to at most maxBytes using tgpt; no-op if already within limit.
func (t *TgptSummarizer) Summarize(text string, maxBytes int) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("tgpt summarizer: empty input")
	}
	if maxBytes <= 0 {
		return "", errors.New("tgpt summarizer: maxBytes must be > 0")
	}
	if len([]byte(text)) <= maxBytes {
		return text, nil
	}

	// Ask tgpt to summarize the stdin content.
	prompt := fmt.Sprintf(
		"Summarize the INPUT text (provided via stdin). "+
			"Return ONLY the summary as plain text (no markdown, no quotes). "+
			"Hard limit: at most %d bytes in UTF-8. Preserve key facts.",
		maxBytes,
	)

	out, err := t.run(prompt, text)
	if err != nil {
		return "", err
	}
	out = cleanOneLine(out)
	if out == "" {
		return "", errors.New("tgpt summarizer: empty output")
	}

	// If tgpt ignores length, retry once using the model output as input.
	if len([]byte(out)) > maxBytes {
		prompt2 := fmt.Sprintf(
			"Rewrite the INPUT (provided via stdin) to be at most %d bytes in UTF-8. "+
				"Return ONLY the rewritten text as plain text.",
			maxBytes,
		)
		out2, err2 := t.run(prompt2, out)
		if err2 == nil {
			out2 = cleanOneLine(out2)
			if out2 != "" {
				out = out2
			}
		}
	}

	if len([]byte(out)) > maxBytes {
		out = HardTruncateToBytes(out, maxBytes)
		out = strings.TrimSpace(out)
	}
	if out == "" {
		return "", errors.New("tgpt summarizer: output became empty after truncation")
	}
	return out, nil
}

// run invokes the tgpt binary with the given prompt and stdin text; returns stdout.
func (t *TgptSummarizer) run(prompt string, stdinText string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", errors.New("tgpt: empty prompt")
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if t.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, t.Timeout)
		defer cancel()
	}

	args := []string{"-q", "-w"}
	if t.Provider != "" {
		args = append(args, "--provider", t.Provider)
	}
	if t.Model != "" {
		args = append(args, "--model", t.Model)
	}
	if t.Key != "" {
		args = append(args, "--key", t.Key)
	}
	if t.URL != "" {
		args = append(args, "--url", t.URL)
	}
	if t.Preprompt != "" {
		args = append(args, "--preprompt", t.Preprompt)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, t.Binary, args...)
	cmd.Stdin = strings.NewReader(stdinText)

	if len(t.Env) > 0 {
		env := os.Environ()
		for k, v := range t.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Prefer stderr if present, else include stdout.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("tgpt: timed out after %s: %s", t.Timeout, msg)
		}
		return "", fmt.Errorf("tgpt: %s", msg)
	}

	out := stdout.String()
	if strings.TrimSpace(out) == "" {
		// Some tgpt builds may print to stderr.
		out = stderr.String()
	}
	out = strings.TrimSpace(out)
	return out, nil
}
