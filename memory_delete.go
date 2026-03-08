package main

import (
	"log"
	"strings"
)

// Delete removes a memory chunk by ID, including all edges referencing it.
// Returns the deleted chunk, or an error if not found.
func (s *MemoryStore) Delete(id string) (MemoryChunk, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MemoryChunk{}, ErrEmptyID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sc := s.chunks[id]
	if sc == nil {
		return MemoryChunk{}, ErrNotFound
	}

	// Remove edges from connected chunks.
	var updatedChunks []*storedChunk
	for otherID := range sc.edges {
		if other := s.chunks[otherID]; other != nil {
			delete(other.edges, id)
			updatedChunks = append(updatedChunks, other)
		}
	}

	// Remove from token index.
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

	// Remove from chunks map and update aggregate doc length.
	delete(s.chunks, id)
	if s.totalDocs > 0 {
		s.totalDocs--
	}
	s.totalDocLen -= sc.docLen
	if s.totalDocLen < 0 {
		s.totalDocLen = 0
	}

	// Delete from persistent storage.
	if err := s.storage.Delete(id); err != nil {
		// Continue even if storage delete fails - memory state is authoritative.
	}

	// Persist updated edges on formerly-connected chunks.
	for _, uc := range updatedChunks {
		_ = s.storage.Save(uc.toStorageData())
	}

	log.Printf("MEMORY: DELETE '%s' '%s'", id, ExcerptForLog(sc.Text, logExcerptLen))
	return sc.MemoryChunk, nil
}
