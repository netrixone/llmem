package main

import (
	"log"
	"strings"
)

// Get retrieves a stored chunk by ID and suggests its neighbors (IDs + labels).
func (s *MemoryStore) Get(id string) (MemoryChunk, []RelatedMemory, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MemoryChunk{}, nil, ErrEmptyID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sc := s.chunks[id]
	if sc == nil {
		return MemoryChunk{}, nil, ErrNotFound
	}
	s.trackAccessLocked(sc)

	log.Printf("MEMORY: GET '%s'", id)
	// Return all neighbors (they're already scope-compatible due to edge creation)
	return sc.MemoryChunk, s.neighborsLocked(sc, ""), nil
}
