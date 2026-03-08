package main

import "log"

// AutoConsolidateOptions controls automatic consolidation behavior.
type AutoConsolidateOptions struct {
	// MinSimilarity is the minimum similarity threshold for auto-consolidation.
	// Defaults to 0.95 (very high similarity).
	MinSimilarity float64
	// TypeFilter restricts consolidation to memories of a specific type.
	// Empty means all types.
	TypeFilter string
	// MaxConsolidations limits how many pairs to consolidate in one run.
	// Zero means no limit (consolidate all qualifying pairs).
	MaxConsolidations int
	// DryRun if true, only reports what would be consolidated without making changes.
	DryRun bool
}

// AutoConsolidateResult reports what was consolidated.
type AutoConsolidateResult struct {
	Consolidated int      `json:"consolidated"` // Number of pairs consolidated
	Removed      []string `json:"removed"`      // IDs of removed source memories
	Merged       []string `json:"merged"`       // IDs of newly created merged memories
}

// AutoConsolidate automatically consolidates highly similar memories.
// This helps prevent memory bloat by merging redundant or near-duplicate memories.
// Returns the number of pairs consolidated and IDs of removed/merged memories.
func (s *MemoryStore) AutoConsolidate(opts AutoConsolidateOptions) (AutoConsolidateResult, error) {
	minSim := opts.MinSimilarity
	if minSim <= 0 {
		minSim = 0.95 // Very high threshold for auto-consolidation
	}

	// Find candidate pairs
	params := ConsolidationParams{
		MinSimilarity: minSim,
		TypeFilter:    opts.TypeFilter,
		Limit:         opts.MaxConsolidations,
	}
	pairs, err := s.FindConsolidationPairs(params)
	if err != nil {
		return AutoConsolidateResult{}, err
	}

	if len(pairs) == 0 {
		log.Printf("MEMORY: AUTO_CONSOLIDATE consolidated=0 removed=0 merged=0")
		return AutoConsolidateResult{Removed: []string{}, Merged: []string{}}, nil
	}

	result := AutoConsolidateResult{
		Removed: make([]string, 0),
		Merged:  make([]string, 0),
	}

	// Track which IDs we've already consolidated to avoid double-processing
	processed := make(map[string]struct{})

	for _, pair := range pairs {
		// Skip if either ID was already processed
		if _, ok := processed[pair.AID]; ok {
			continue
		}
		if _, ok := processed[pair.BID]; ok {
			continue
		}

		if opts.DryRun {
			// Just count what would be consolidated
			result.Consolidated++
			result.Removed = append(result.Removed, pair.AID, pair.BID)
			processed[pair.AID] = struct{}{}
			processed[pair.BID] = struct{}{}
			if opts.MaxConsolidations > 0 && result.Consolidated >= opts.MaxConsolidations {
				break
			}
			continue
		}

		// Consolidate the pair
		merged, removed, err := s.Consolidate([]string{pair.AID, pair.BID}, ConsolidateOptions{
			DeleteSources: boolPtr(true),
		})
		if err != nil {
			// Log but continue with other pairs
			continue
		}

		result.Consolidated++
		result.Merged = append(result.Merged, merged.ID)
		result.Removed = append(result.Removed, removed...)

		// Mark as processed
		processed[pair.AID] = struct{}{}
		processed[pair.BID] = struct{}{}
		processed[merged.ID] = struct{}{}

		// Respect max consolidations limit
		if opts.MaxConsolidations > 0 && result.Consolidated >= opts.MaxConsolidations {
			break
		}
	}

	log.Printf("MEMORY: AUTO_CONSOLIDATE consolidated=%d removed=%d merged=%d", result.Consolidated, len(result.Removed), len(result.Merged))
	return result, nil
}
