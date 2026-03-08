package main

import (
	"log"
	"sort"
	"time"
)

// MemoryListItem is a lightweight representation for listing memories.
type MemoryListItem struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Type      string     `json:"type,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	EdgeCount int        `json:"edgeCount"`
}

// List returns memories as lightweight items (no full text).
// If typeFilter is non-empty, only chunks with that type are returned.
// If scope is non-empty, only chunks matching that scope (or global) are returned.
// Sorted by creation time descending (newest first).
func (s *MemoryStore) List(typeFilter string, scope string) []MemoryListItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]MemoryListItem, 0, len(s.chunks))
	for _, sc := range s.chunks {
		if typeFilter != "" && sc.Type != typeFilter {
			continue
		}
		if !matchesScope(sc.Scopes, scope) {
			continue
		}
		items = append(items, MemoryListItem{
			ID:        sc.ID,
			Label:     sc.Label,
			Type:      sc.Type,
			CreatedAt: sc.CreatedAt,
			UpdatedAt: sc.UpdatedAt,
			EdgeCount: len(sc.edges),
		})
	}

	// Sort by creation time descending.
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	log.Printf("MEMORY: LIST type=%s scope=%s count=%d", typeFilter, scope, len(items))
	return items
}
