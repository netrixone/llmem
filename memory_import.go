package main

import (
	"errors"
	"log"
	"strings"
	"time"
)

// ImportChunk is the input format for importing memories.
type ImportChunk struct {
	ID        string     `json:"id,omitempty"`        // Optional - if provided, preserved when no conflict
	Text      string     `json:"text"`
	Label     string     `json:"label,omitempty"`     // Optional - short label/title
	Type      string     `json:"type,omitempty"`      // Optional - e.g. story, fact, artifact, note, decision
	Scopes    []string   `json:"scopes,omitempty"`    // Optional - project scopes
	CreatedAt time.Time  `json:"createdAt,omitempty"` // Optional - original creation time; zero = now
	UpdatedAt *time.Time `json:"updatedAt,omitempty"` // Optional - original update time; nil = never updated
}

// ImportResult contains the result of importing a single chunk.
type ImportResult struct {
	OriginalID string `json:"originalId,omitempty"`
	NewID      string `json:"newId"`
	Skipped    bool   `json:"skipped,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ImportResults summarizes a batch import.
type ImportResults struct {
	Imported int            `json:"imported"`
	Skipped  int            `json:"skipped"`
	Failed   int            `json:"failed"`
	Results  []ImportResult `json:"results"`
}

// Import adds multiple memories from an export or manual list.
// Each chunk is processed independently - failures don't stop other imports.
// If skipExisting is true, chunks with IDs that already exist are skipped.
// When a chunk has an ID, Import tries to preserve it (unless it conflicts with
// an existing memory). When a chunk has a CreatedAt, Import preserves the original
// timestamp instead of using time.Now(). This enables lossless backup/restore via
// Export → Import round-trips.
func (s *MemoryStore) Import(chunks []ImportChunk, skipExisting bool) ImportResults {
	results := ImportResults{
		Results: make([]ImportResult, 0, len(chunks)),
	}

	for _, chunk := range chunks {
		text := strings.TrimSpace(chunk.Text)
		if text == "" {
			results.Failed++
			results.Results = append(results.Results, ImportResult{
				OriginalID: chunk.ID,
				Error:      "empty text",
			})
			continue
		}

		opts := addOpts{createdAt: chunk.CreatedAt, updatedAt: chunk.UpdatedAt}

		if chunk.ID != "" {
			s.mu.RLock()
			_, exists := s.chunks[chunk.ID]
			s.mu.RUnlock()

			if exists && skipExisting {
				results.Skipped++
				results.Results = append(results.Results, ImportResult{
					OriginalID: chunk.ID,
					NewID:      chunk.ID,
					Skipped:    true,
				})
				continue
			}

			// Preserve original ID when slot is available.
			if !exists {
				opts.id = chunk.ID
			}
			// If exists and !skipExisting, fall through with empty opts.id
			// to get an auto-generated ID (upsert is not the intent of import).
		}

		// Add as new memory, preserving label, type, scopes, and optionally ID/timestamp.
		newChunk, _, err := s.addInternal(text, strings.TrimSpace(chunk.Label), strings.TrimSpace(chunk.Type), chunk.Scopes, opts)
		if err != nil {
			results.Failed++
			results.Results = append(results.Results, ImportResult{
				OriginalID: chunk.ID,
				Error:      err.Error(),
			})
			continue
		}

		results.Imported++
		results.Results = append(results.Results, ImportResult{
			OriginalID: chunk.ID,
			NewID:      newChunk.ID,
		})
	}

	log.Printf("MEMORY: IMPORT imported=%d skipped=%d failed=%d", results.Imported, results.Skipped, results.Failed)
	return results
}

// ImportOne imports a single chunk. Convenience wrapper around Import.
func (s *MemoryStore) ImportOne(text string) (MemoryChunk, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return MemoryChunk{}, errors.New("empty text")
	}
	chunk, _, err := s.Add(text, "", "", nil)
	return chunk, err
}
