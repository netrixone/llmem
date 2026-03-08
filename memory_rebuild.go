package main

import (
	"log"
	"sort"
)

// RebuildEdgesOptions controls edge rebuilding behavior.
type RebuildEdgesOptions struct {
	// ForceRebuild if true, recalculates and overwrites all edges even if the
	// similarity hasn't changed. If false, only adds missing edges and updates
	// those whose similarity changed. Stale edges (below threshold) are always
	// removed regardless of this flag.
	ForceRebuild bool
	// MinSimilarity is the minimum similarity threshold for creating edges.
	// If zero or negative, uses the store's default delta.
	MinSimilarity float64
}

// RebuildEdgesResult reports what was rebuilt.
type RebuildEdgesResult struct {
	EdgesCreated    int      `json:"edgesCreated"`     // Number of new edges created
	EdgesUpdated    int      `json:"edgesUpdated"`     // Number of existing edges recalculated
	EdgesRemoved    int      `json:"edgesRemoved"`     // Number of edges removed (below threshold)
	ChunksProcessed int      `json:"chunksProcessed"`  // Number of chunks processed
	Errors          []string `json:"errors,omitempty"` // Any errors encountered
}

// RebuildEdges rebuilds similarity edges between all memories.
// This is useful for maintenance after changing the similarity delta or fixing edge corruption.
func (s *MemoryStore) RebuildEdges(opts RebuildEdgesOptions) (RebuildEdgesResult, error) {
	result := RebuildEdgesResult{
		Errors: make([]string, 0),
	}

	minSim := opts.MinSimilarity
	if minSim <= 0 {
		minSim = s.similarityDelta
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get all chunk IDs in a deterministic order
	chunkIDs := make([]string, 0, len(s.chunks))
	for id := range s.chunks {
		chunkIDs = append(chunkIDs, id)
	}
	sort.Strings(chunkIDs)

	result.ChunksProcessed = len(chunkIDs)

	// Track which chunks have been modified so we only persist those.
	dirty := make(map[string]struct{})

	// Compare each pair of chunks
	for i, aID := range chunkIDs {
		a := s.chunks[aID]
		if a == nil {
			continue
		}

		for j := i + 1; j < len(chunkIDs); j++ {
			bID := chunkIDs[j]
			b := s.chunks[bID]
			if b == nil {
				continue
			}

			// Check scope compatibility
			if !scopesCompatible(a.Scopes, b.Scopes) {
				continue
			}

			// Calculate similarity using BM25 (symmetric doc-doc, normalized to [0,1)).
			sim := s.bm25MemSimilarityLocked(a, b)

			// Check if edge should exist
			existingSim, hasEdge := a.edges[bID]

			if sim > minSim {
				// Edge should exist
				if !hasEdge || opts.ForceRebuild || existingSim != sim {
					s.addEdgeLocked(aID, a, bID, b, sim)
					dirty[aID] = struct{}{}
					dirty[bID] = struct{}{}
					if hasEdge {
						result.EdgesUpdated++
					} else {
						result.EdgesCreated++
					}
				}
			} else {
				// Edge should not exist (below threshold).
				// Always remove stale edges — even in non-force mode the caller
				// expects edges below the threshold to be cleaned up.
				if hasEdge {
					delete(a.edges, bID)
					delete(b.edges, aID)
					dirty[aID] = struct{}{}
					dirty[bID] = struct{}{}
					result.EdgesRemoved++
				}
			}
		}
	}

	// Persist only chunks whose edges were modified.
	for id := range dirty {
		chunk := s.chunks[id]
		if chunk == nil {
			continue
		}
		if err := s.storage.Save(chunk.toStorageData()); err != nil {
			result.Errors = append(result.Errors, "failed to save "+id+": "+err.Error())
		}
	}

	log.Printf("MEMORY: REBUILD_EDGES created=%d updated=%d removed=%d", result.EdgesCreated, result.EdgesUpdated, result.EdgesRemoved)
	return result, nil
}
