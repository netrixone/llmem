package main

import (
	"errors"
	"log"
	"strings"
	"time"
)

// Update modifies an existing memory's text and optional label, recalculates similarity
// edges (content-based), and persists changes. Search uses content only.
// label is optional: if non-empty the chunk's label is set to it; if empty, existing label is kept.
// chunkType is optional: if non-empty the chunk's type is set to it; if empty, existing type is kept.
// scopes is optional: if nil, existing scopes are kept; if non-nil (even empty slice), scopes are updated.
func (s *MemoryStore) Update(id string, newText string, label string, chunkType string, scopes []string) (MemoryChunk, []RelatedMemory, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MemoryChunk{}, nil, ErrEmptyID
	}
	newText = strings.TrimSpace(newText)
	if newText == "" {
		return MemoryChunk{}, nil, errors.New("text is empty")
	}

	// Normalize text (summarize if too long).
	normalizedText := newText
	var err error
	if len([]byte(normalizedText)) > MaxMemoryBytes {
		normalizedText, err = s.summarizer.Summarize(normalizedText, MaxMemoryBytes)
		if err != nil {
			return MemoryChunk{}, nil, err
		}
		normalizedText = strings.TrimSpace(normalizedText)
		if normalizedText == "" {
			return MemoryChunk{}, nil, errors.New("summarizer returned empty text")
		}
		if len([]byte(normalizedText)) > MaxMemoryBytes {
			normalizedText = HardTruncateToBytes(normalizedText, MaxMemoryBytes)
		}
	}

	// Compute vectors from content only; label is not used for similarity.
	vec, norm := vectorize(normalizedText)
	tokens := tokenSet(normalizedText)

	s.mu.Lock()
	defer s.mu.Unlock()

	sc := s.chunks[id]
	if sc == nil {
		return MemoryChunk{}, nil, ErrNotFound
	}

	// Label: if non-empty, use the new label; if empty, keep existing label.
	label = strings.TrimSpace(label)
	if label == "" {
		label = sc.Label
	}

	// Remove old token index entries.
	for tok := range sc.tokens {
		if set := s.tokenIndex[tok]; set != nil {
			delete(set, id)
			if len(set) == 0 {
				delete(s.tokenIndex, tok)
			}
		}
		if s.tokenDocFreq[tok] > 0 {
			s.tokenDocFreq[tok]--
			if s.tokenDocFreq[tok] == 0 {
				delete(s.tokenDocFreq, tok)
			}
		}
	}

	// Remove old edges (both directions).
	oldEdgeIDs := make([]string, 0, len(sc.edges))
	for otherID := range sc.edges {
		oldEdgeIDs = append(oldEdgeIDs, otherID)
		if other := s.chunks[otherID]; other != nil {
			delete(other.edges, id)
		}
	}

	// Update chunk data and BM25 doc length aggregate.
	oldDocLen := sc.docLen
	newDocLen := docLenFromVector(vec)
	s.totalDocLen -= oldDocLen
	s.totalDocLen += newDocLen
	if s.totalDocLen < 0 {
		s.totalDocLen = 0
	}

	sc.Text = normalizedText
	sc.Label = label
	if strings.TrimSpace(chunkType) != "" {
		sc.Type = strings.TrimSpace(chunkType)
	}

	// Update scopes if provided
	if scopes != nil {
		sc.Scopes = normalizeScopes(scopes)
	}

	sc.vector = vec
	sc.norm = norm
	sc.tokens = tokens
	sc.docLen = newDocLen
	sc.edges = make(map[string]float64)
	// Keep original CreatedAt; track modification time.
	now := time.Now()
	sc.UpdatedAt = &now

	// Re-index tokens.
	s.indexChunkLocked(id, tokens)

	// Recalculate edges with all other chunks.
	// Only connect memories that are scope-compatible (share scopes or one/both are global).
	related := make([]RelatedMemory, 0)
	var updatedChunks []*storedChunk
	for otherID, other := range s.chunks {
		if otherID == id {
			continue
		}

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

	// Persist updated chunk.
	if err := s.storage.Save(sc.toStorageData()); err != nil {
		return MemoryChunk{}, nil, err
	}

	// Collect all dirty neighbor IDs (lost or gained edges), deduplicated.
	dirty := make(map[string]struct{}, len(oldEdgeIDs)+len(updatedChunks))
	for _, oldID := range oldEdgeIDs {
		dirty[oldID] = struct{}{}
	}
	for _, uc := range updatedChunks {
		dirty[uc.ID] = struct{}{}
	}
	for dirtyID := range dirty {
		if other := s.chunks[dirtyID]; other != nil {
			_ = s.storage.Save(other.toStorageData())
		}
	}

	log.Printf("MEMORY: UPDATE '%s' '%s'", id, ExcerptForLog(normalizedText, logExcerptLen))
	return sc.MemoryChunk, related, nil
}
