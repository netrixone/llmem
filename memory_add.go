package main

import (
	"errors"
	"log"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// addOpts holds optional overrides for chunk insertion.
// Used by Import to preserve original IDs and timestamps from exported data.
type addOpts struct {
	id        string     // Use this ID instead of generating one. Empty = auto-generate.
	createdAt time.Time  // Use this timestamp instead of time.Now(). Zero = now.
	updatedAt *time.Time // Preserve original update timestamp from export. Nil = never updated.
}

// Add inserts a new memory chunk, connects it to existing chunks with similarity > delta,
// and returns the related chunks (id, label, similarity) sorted by decreasing similarity.
// The new chunk and updated related chunks are persisted to storage.
// Label is optional: if non-empty it is used as the chunk's label; otherwise a trivial
// fallback (first ~8 tokens of text, capped at ~60 bytes) is used. Search uses content only.
// Type is optional (e.g. story, fact, artifact, note, decision); empty means unspecified.
// Scopes is optional: if nil or empty, the memory is global (matches all queries);
// otherwise it only matches queries with matching scope.
func (s *MemoryStore) Add(text string, label string, chunkType string, scopes []string) (newChunk MemoryChunk, related []RelatedMemory, err error) {
	return s.addInternal(text, label, chunkType, scopes, addOpts{})
}

// addInternal is the shared implementation for Add and Import.
// It normalizes text, optionally summarizes, vectorizes, creates edges, indexes, and persists.
func (s *MemoryStore) addInternal(text string, label string, chunkType string, scopes []string, opts addOpts) (newChunk MemoryChunk, related []RelatedMemory, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return MemoryChunk{}, nil, errors.New("memory text is empty")
	}

	normalizedText := text
	if len([]byte(normalizedText)) > MaxMemoryBytes {
		normalizedText, err = s.summarizer.Summarize(normalizedText, MaxMemoryBytes)
		if err != nil {
			return MemoryChunk{}, nil, err
		}
		normalizedText = strings.TrimSpace(normalizedText)
		if normalizedText == "" {
			return MemoryChunk{}, nil, errors.New("summarizer returned empty memory")
		}
		if len([]byte(normalizedText)) > MaxMemoryBytes {
			// Summarizer should respect maxBytes; enforce anyway to avoid RAM blowups.
			normalizedText = HardTruncateToBytes(normalizedText, MaxMemoryBytes)
		}
	}

	// Optional user-defined label; otherwise trivial fallback (no tgpt/summarizer).
	label = strings.TrimSpace(label)
	if label == "" {
		// Use a local, deterministic label builder rather than calling the configured
		// Summarizer. This keeps label generation cheap and avoids network calls.
		label = defaultFallbackLabel(normalizedText)
	}

	vec, norm := vectorize(normalizedText)
	tokens := tokenSet(normalizedText)

	// Use provided ID or generate new one.
	id := opts.id
	if id == "" {
		id = s.newID()
	}

	// Use provided timestamp or now.
	now := opts.createdAt
	if now.IsZero() {
		now = time.Now()
	}

	chunkType = strings.TrimSpace(chunkType)

	docLen := docLenFromVector(vec)
	sc := &storedChunk{
		MemoryChunk: MemoryChunk{
			ID:        id,
			Text:      normalizedText,
			Label:     label,
			Type:      chunkType,
			Scopes:    normalizeScopes(scopes),
			CreatedAt: now,
			UpdatedAt: opts.updatedAt, // Nil for new memories, set from import data
		},
		vector:       vec,
		norm:         norm,
		tokens:       tokens,
		docLen:       docLen,
		edges:        make(map[string]float64),
		lastAccessed: time.Time{},
		accessCount:  0,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// If using a provided ID, ensure nextID stays ahead to avoid future collisions.
	if opts.id != "" {
		if n := parseChunkID(opts.id); n > 0 {
			for {
				cur := atomic.LoadUint64(&s.nextID)
				if n <= cur {
					break
				}
				if atomic.CompareAndSwapUint64(&s.nextID, cur, n) {
					break
				}
			}
		}
	}

	// Compare against existing chunks and create connections. For each similar pair we update
	// both the new chunk's edges and the existing chunk's edges so that all similar relations
	// stay consistent (symmetric).
	// Only connect memories that are scope-compatible (share scopes or one/both are global).
	related = make([]RelatedMemory, 0)
	var updatedChunks []*storedChunk
	for otherID, other := range s.chunks {
		// Check scope compatibility before creating edge
		if !scopesCompatible(sc.Scopes, other.Scopes) {
			continue
		}
		sim := s.bm25MemSimilarityLocked(sc, other)
		if sim > s.similarityDelta {
			s.addEdgeLocked(id, sc, otherID, other, sim)
			updatedChunks = append(updatedChunks, other)
			related = append(related, RelatedMemory{
				ID:         otherID,
				Label:      other.Label,
				Similarity: sim,
				CreatedAt:  other.CreatedAt,
			})
		}
	}

	// Store in memory.
	s.chunks[id] = sc
	s.indexChunkLocked(id, tokens)
	s.totalDocs++
	s.totalDocLen += docLen

	// Persist new chunk to storage.
	if err := s.storage.Save(sc.toStorageData()); err != nil {
		return MemoryChunk{}, nil, err
	}

	// Persist updated edges on related chunks.
	for _, uc := range updatedChunks {
		if err := s.storage.Save(uc.toStorageData()); err != nil {
			// Log but don't fail - the new chunk is already saved.
			// Edges will be rebuilt on next restart if needed.
			break
		}
	}

	sort.Slice(related, func(i, j int) bool {
		if related[i].Similarity == related[j].Similarity {
			return related[i].ID < related[j].ID
		}
		return related[i].Similarity > related[j].Similarity
	})

	log.Printf("MEMORY: ADD '%s'", ExcerptForLog(sc.Text, logExcerptLen))
	return sc.MemoryChunk, related, nil
}
