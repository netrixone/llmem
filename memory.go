package main

import (
	"errors"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// excerptLenForLog returns the excerpt length for MEMORY logs from env LLMEM_LOG_EXCERPT_LEN (default 80).
var logExcerptLen int

func init() {
	n, _ := strconv.Atoi(envOr("LLMEM_LOG_EXCERPT_LEN", "80"))
	if n >= 0 {
		logExcerptLen = n
	}
}

// Sentinel errors for error classification across package/layer boundaries.
var (
	ErrNotFound          = errors.New("memory not found")
	ErrEmptyID           = errors.New("id is empty")
	ErrIncompatibleTypes = errors.New("incompatible types; provide explicit new_type to override")
	ErrTooFewIDs         = errors.New("need at least two distinct ids to consolidate")
)

const (
	// MaxMemoryBytes is the maximum stored size of a memory chunk.
	// If the input exceeds this, the store will call the configured Summarizer.
	MaxMemoryBytes = 1024 * 1024

	// DefaultSimilarityDelta controls which memories get connected.
	// Used as threshold for similarity (BM25 normalized to [0,1); query-doc may also use lexical boost).
	DefaultSimilarityDelta = 0.35

	// simEpsilon absorbs floating-point noise in score comparisons.
	// BM25 scoring iterates over Go maps whose order is randomized, making
	// float accumulation order-dependent: (a+b)+c != a+(b+c). This epsilon
	// ensures that identical-content documents sort by the intended tiebreaker
	// (ID or recency) rather than by arithmetic noise.
	simEpsilon = 1e-12
)

// RelatedMemory is returned from Add() for each connected memory.
type RelatedMemory struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	Similarity float64   `json:"similarity"`
	CreatedAt  time.Time `json:"createdAt"`
}

// MemoryMatch is returned from Search with the matched chunk and suggested neighbors.
type MemoryMatch struct {
	Chunk      MemoryChunk     `json:"chunk"`
	Similarity float64         `json:"similarity"`
	Neighbors  []RelatedMemory `json:"neighbors"`
}

// MemoryChunk is a stored, normalized chunk (already summarized if necessary).
type MemoryChunk struct {
	ID        string     `json:"ID,omitempty"`
	Text      string     `json:"text,omitempty"`
	Label     string     `json:"label,omitempty"`
	Type      string     `json:"type,omitempty"`
	Scopes    []string   `json:"scopes,omitempty"` // Empty/nil = global, matches all queries
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"` // Nil until first Update(); set on each Update()
}

// storedChunk is the internal representation of a memory chunk.
type storedChunk struct {
	MemoryChunk
	vector map[string]float64  // Vector of term frequencies.
	norm   float64             // Normalization factor for BM25.
	tokens map[string]struct{} // Set of tokens in the chunk.
	edges  map[string]float64  // Map of connected chunk IDs to their similarity score.

	// docLen is the sum of term frequencies (total token count) for BM25 length normalization.
	docLen float64

	// Access tracking for importance weighting
	lastAccessed time.Time
	accessCount  uint64
}

// scoredChunk holds a chunk with its computed similarity scores.
// Used by Search and FindRelevant as the shared scoring output.
type scoredChunk struct {
	chunk   *storedChunk
	sim     float64 // Best of BM25 (normalized) and lexical overlap.
	bm25Raw float64 // Raw BM25 score (for tiebreaking).
}

// MemoryStore stores chunks in RAM and connects them by similarity.
// Retrieval/querying is supported via content similarity and ID lookups.
// Optionally backed by persistent storage.
type MemoryStore struct {
	summarizer      Summarizer // Summarizer interface for summarizing text.
	storage         Storage    // Storage interface for persistence.
	similarityDelta float64    // Threshold for “what counts as a Search hit” and “what counts as a connected pair” in the graph.
	maxResults      int        // Maximum number of results to return from Search.

	mu     sync.RWMutex   // Mutex for concurrent access to the store.
	nextID uint64         // Next ID to assign to a new chunk.
	wg     sync.WaitGroup // WaitGroup to track in-flight async storage writes (e.g. access tracking).

	chunks       map[string]*storedChunk        // Map of chunk IDs to stored chunks.
	tokenIndex   map[string]map[string]struct{} // Map of token to chunk IDs containing that token.
	tokenDocFreq map[string]uint64              // Map of token to the number of chunks containing that token.
	totalDocs    uint64                         // Total number of chunks in the store.
	totalDocLen  float64                        // Total length of all chunks in the store.
}

// MemoryStoreOptions configures a new MemoryStore.
type MemoryStoreOptions struct {
	// Summarizer is optional; if nil, a default truncating implementation is used.
	Summarizer Summarizer

	// Storage is optional; if nil, NullStorage is used (no persistence).
	Storage Storage

	// SimilarityDelta is optional; if 0, DefaultSimilarityDelta is used.
	SimilarityDelta float64

	// MaxResults limits Search results when > 0. Zero means no limit.
	MaxResults int
}

// NewMemoryStore creates a new in-memory store with the given options.
// If Storage is provided, it loads existing chunks from persistent storage.
func NewMemoryStore(opts MemoryStoreOptions) (*MemoryStore, error) {
	sum := opts.Summarizer
	if sum == nil {
		sum = NewTruncater()
	}

	stor := opts.Storage
	if stor == nil {
		stor = NullStorage{}
	}

	similarityDelta := opts.SimilarityDelta
	if similarityDelta == 0 {
		similarityDelta = DefaultSimilarityDelta
	}

	store := &MemoryStore{
		summarizer:      sum,
		storage:         stor,
		similarityDelta: similarityDelta,
		maxResults:      opts.MaxResults,
		chunks:          make(map[string]*storedChunk),
		tokenIndex:      make(map[string]map[string]struct{}),
		tokenDocFreq:    make(map[string]uint64),
	}

	// Load existing chunks from storage.
	if err := store.loadFromStorage(); err != nil {
		return nil, err
	}

	return store, nil
}

// loadFromStorage loads all chunks from persistent storage into memory.
func (s *MemoryStore) loadFromStorage() error {
	chunks, err := s.storage.LoadAll()
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var maxID uint64
	for _, data := range chunks {
		// Re-tokenize from text to ensure tokens match current tokenization logic
		// (e.g. after adding stemming). This is cheap for typical store sizes.
		vec, norm := vectorize(data.Text)
		tokens := tokenSet(data.Text)
		docLen := docLenFromVector(vec)

		sc := &storedChunk{
			MemoryChunk: MemoryChunk{
				ID:        data.ID,
				Text:      data.Text,
				Label:     data.Label,
				Type:      data.Type,
				Scopes:    data.Scopes,
				CreatedAt: data.CreatedAt,
				UpdatedAt: data.UpdatedAt,
			},
			vector:       vec,
			norm:         norm,
			tokens:       tokens,
			docLen:       docLen,
			edges:        data.Edges,
			lastAccessed: time.Time{},
			accessCount:  data.AccessCount,
		}
		if data.LastAccessed != nil {
			sc.lastAccessed = *data.LastAccessed
		}

		s.chunks[data.ID] = sc
		s.indexChunkLocked(data.ID, tokens)
		s.totalDocs++
		s.totalDocLen += docLen

		// Track max ID for next ID generation.
		if id := parseChunkID(data.ID); id > maxID {
			maxID = id
		}
	}

	s.nextID = maxID
	return nil
}

// parseChunkID extracts the numeric part from an ID like "m123".
func parseChunkID(id string) uint64 {
	if len(id) < 2 || id[0] != 'm' {
		return 0
	}
	var n uint64
	for i := 1; i < len(id); i++ {
		c := id[i]
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}

// newID returns the next unique chunk ID (e.g. m1, m2).
func (s *MemoryStore) newID() string {
	n := atomic.AddUint64(&s.nextID, 1)
	// Stable, readable, no extra deps; swap to UUID later if desired.
	return "m" + itoaBase10(n)
}

// neighborsLocked returns neighbors of sc sorted by edge similarity (caller must hold s.mu).
// If filterScope is non-empty, only returns neighbors matching that scope (or global).
func (s *MemoryStore) neighborsLocked(sc *storedChunk, filterScope string) []RelatedMemory {
	if sc == nil || len(sc.edges) == 0 {
		return []RelatedMemory{}
	}

	type tmp struct {
		id  string
		lab string
		sim float64
		at  time.Time
	}
	tmpList := make([]tmp, 0, len(sc.edges))
	for nid, sim := range sc.edges {
		n := s.chunks[nid]
		if n == nil {
			continue
		}
		// Filter by scope if requested
		if filterScope != "" && !matchesScope(n.Scopes, filterScope) {
			continue
		}
		tmpList = append(tmpList, tmp{id: nid, lab: n.Label, sim: sim, at: n.CreatedAt})
	}

	sort.Slice(tmpList, func(i, j int) bool {
		if tmpList[i].sim == tmpList[j].sim {
			return tmpList[i].id < tmpList[j].id
		}
		return tmpList[i].sim > tmpList[j].sim
	})

	out := make([]RelatedMemory, 0, len(tmpList))
	for _, e := range tmpList {
		out = append(out, RelatedMemory{ID: e.id, Label: e.lab, Similarity: e.sim, CreatedAt: e.at})
	}
	return out
}

// addEdgeLocked records a bidirectional edge between a and b with the given similarity (caller must hold s.mu).
func (s *MemoryStore) addEdgeLocked(aID string, a *storedChunk, bID string, b *storedChunk, sim float64) {
	if a.edges == nil {
		a.edges = make(map[string]float64)
	}
	if b.edges == nil {
		b.edges = make(map[string]float64)
	}
	a.edges[bID] = sim
	b.edges[aID] = sim
}

func (s *MemoryStore) indexChunkLocked(id string, tokens map[string]struct{}) {
	if len(tokens) == 0 {
		return
	}
	for tok := range tokens {
		s.tokenDocFreq[tok]++
		set := s.tokenIndex[tok]
		if set == nil {
			set = make(map[string]struct{})
			s.tokenIndex[tok] = set
		}
		set[id] = struct{}{}
	}
}

func (s *MemoryStore) lexicalOverlapScoreLocked(queryTokens, docTokens map[string]struct{}) float64 {
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}
	var overlap float64
	var total float64
	for tok := range queryTokens {
		w := s.idfLocked(tok)
		total += w
		if _, ok := docTokens[tok]; ok {
			overlap += w
		}
	}
	if total == 0 {
		return 0
	}
	return overlap / total
}

func (s *MemoryStore) idfLocked(tok string) float64 {
	df := s.tokenDocFreq[tok]
	// Smooth with +1 to avoid divide-by-zero and keep values sane at small N.
	return math.Log((1+float64(s.totalDocs))/(1+float64(df))) + 1
}

// scoreChunksLocked computes similarity scores for all chunks matching the filters.
// Returns chunks scoring above threshold. Must be called with s.mu held for reading.
// qVec is the query term-frequency vector (expanded with corpus-aware synonyms).
// qTokens is the original (unexpanded) token set for lexical overlap scoring.
func (s *MemoryStore) scoreChunksLocked(qVec map[string]float64, qTokens map[string]struct{}, threshold float64, typeFilter, scope string) []scoredChunk {
	// Expand query using corpus-aware synonyms.
	qVec = expandQueryVectorCorpus(qVec, s.tokenIndex)
	qTokensExpanded := expandTokenSetCorpus(qTokens, s.tokenIndex)

	// Find lexical candidates using expanded tokens (broader recall).
	lexicalCandidates := make(map[string]struct{})
	for tok := range qTokensExpanded {
		for id := range s.tokenIndex[tok] {
			lexicalCandidates[id] = struct{}{}
		}
	}

	hasTokens := len(qTokens) > 0
	var matches []scoredChunk

	for _, sc := range s.chunks {
		if typeFilter != "" && sc.Type != typeFilter {
			continue
		}
		if !matchesScope(sc.Scopes, scope) {
			continue
		}
		bm25Raw := s.bm25ScoreLocked(qVec, sc)
		contentSim := bm25Raw / (1 + bm25Raw) // normalize to [0,1)
		bestSim := contentSim
		if hasTokens {
			if _, ok := lexicalCandidates[sc.ID]; ok {
				// Use original (unexpanded) tokens for overlap scoring
				// to avoid diluting the overlap ratio with broad synonyms.
				lexicalScore := s.lexicalOverlapScoreLocked(qTokens, sc.tokens)
				if lexicalScore > bestSim {
					bestSim = lexicalScore
				}
			}
		}
		if bestSim > threshold {
			matches = append(matches, scoredChunk{chunk: sc, sim: bestSim, bm25Raw: bm25Raw})
		}
	}
	return matches
}

// toStorageData converts a storedChunk to storedChunkData for persistence.
// The returned struct is a deep copy — safe to use after releasing locks.
func (sc *storedChunk) toStorageData() storedChunkData {
	tokens := make([]string, 0, len(sc.tokens))
	for tok := range sc.tokens {
		tokens = append(tokens, tok)
	}

	// Deep-copy maps so the snapshot is independent of the live chunk.
	vector := make(map[string]float64, len(sc.vector))
	for k, v := range sc.vector {
		vector[k] = v
	}
	edges := make(map[string]float64, len(sc.edges))
	for k, v := range sc.edges {
		edges[k] = v
	}

	// Copy scopes slice.
	var scopes []string
	if sc.Scopes != nil {
		scopes = make([]string, len(sc.Scopes))
		copy(scopes, sc.Scopes)
	}

	var lastAccessed *time.Time
	if !sc.lastAccessed.IsZero() {
		t := sc.lastAccessed
		lastAccessed = &t
	}
	return storedChunkData{
		ID:           sc.ID,
		Text:         sc.Text,
		Label:        sc.Label,
		Type:         sc.Type,
		Scopes:       scopes,
		CreatedAt:    sc.CreatedAt,
		UpdatedAt:    sc.UpdatedAt,
		Vector:       vector,
		Norm:         sc.norm,
		Tokens:       tokens,
		Edges:        edges,
		LastAccessed: lastAccessed,
		AccessCount:  sc.accessCount,
	}
}

// trackAccessLocked increments access count and updates last accessed time.
// Caller must hold the write lock (s.mu.Lock) since this mutates the chunk.
func (s *MemoryStore) trackAccessLocked(sc *storedChunk) {
	if sc == nil {
		return
	}

	sc.lastAccessed = time.Now()
	sc.accessCount++
	// Snapshot while lock is held; the goroutine must not touch sc directly.
	data := sc.toStorageData()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_ = s.storage.Save(data)
	}()
}

// matchesScope checks if a memory matches the given query scope.
// Global memories (empty scopes) match all queries.
// If queryScope is empty, all memories match (no filtering).
func matchesScope(memoryScopes []string, queryScope string) bool {
	// Global memories match everything
	if len(memoryScopes) == 0 {
		return true
	}
	// No scope filter = return all
	if queryScope == "" {
		return true
	}
	// Check if query scope is in memory's scopes
	for _, s := range memoryScopes {
		if s == queryScope {
			return true
		}
	}
	return false
}

// scopesCompatible checks if two memories should be connected based on their scopes.
// Returns true if:
//   - Both are global (empty scopes)
//   - One is global and one is scoped (global connects to everything)
//   - Both are scoped and share at least one scope
//
// Returns false if both are scoped but have no shared scopes.
func scopesCompatible(aScopes, bScopes []string) bool {
	// Both global - always compatible
	if len(aScopes) == 0 && len(bScopes) == 0 {
		return true
	}
	// One global, one scoped - compatible (global connects to everything)
	if len(aScopes) == 0 || len(bScopes) == 0 {
		return true
	}
	// Both scoped - check for overlap
	// Build a set of a's scopes for quick lookup
	aSet := make(map[string]struct{}, len(aScopes))
	for _, s := range aScopes {
		aSet[s] = struct{}{}
	}
	// Check if any of b's scopes are in a's scopes
	for _, s := range bScopes {
		if _, ok := aSet[s]; ok {
			return true
		}
	}
	return false
}

// Close waits for in-flight async writes to finish, then releases storage resources.
func (s *MemoryStore) Close() error {
	log.Printf("MEMORY: CLOSE")
	s.wg.Wait()
	return s.storage.Close()
}

// defaultFallbackLabel builds a label from the first ~8 words of text, capped to ~60 bytes.
// Uses raw words (not stemmed tokens) so the label stays human-readable.
// Unlike HardTruncateToBytes, this respects word boundaries to avoid mid-word cuts.
func defaultFallbackLabel(text string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return "memory"
	}
	if len(words) > 8 {
		words = words[:8]
	}

	// Build label word-by-word, stopping before exceeding 60 bytes.
	const maxBytes = 60
	var label strings.Builder
	for i, w := range words {
		nextLen := len([]byte(w))
		if i > 0 {
			nextLen++ // account for space separator
		}
		if label.Len()+nextLen > maxBytes {
			break
		}
		if i > 0 {
			label.WriteByte(' ')
		}
		label.WriteString(w)
	}

	if label.Len() == 0 {
		return "memory"
	}
	return label.String()
}
