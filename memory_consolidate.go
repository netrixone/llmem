package main

import (
	"log"
	"sort"
	"strings"
)

// ConsolidationPair represents a candidate pair of memories that are similar enough
// to consider consolidating them into a single chunk.
type ConsolidationPair struct {
	AID        string  `json:"a_id"`
	BID        string  `json:"b_id"`
	Similarity float64 `json:"similarity"`
	Type       string  `json:"type,omitempty"`
}

// ConsolidationParams controls how candidate pairs are discovered.
type ConsolidationParams struct {
	// MinSimilarity is the minimum edge similarity required to include a pair.
	// If zero or negative, a conservative default (0.9) is used.
	MinSimilarity float64
	// TypeFilter, when non-empty, restricts pairs to memories where both sides
	// have this type.
	TypeFilter string
	// Limit caps the number of pairs returned. Zero means no limit.
	Limit int
}

// ConsolidateOptions controls how multiple memories are merged into one.
type ConsolidateOptions struct {
	// NewLabel, when non-empty, overrides the label for the merged chunk.
	// If empty, the label is derived from the merged text (same as Add()).
	NewLabel string
	// NewType, when non-empty, sets the type for the merged chunk.
	// If empty, the type is inferred from the source chunks (they must be compatible).
	NewType string
	// DeleteSources controls whether the source chunks are deleted after a successful merge.
	// If nil, the default is true.
	DeleteSources *bool
	// TextJoiner is inserted between source texts when building the merged text.
	// If empty, a clear separator is used.
	TextJoiner string
}

// defaultMinConsolidationSimilarity is intentionally high; callers can lower it explicitly.
const defaultMinConsolidationSimilarity = 0.9

// FindConsolidationPairs scans existing similarity edges and returns candidate pairs
// whose similarity exceeds the configured threshold. It is read-only; no mutations.
func (s *MemoryStore) FindConsolidationPairs(params ConsolidationParams) ([]ConsolidationPair, error) {
	minSim := params.MinSimilarity
	if minSim <= 0 {
		minSim = defaultMinConsolidationSimilarity
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	pairs := make([]ConsolidationPair, 0)
	seen := make(map[string]struct{})

	for id, sc := range s.chunks {
		// Apply type filter on the anchor side.
		if params.TypeFilter != "" && sc.Type != params.TypeFilter {
			continue
		}
		for otherID, sim := range sc.edges {
			if sim < minSim {
				continue
			}
			other := s.chunks[otherID]
			if other == nil {
				continue
			}
			// Apply type filter on the neighbor side.
			if params.TypeFilter != "" && other.Type != params.TypeFilter {
				continue
			}

			aID, bID := id, otherID
			if aID == bID {
				continue
			}
			if aID > bID {
				aID, bID = bID, aID
			}
			key := aID + "|" + bID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			pairType := sc.Type
			if pairType == "" {
				pairType = other.Type
			}

			pairs = append(pairs, ConsolidationPair{
				AID:        aID,
				BID:        bID,
				Similarity: sim,
				Type:       pairType,
			})

			if params.Limit > 0 && len(pairs) >= params.Limit {
				// We'll still sort below; limit is just a cap.
				break
			}
		}
	}

	// Sort by similarity desc, then by IDs for stability.
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Similarity == pairs[j].Similarity {
			if pairs[i].AID == pairs[j].AID {
				return pairs[i].BID < pairs[j].BID
			}
			return pairs[i].AID < pairs[j].AID
		}
		return pairs[i].Similarity > pairs[j].Similarity
	})

	if params.Limit > 0 && len(pairs) > params.Limit {
		pairs = pairs[:params.Limit]
	}

	log.Printf("MEMORY: CONSOLIDATION_CANDIDATES pairs=%d", len(pairs))
	return pairs, nil
}

// Consolidate merges the given memory IDs into a single new chunk, returning the merged
// chunk and the list of source IDs that were deleted (if any). It uses the same creation
// pipeline as Add() so that summarization, tokenization, and edges are handled consistently.
func (s *MemoryStore) Consolidate(ids []string, opts ConsolidateOptions) (MemoryChunk, []string, error) {
	// Normalize and deduplicate IDs.
	uniq := make([]string, 0, len(ids))
	seen := make(map[string]struct{})
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) < 2 {
		return MemoryChunk{}, nil, ErrTooFewIDs
	}

	// Read source chunks under a single lock for atomicity.
	// We read directly instead of using Get() to avoid inflating access counts
	// on chunks that are about to be deleted.
	sourceChunks := make([]MemoryChunk, 0, len(uniq))
	s.mu.RLock()
	for _, id := range uniq {
		sc := s.chunks[id]
		if sc == nil {
			s.mu.RUnlock()
			return MemoryChunk{}, nil, ErrNotFound
		}
		sourceChunks = append(sourceChunks, sc.MemoryChunk)
	}
	s.mu.RUnlock()

	// Determine resulting type.
	targetType := strings.TrimSpace(opts.NewType)
	if targetType == "" {
		// Infer type from sources; require compatibility when non-empty.
		for _, ch := range sourceChunks {
			if ch.Type == "" {
				continue
			}
			if targetType == "" {
				targetType = ch.Type
			} else if ch.Type != targetType {
				return MemoryChunk{}, nil, ErrIncompatibleTypes
			}
		}
	}

	// Build merged text in a deterministic order (by creation time, then ID).
	sort.Slice(sourceChunks, func(i, j int) bool {
		if sourceChunks[i].CreatedAt.Equal(sourceChunks[j].CreatedAt) {
			return sourceChunks[i].ID < sourceChunks[j].ID
		}
		return sourceChunks[i].CreatedAt.Before(sourceChunks[j].CreatedAt)
	})

	joiner := opts.TextJoiner
	if joiner == "" {
		joiner = "\n\n---\n\n"
	}
	textParts := make([]string, 0, len(sourceChunks))
	for _, ch := range sourceChunks {
		textParts = append(textParts, ch.Text)
	}
	mergedText := strings.Join(textParts, joiner)

	// Use Add() to create the new chunk so vectors/edges/token indexes and persistence are handled.
	// Preserve scopes if all source chunks have the same scopes; otherwise make it global.
	var mergedScopes []string
	if len(sourceChunks) > 0 {
		firstScopes := sourceChunks[0].Scopes
		scopeSet := make(map[string]struct{}, len(firstScopes))
		for _, s := range firstScopes {
			scopeSet[s] = struct{}{}
		}
		allSame := true
		for _, ch := range sourceChunks[1:] {
			if len(ch.Scopes) != len(firstScopes) {
				allSame = false
				break
			}
			for _, s := range ch.Scopes {
				if _, ok := scopeSet[s]; !ok {
					allSame = false
					break
				}
			}
			if !allSame {
				break
			}
		}
		if allSame {
			mergedScopes = firstScopes
		}
	}
	newLabel := strings.TrimSpace(opts.NewLabel)
	mergedChunk, _, err := s.Add(mergedText, newLabel, targetType, mergedScopes)
	if err != nil {
		return MemoryChunk{}, nil, err
	}

	// Decide whether to delete sources; default is true.
	deleteSources := true
	if opts.DeleteSources != nil {
		deleteSources = *opts.DeleteSources
	}

	removed := make([]string, 0)
	if deleteSources {
		for _, id := range uniq {
			if id == mergedChunk.ID {
				// Shouldn't happen (new ID), but be defensive.
				continue
			}
			if _, err := s.Delete(id); err == nil {
				removed = append(removed, id)
			}
		}
	}

	log.Printf("MEMORY: CONSOLIDATE merged=%s from=%v '%s'", mergedChunk.ID, removed, ExcerptForLog(mergedChunk.Text, logExcerptLen))
	return mergedChunk, removed, nil
}
