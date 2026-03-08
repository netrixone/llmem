package main

import (
	"errors"
	"log"
	"math"
	"sort"
	"strings"
)

// Search compares the input text against stored chunk content only (label is not
// used for similarity). Returns all matches with similarity > delta (sorted by similarity desc).
// If typeFilter is non-empty, only chunks with that type are considered.
// If scope is non-empty, only chunks matching that scope (or global) are considered.
// Hashtags in content (e.g. #decision) are indexed, so "search by #decision" works.
// For each matched chunk, it also returns suggested neighbors (IDs + labels).
func (s *MemoryStore) Search(text string, typeFilter string, scope string) ([]MemoryMatch, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("search text is empty")
	}

	qVec, _ := vectorize(text)
	qTokens := tokenSet(text)

	s.mu.RLock()
	defer s.mu.RUnlock()

	matches := s.scoreChunksLocked(qVec, qTokens, s.similarityDelta, typeFilter, scope)

	// Sort by similarity desc; break ties by accessCount (frequently accessed = more
	// likely important), then by ID for deterministic ordering.
	sort.Slice(matches, func(i, j int) bool {
		if math.Abs(matches[i].sim-matches[j].sim) < simEpsilon {
			if matches[i].chunk.accessCount != matches[j].chunk.accessCount {
				return matches[i].chunk.accessCount > matches[j].chunk.accessCount
			}
			return matches[i].chunk.ID < matches[j].chunk.ID
		}
		return matches[i].sim > matches[j].sim
	})

	// Limit before computing Neighbors (which iterates edges for each result).
	limit := len(matches)
	if s.maxResults > 0 && limit > s.maxResults {
		limit = s.maxResults
	}

	out := make([]MemoryMatch, limit)
	for i := 0; i < limit; i++ {
		out[i] = MemoryMatch{
			Chunk:      matches[i].chunk.MemoryChunk,
			Similarity: matches[i].sim,
			Neighbors:  s.neighborsLocked(matches[i].chunk, scope),
		}
	}
	log.Printf("MEMORY: SEARCH '%s'", ExcerptForLog(text, logExcerptLen))
	return out, nil
}
