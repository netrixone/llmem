package main

import (
	"log"
	"sort"
	"time"
)

// MemoryStats contains aggregate statistics about the memory store.
type MemoryStats struct {
	TotalMemories  int       `json:"totalMemories"`
	TotalEdges     int       `json:"totalEdges"`
	TotalTokens    int       `json:"totalTokens"`
	OldestMemory   time.Time `json:"oldestMemory,omitempty"`
	NewestMemory   time.Time `json:"newestMemory,omitempty"`
	MostConnected  []string  `json:"mostConnected,omitempty"`  // Top 5 IDs by edge count
	AvgConnections float64   `json:"avgConnections"`
}

// Stats returns aggregate statistics about the memory store.
func (s *MemoryStore) Stats() MemoryStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := MemoryStats{
		TotalMemories: len(s.chunks),
		TotalTokens:   len(s.tokenDocFreq),
	}

	if stats.TotalMemories == 0 {
		log.Printf("MEMORY: STATS count=0")
		return stats
	}

	var totalEdges int
	var oldest, newest time.Time

	type connCount struct {
		id    string
		count int
	}
	connections := make([]connCount, 0, len(s.chunks))

	for id, sc := range s.chunks {
		edgeCount := len(sc.edges)
		totalEdges += edgeCount
		connections = append(connections, connCount{id: id, count: edgeCount})

		if oldest.IsZero() || sc.CreatedAt.Before(oldest) {
			oldest = sc.CreatedAt
		}
		if newest.IsZero() || sc.CreatedAt.After(newest) {
			newest = sc.CreatedAt
		}
	}

	// Edges are bidirectional, so divide by 2 for actual edge count.
	stats.TotalEdges = totalEdges / 2
	stats.OldestMemory = oldest
	stats.NewestMemory = newest
	stats.AvgConnections = float64(totalEdges) / float64(stats.TotalMemories)

	// Top 5 most connected.
	sort.Slice(connections, func(i, j int) bool {
		if connections[i].count == connections[j].count {
			return connections[i].id < connections[j].id
		}
		return connections[i].count > connections[j].count
	})
	top := 5
	if len(connections) < top {
		top = len(connections)
	}
	stats.MostConnected = make([]string, top)
	for i := 0; i < top; i++ {
		stats.MostConnected[i] = connections[i].id
	}

	log.Printf("MEMORY: STATS count=%d", stats.TotalMemories)
	return stats
}

// ExportChunk is a memory chunk with its edges, suitable for export/backup.
type ExportChunk struct {
	ID        string             `json:"id"`
	Text      string             `json:"text"`
	Label     string             `json:"label"`
	Type      string             `json:"type,omitempty"`
	Scopes    []string           `json:"scopes,omitempty"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt *time.Time         `json:"updatedAt,omitempty"`
	Edges     map[string]float64 `json:"edges,omitempty"`
}

// Export returns all memories in a format suitable for backup.
func (s *MemoryStore) Export() []ExportChunk {
	s.mu.RLock()
	defer s.mu.RUnlock()

	chunks := make([]ExportChunk, 0, len(s.chunks))
	for _, sc := range s.chunks {
		edges := make(map[string]float64, len(sc.edges))
		for k, v := range sc.edges {
			edges[k] = v
		}
		// Copy scopes for snapshot safety.
		var scopes []string
		if len(sc.Scopes) > 0 {
			scopes = make([]string, len(sc.Scopes))
			copy(scopes, sc.Scopes)
		}
		chunks = append(chunks, ExportChunk{
			ID:        sc.ID,
			Text:      sc.Text,
			Label:     sc.Label,
			Type:      sc.Type,
			Scopes:    scopes,
			CreatedAt: sc.CreatedAt,
			UpdatedAt: sc.UpdatedAt,
			Edges:     edges,
		})
	}

	// Sort by ID for deterministic output.
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].ID < chunks[j].ID
	})

	log.Printf("MEMORY: EXPORT count=%d", len(chunks))
	return chunks
}
