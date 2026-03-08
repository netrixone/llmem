package main

import (
	"log"
	"sort"
	"strings"
	"time"
)

// ContextMemory is a memory returned by GetContext, with its category.
type ContextMemory struct {
	Category string      `json:"category"`
	Chunk    MemoryChunk `json:"chunk"`
}

// ContextResult contains categorized memories for quick context restoration.
type ContextResult struct {
	Memories []ContextMemory `json:"memories"`
}

// priorityTags defines which hashtags to look for and their priority order.
var priorityTags = []string{"#self", "#goal", "#relationship", "#status", "#principle", "#thought"}

// defaultPerCategory is the default number of memories returned per hashtag category.
const defaultPerCategory = 2

// GetContext returns important memories for context restoration.
// Looks for memories that START with priority hashtags (#self, #goal, #relationship, #status, #principle, #thought).
// Returns up to perCategory most recent memories per category, sorted by category priority.
// If perCategory is <= 0, the default (2) is used.
// If typeFilter is non-empty, only chunks with that type are considered.
// If scope is non-empty, only chunks matching that scope (or global) are considered.
func (s *MemoryStore) GetContext(typeFilter string, scope string, perCategory int) ContextResult {
	if perCategory <= 0 {
		perCategory = defaultPerCategory
	}
	log.Printf("MEMORY: CONTEXT type=%s scope=%s per_category=%d", typeFilter, scope, perCategory)

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Group memories by priority tag (must start with tag).
	byTag := make(map[string][]*storedChunk)
	for _, sc := range s.chunks {
		if typeFilter != "" && sc.Type != typeFilter {
			continue
		}
		if !matchesScope(sc.Scopes, scope) {
			continue
		}
		textLower := strings.ToLower(sc.Text)
		for _, tag := range priorityTags {
			if strings.HasPrefix(textLower, tag) {
				byTag[tag] = append(byTag[tag], sc)
				break // Only first matching tag
			}
		}
	}

	// Sort each group by most-recent time (newest first) and take top N.
	// Use UpdatedAt if set, otherwise CreatedAt — so recently updated memories
	// (e.g. #status) surface first even if they were created long ago.
	result := make([]ContextMemory, 0)
	for _, tag := range priorityTags {
		chunks := byTag[tag]
		if len(chunks) == 0 {
			continue
		}

		sort.Slice(chunks, func(i, j int) bool {
			return effectiveTime(chunks[i]).After(effectiveTime(chunks[j]))
		})

		limit := perCategory
		if len(chunks) < limit {
			limit = len(chunks)
		}
		for i := 0; i < limit; i++ {
			result = append(result, ContextMemory{
				Category: tag,
				Chunk:    chunks[i].MemoryChunk,
			})
		}
	}

	return ContextResult{Memories: result}
}

// FormatAsPrompt returns a plain-text block suitable for system prompt injection.
// Each memory is prefixed with its category. Use when MCP is unavailable (e.g. cron).
func (r ContextResult) FormatAsPrompt() string {
	if len(r.Memories) == 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range r.Memories {
		b.WriteString(m.Category)
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(m.Chunk.Text))
		b.WriteString("\n\n")
	}
	return strings.TrimSuffix(b.String(), "\n\n")
}

// effectiveTime returns the most recent meaningful timestamp for a chunk:
// UpdatedAt if set, otherwise CreatedAt.
func effectiveTime(sc *storedChunk) time.Time {
	if sc.UpdatedAt != nil {
		return *sc.UpdatedAt
	}
	return sc.CreatedAt
}
