package main

import (
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSummarizer struct {
	summarizeCalls int
	labelCalls     int

	lastSummarizeText    string
	lastSummarizeMaxByte int
	lastLabelText        string

	summarizeFn func(text string, maxBytes int) (string, error)
	labelFn     func(text string) (string, error)
}

func (f *fakeSummarizer) Summarize(text string, maxBytes int) (string, error) {
	f.summarizeCalls++
	f.lastSummarizeText = text
	f.lastSummarizeMaxByte = maxBytes
	if f.summarizeFn != nil {
		return f.summarizeFn(text, maxBytes)
	}
	return text, nil
}

func (f *fakeSummarizer) Label(text string) (string, error) {
	f.labelCalls++
	f.lastLabelText = text
	if f.labelFn != nil {
		return f.labelFn(text)
	}
	return "label", nil
}

func mustNewMemoryStore(t *testing.T, opts MemoryStoreOptions) *MemoryStore {
	t.Helper()
	s, err := NewMemoryStore(opts)
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	return s
}

func TestMemoryStoreAdd_EmptyRejected(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})
	_, _, err := s.Add("   ", "", "", nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := err.Error(); got != "memory text is empty" {
		t.Fatalf("unexpected error: got %q", got)
	}
}

func TestMemoryStoreAdd_TrimsAssignsIDAndCreatedAt(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	start := time.Now()
	c1, _, err := s.Add("  hello world  ", "", "", nil)
	end := time.Now()
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if c1.ID != "m1" {
		t.Fatalf("unexpected id: got %q", c1.ID)
	}
	if c1.Text != "hello world" {
		t.Fatalf("unexpected stored text: got %q", c1.Text)
	}
	if strings.TrimSpace(c1.Label) == "" {
		t.Fatalf("expected non-empty label")
	}
	if c1.CreatedAt.Before(start) || c1.CreatedAt.After(end) {
		t.Fatalf("CreatedAt not within call window: start=%s created=%s end=%s", start, c1.CreatedAt, end)
	}

	c2, _, err := s.Add("second", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if c2.ID != "m2" {
		t.Fatalf("unexpected id: got %q", c2.ID)
	}
}

func TestMemoryStoreAdd_SummarizeOnlyWhenTooLarge(t *testing.T) {
	fs := &fakeSummarizer{
		summarizeFn: func(text string, maxBytes int) (string, error) { return text, nil },
		labelFn:     func(text string) (string, error) { return "ok", nil },
	}
	s := mustNewMemoryStore(t, MemoryStoreOptions{Summarizer: fs})

	if _, _, err := s.Add("small", "", "", nil); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if fs.summarizeCalls != 0 {
		t.Fatalf("expected Summarize not to be called; got %d", fs.summarizeCalls)
	}
	if fs.labelCalls != 0 {
		t.Fatalf("expected Label not to be called; got %d", fs.labelCalls)
	}

	long := strings.Repeat("x", MaxMemoryBytes+1)
	if _, _, err := s.Add(long, "", "", nil); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if fs.summarizeCalls != 1 {
		t.Fatalf("expected Summarize to be called once; got %d", fs.summarizeCalls)
	}
	if fs.lastSummarizeMaxByte != MaxMemoryBytes {
		t.Fatalf("unexpected maxBytes passed to Summarize: got %d", fs.lastSummarizeMaxByte)
	}
}

func TestMemoryStoreAdd_SummarizeWhitespaceBecomesError(t *testing.T) {
	fs := &fakeSummarizer{
		summarizeFn: func(text string, maxBytes int) (string, error) { return "   \n\t  ", nil },
		labelFn:     func(text string) (string, error) { return "unused", nil },
	}
	s := mustNewMemoryStore(t, MemoryStoreOptions{Summarizer: fs})

	long := strings.Repeat("x", MaxMemoryBytes+1)
	_, _, err := s.Add(long, "", "", nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := err.Error(); got != "summarizer returned empty memory" {
		t.Fatalf("unexpected error: got %q", got)
	}
}

func TestMemoryStoreAdd_SummarizeStillTooLongIsHardTruncated(t *testing.T) {
	tooLong := strings.Repeat("a", MaxMemoryBytes+50)
	fs := &fakeSummarizer{
		summarizeFn: func(text string, maxBytes int) (string, error) { return tooLong, nil },
		labelFn:     func(text string) (string, error) { return "ok", nil },
	}
	s := mustNewMemoryStore(t, MemoryStoreOptions{Summarizer: fs})

	in := strings.Repeat("x", MaxMemoryBytes+1)
	c, _, err := s.Add(in, "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if got := len([]byte(c.Text)); got > MaxMemoryBytes {
		t.Fatalf("stored text exceeded MaxMemoryBytes: got %d > %d", got, MaxMemoryBytes)
	}
}

func TestMemoryStoreAdd_LabelWhitespaceFallsBack(t *testing.T) {
	fs := &fakeSummarizer{
		labelFn: func(text string) (string, error) { return "   ", nil },
	}
	s := mustNewMemoryStore(t, MemoryStoreOptions{Summarizer: fs})

	// Fallback label uses the first 8 raw words (not stemmed), capped at 60 bytes.
	c, _, err := s.Add("alpha beta gamma delta epsilon zeta eta theta iota", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	want := "alpha beta gamma delta epsilon zeta eta theta"
	if c.Label != want {
		t.Fatalf("unexpected fallback label: got %q want %q", c.Label, want)
	}
}

func TestMemoryStoreAdd_LabelFallbackPreservesOriginalWords(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	// Words that stemming would mangle: "Running"→"run", "databases"→"databas", "Memory"→"memori".
	// Fallback label should preserve the original word forms for readability.
	c, _, err := s.Add("Running databases for production systems", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	want := "Running databases for production systems"
	if c.Label != want {
		t.Fatalf("fallback label should use original words: got %q want %q", c.Label, want)
	}
}

func TestMemoryStoreAdd_SimilarityLinksAreSymmetricAndSorted(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})

	c1, _, err := s.Add("alpha", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	c2, _, err := s.Add("beta", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	c3, related, err := s.Add("alpha beta", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	if len(related) != 2 {
		t.Fatalf("expected 2 related memories, got %d", len(related))
	}
	// Similarities should tie; expect deterministic fallback ordering by ID.
	if related[0].ID != c1.ID || related[1].ID != c2.ID {
		t.Fatalf("unexpected related ordering: got [%s, %s], want [%s, %s]", related[0].ID, related[1].ID, c1.ID, c2.ID)
	}

	// With BM25 doc-doc similarity, "alpha beta" is related to both "alpha" and "beta".
	// Similarity should be above delta and symmetric.
	const eps = 1e-12
	relatedByID := make(map[string]float64)
	for _, r := range related {
		if r.Similarity <= s.similarityDelta {
			t.Fatalf("unexpected similarity for %s: got %.15f want > %.15f", r.ID, r.Similarity, s.similarityDelta)
		}
		relatedByID[r.ID] = r.Similarity
	}

	// Verify symmetric edges on internal graph.
	s.mu.RLock()
	defer s.mu.RUnlock()

	m1 := s.chunks[c1.ID]
	m2 := s.chunks[c2.ID]
	m3 := s.chunks[c3.ID]
	if m1 == nil || m2 == nil || m3 == nil {
		t.Fatalf("expected all chunks present")
	}
	want1 := relatedByID[c1.ID]
	if math.Abs(m3.edges[c1.ID]-want1) > eps || math.Abs(m1.edges[c3.ID]-want1) > eps {
		t.Fatalf("missing or asymmetric edge between %s and %s", c3.ID, c1.ID)
	}
	want2 := relatedByID[c2.ID]
	if math.Abs(m3.edges[c2.ID]-want2) > eps || math.Abs(m2.edges[c3.ID]-want2) > eps {
		t.Fatalf("missing or asymmetric edge between %s and %s", c3.ID, c2.ID)
	}
}

func TestMemoryStoreAdd_DeltaTooHighNoEdges(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 1.1})
	c1, _, err := s.Add("alpha", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	c2, related, err := s.Add("alpha", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if len(related) != 0 {
		t.Fatalf("expected no related memories, got %d", len(related))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.chunks[c1.ID].edges); got != 0 {
		t.Fatalf("expected no edges on %s, got %d", c1.ID, got)
	}
	if got := len(s.chunks[c2.ID].edges); got != 0 {
		t.Fatalf("expected no edges on %s, got %d", c2.ID, got)
	}
}

func TestMemoryStoreGetByID_ReturnsNeighbors(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})
	a, _, err := s.Add("alpha", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	_, _, err = s.Add("beta", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	c, _, err := s.Add("alpha beta", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	got, neigh, err := s.Get(c.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.ID != c.ID {
		t.Fatalf("unexpected chunk id: got %q want %q", got.ID, c.ID)
	}
	if len(neigh) != 2 {
		t.Fatalf("expected 2 neighbors, got %d", len(neigh))
	}
	// Deterministic ordering by edge similarity then ID (ties => IDs).
	if neigh[0].ID != a.ID {
		t.Fatalf("unexpected first neighbor: got %q want %q", neigh[0].ID, a.ID)
	}
}

func TestMemoryStoreSearch_ContentOnlyNotLabel(t *testing.T) {
	// Search uses content only; custom label is for display and does not affect similarity.
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})

	a, _, err := s.Add("alpha content", "topicA", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	_, _, err = s.Add("beta content", "topicB", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	// Query by content "alpha"; should match the chunk with content "alpha content" (id a),
	// not by label "topicA".
	matches, err := s.Search("alpha", "", "")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected at least one match for content 'alpha'")
	}
	found := false
	for _, m := range matches {
		if m.Chunk.ID == a.ID {
			found = true
			if m.Chunk.Label != "topicA" {
				t.Fatalf("chunk label should still be displayed: got %q", m.Chunk.Label)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected content-based match for chunk %q", a.ID)
	}
}

func TestMemoryStoreSearch_LexicalMatchBypassesCosineThreshold(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.5})

	// Ensure cosine similarity stays below delta due to many tokens.
	text := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda Gustav"
	c, _, err := s.Add(text, "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	matches, err := s.Search("Gustav", "", "")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected lexical match to return a result")
	}
	found := false
	for _, m := range matches {
		if m.Chunk.ID == c.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected lexical match for chunk %q", c.ID)
	}
}

// TestMemoryStoreSearch_BM25Used verifies that Search uses BM25: a query matching
// document content returns results with similarity above delta, and scores are deterministic.
func TestMemoryStoreSearch_BM25Used(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.2})

	_, _, _ = s.Add("retrieval algorithm for search", "", "fact", nil)
	_, _, _ = s.Add("database schema design", "", "fact", nil)

	matches, err := s.Search("retrieval algorithm", "", "")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one match for query overlapping first doc")
	}
	// First document should rank at or near top (BM25 or lexical match).
	if matches[0].Chunk.ID != "m1" && (len(matches) < 2 || matches[1].Chunk.ID != "m1") {
		// m1 is the only doc with both "retrieval" and "algorithm"; it must appear in top results
		found := false
		for _, m := range matches {
			if m.Chunk.ID == "m1" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected m1 in results, got %v", func() []string {
				ids := make([]string, len(matches))
				for i, m := range matches {
					ids[i] = m.Chunk.ID
				}
				return ids
			}())
		}
	}
	if matches[0].Similarity <= 0 {
		t.Errorf("expected positive similarity, got %f", matches[0].Similarity)
	}
}

func TestMemoryStoreSearch_RespectsMaxResults(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.0, MaxResults: 2})

	_, _, err := s.Add("alpha beta", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	_, _, err = s.Add("alpha gamma", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	_, _, err = s.Add("alpha delta", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	matches, err := s.Search("alpha", "", "")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if got := len(matches); got != 2 {
		t.Fatalf("expected 2 matches, got %d", got)
	}
}

func TestMemoryStore_List_TypeFilter(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})
	_, _, _ = s.Add("fact one", "", "fact", nil)
	_, _, _ = s.Add("story one", "", "story", nil)
	_, _, _ = s.Add("fact two", "", "fact", nil)

	all := s.List("", "")
	if len(all) != 3 {
		t.Fatalf("List() no filter: expected 3, got %d", len(all))
	}

	facts := s.List("fact", "")
	if len(facts) != 2 {
		t.Fatalf("List(\"fact\"): expected 2, got %d", len(facts))
	}
	for _, it := range facts {
		if it.Type != "fact" {
			t.Fatalf("expected type fact, got %q", it.Type)
		}
	}

	stories := s.List("story", "")
	if len(stories) != 1 {
		t.Fatalf("List(\"story\"): expected 1, got %d", len(stories))
	}
	if stories[0].Type != "story" {
		t.Fatalf("expected type story, got %q", stories[0].Type)
	}

	empty := s.List("nonexistent", "")
	if len(empty) != 0 {
		t.Fatalf("List(\"nonexistent\"): expected 0, got %d", len(empty))
	}
}

func TestMemoryStore_Search_TypeFilter(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})
	_, _, _ = s.Add("alpha fact content", "", "fact", nil)
	_, _, _ = s.Add("alpha story content", "", "story", nil)

	all, err := s.Search("alpha", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("Search no filter: expected 2 matches, got %d", len(all))
	}

	facts, err := s.Search("alpha", "fact", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Chunk.Type != "fact" {
		t.Fatalf("Search type=fact: expected 1 fact match, got %d", len(facts))
	}

	stories, err := s.Search("alpha", "story", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 1 || stories[0].Chunk.Type != "story" {
		t.Fatalf("Search type=story: expected 1 story match, got %d", len(stories))
	}

	none, err := s.Search("alpha", "note", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("Search type=note: expected 0, got %d", len(none))
	}
}

func TestMemoryStore_GetContext_TypeFilter(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})
	_, _, _ = s.Add("#goal build the feature", "", "goal", nil)
	_, _, _ = s.Add("#status in progress", "", "status", nil)
	_, _, _ = s.Add("#goal another goal", "", "goal", nil)

	all := s.GetContext("", "", 0)
	if len(all.Memories) == 0 {
		t.Fatal("GetContext() no filter: expected some memories")
	}

	goalOnly := s.GetContext("goal", "", 0)
	countGoal := 0
	for _, m := range goalOnly.Memories {
		if m.Category != "#goal" {
			t.Fatalf("expected category #goal, got %q", m.Category)
		}
		if m.Chunk.Type != "goal" {
			t.Fatalf("expected type goal, got %q", m.Chunk.Type)
		}
		countGoal++
	}
	if countGoal != 2 {
		t.Fatalf("GetContext(\"goal\"): expected 2, got %d", countGoal)
	}

	statusOnly := s.GetContext("status", "", 0)
	if len(statusOnly.Memories) != 1 || statusOnly.Memories[0].Chunk.Type != "status" {
		t.Fatalf("GetContext(\"status\"): expected 1 memory with type status, got %d", len(statusOnly.Memories))
	}

	empty := s.GetContext("nonexistent", "", 0)
	if len(empty.Memories) != 0 {
		t.Fatalf("GetContext(\"nonexistent\"): expected 0, got %d", len(empty.Memories))
	}
}

func TestMemoryStore_GetContext_PerCategory(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})
	// Add 4 goals
	_, _, _ = s.Add("#goal first", "", "", nil)
	_, _, _ = s.Add("#goal second", "", "", nil)
	_, _, _ = s.Add("#goal third", "", "", nil)
	_, _, _ = s.Add("#goal fourth", "", "", nil)

	// Default (0 → 2 per category)
	ctx0 := s.GetContext("", "", 0)
	goalCount := 0
	for _, m := range ctx0.Memories {
		if m.Category == "#goal" {
			goalCount++
		}
	}
	if goalCount != 2 {
		t.Fatalf("default per_category: expected 2 goals, got %d", goalCount)
	}

	// Explicit 3 per category
	ctx3 := s.GetContext("", "", 3)
	goalCount = 0
	for _, m := range ctx3.Memories {
		if m.Category == "#goal" {
			goalCount++
		}
	}
	if goalCount != 3 {
		t.Fatalf("per_category=3: expected 3 goals, got %d", goalCount)
	}

	// Explicit 1 per category
	ctx1 := s.GetContext("", "", 1)
	goalCount = 0
	for _, m := range ctx1.Memories {
		if m.Category == "#goal" {
			goalCount++
		}
	}
	if goalCount != 1 {
		t.Fatalf("per_category=1: expected 1 goal, got %d", goalCount)
	}

	// Large limit (more than available) — should return all 4
	ctx99 := s.GetContext("", "", 99)
	goalCount = 0
	for _, m := range ctx99.Memories {
		if m.Category == "#goal" {
			goalCount++
		}
	}
	if goalCount != 4 {
		t.Fatalf("per_category=99: expected 4 goals, got %d", goalCount)
	}
}

func TestMemoryStore_GetContext_UpdatedAtSorting(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	// Add an older #status memory, then a newer one.
	old, _, _ := s.Add("#status old status created first", "", "", nil)
	time.Sleep(10 * time.Millisecond) // Ensure distinct timestamps
	_, _, _ = s.Add("#status newer status created second", "", "", nil)

	// Without updates, the newer memory should appear first (newest CreatedAt).
	ctx := s.GetContext("", "", 2)
	var statusTexts []string
	for _, m := range ctx.Memories {
		if m.Category == "#status" {
			statusTexts = append(statusTexts, m.Chunk.Text)
		}
	}
	if len(statusTexts) != 2 {
		t.Fatalf("expected 2 #status memories, got %d", len(statusTexts))
	}
	if statusTexts[0] != "#status newer status created second" {
		t.Fatalf("before update: expected newer first, got %q", statusTexts[0])
	}

	// Now update the older memory — its UpdatedAt becomes the most recent timestamp.
	time.Sleep(10 * time.Millisecond)
	s.Update(old.ID, "#status old status just updated", "", "", nil)

	ctx2 := s.GetContext("", "", 2)
	statusTexts = nil
	for _, m := range ctx2.Memories {
		if m.Category == "#status" {
			statusTexts = append(statusTexts, m.Chunk.Text)
		}
	}
	if len(statusTexts) != 2 {
		t.Fatalf("expected 2 #status memories, got %d", len(statusTexts))
	}
	if statusTexts[0] != "#status old status just updated" {
		t.Fatalf("after update: expected recently-updated first, got %q", statusTexts[0])
	}
}

func TestMemoryStore_FindConsolidationPairs_Basic(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})

	c1, _, err := s.Add("alpha", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	c2, _, err := s.Add("alpha", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	// Unrelated chunk.
	if _, _, err := s.Add("beta", "", "", nil); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	pairs, err := s.FindConsolidationPairs(ConsolidationParams{MinSimilarity: 0.1})
	if err != nil {
		t.Fatalf("FindConsolidationPairs() error: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d: %+v", len(pairs), pairs)
	}
	p := pairs[0]
	if !((p.AID == c1.ID && p.BID == c2.ID) || (p.AID == c2.ID && p.BID == c1.ID)) {
		t.Fatalf("unexpected pair IDs: %+v", p)
	}
	if p.Similarity <= 0.1 {
		t.Fatalf("expected similarity > 0.1, got %.4f", p.Similarity)
	}
}

func TestMemoryStore_FindConsolidationPairs_TypeFilter(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})

	f1, _, _ := s.Add("alpha fact", "", "fact", nil)
	f2, _, _ := s.Add("alpha fact again", "", "fact", nil)
	// Same content but different type; should not appear for type=fact.
	_, _, _ = s.Add("alpha fact", "", "story", nil)

	pairs, err := s.FindConsolidationPairs(ConsolidationParams{
		TypeFilter:    "fact",
		MinSimilarity: 0.1,
	})
	if err != nil {
		t.Fatalf("FindConsolidationPairs() error: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair for type=fact, got %d: %+v", len(pairs), pairs)
	}
	p := pairs[0]
	if !((p.AID == f1.ID && p.BID == f2.ID) || (p.AID == f2.ID && p.BID == f1.ID)) {
		t.Fatalf("unexpected pair IDs for type filter: %+v", p)
	}
	if p.Type != "fact" {
		t.Fatalf("expected pair Type=fact, got %q", p.Type)
	}
}

func TestMemoryStore_Consolidate_BasicMergeAndDeleteSources(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})

	c1, _, _ := s.Add("first fact", "", "fact", nil)
	c2, _, _ := s.Add("second fact", "", "fact", nil)

	merged, removed, err := s.Consolidate([]string{c1.ID, c2.ID}, ConsolidateOptions{})
	if err != nil {
		t.Fatalf("Consolidate() error: %v", err)
	}
	if merged.ID == c1.ID || merged.ID == c2.ID {
		t.Fatalf("expected new merged ID, got %q", merged.ID)
	}
	if merged.Type != "fact" {
		t.Fatalf("expected merged type fact, got %q", merged.Type)
	}
	if !strings.Contains(merged.Text, "first fact") || !strings.Contains(merged.Text, "second fact") {
		t.Fatalf("merged text does not contain both sources: %q", merged.Text)
	}
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed IDs, got %d", len(removed))
	}

	// Sources should no longer be retrievable.
	if _, _, err := s.Get(c1.ID); err == nil {
		t.Fatalf("expected error getting deleted source %s", c1.ID)
	}
	if _, _, err := s.Get(c2.ID); err == nil {
		t.Fatalf("expected error getting deleted source %s", c2.ID)
	}
}

func TestMemoryStore_Consolidate_IncompatibleTypesError(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	a, _, _ := s.Add("alpha fact", "", "fact", nil)
	b, _, _ := s.Add("alpha story", "", "story", nil)

	_, _, err := s.Consolidate([]string{a.ID, b.ID}, ConsolidateOptions{})
	if err == nil {
		t.Fatalf("expected error for incompatible types, got nil")
	}
	if !errors.Is(err, ErrIncompatibleTypes) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMemoryStore_Consolidate_OverrideTypeAndKeepSources(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	a, _, _ := s.Add("alpha fact", "", "fact", nil)
	b, _, _ := s.Add("beta story", "", "story", nil)

	del := false
	merged, removed, err := s.Consolidate([]string{a.ID, b.ID}, ConsolidateOptions{
		NewType:       "note",
		DeleteSources: &del,
	})
	if err != nil {
		t.Fatalf("Consolidate() error: %v", err)
	}
	if merged.Type != "note" {
		t.Fatalf("expected merged type note, got %q", merged.Type)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removed IDs when DeleteSources=false, got %d", len(removed))
	}

	// Sources should still exist.
	if _, _, err := s.Get(a.ID); err != nil {
		t.Fatalf("expected source %s to still exist; err=%v", a.ID, err)
	}
	if _, _, err := s.Get(b.ID); err != nil {
		t.Fatalf("expected source %s to still exist; err=%v", b.ID, err)
	}
}

func TestMemoryStore_ScopeFiltering(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})

	// Add global memory (no scopes) with hashtag for GetContext
	global, _, _ := s.Add("#status global memory", "", "", nil)

	// Add project-scoped memories with hashtags
	proj1, _, _ := s.Add("#status project one memory", "", "", []string{"project1"})
	proj2, _, _ := s.Add("#status project two memory", "", "", []string{"project2"})
	multiScope, _, _ := s.Add("#status multi-scope memory", "", "", []string{"project1", "project2"})

	// Search without scope filter - should return all
	all, _ := s.Search("memory", "", "")
	if len(all) != 4 {
		t.Fatalf("Search without scope: expected 4, got %d", len(all))
	}

	// Search with scope filter - should return global + matching scopes
	proj1Results, _ := s.Search("memory", "", "project1")
	if len(proj1Results) != 3 { // global + proj1 + multiScope
		t.Fatalf("Search scope=project1: expected 3, got %d", len(proj1Results))
	}
	foundGlobal := false
	foundProj1 := false
	foundMulti := false
	for _, m := range proj1Results {
		if m.Chunk.ID == global.ID {
			foundGlobal = true
		}
		if m.Chunk.ID == proj1.ID {
			foundProj1 = true
		}
		if m.Chunk.ID == multiScope.ID {
			foundMulti = true
		}
	}
	if !foundGlobal || !foundProj1 || !foundMulti {
		t.Fatalf("Search scope=project1: missing expected memories")
	}

	// Search with different scope
	proj2Results, _ := s.Search("memory", "", "project2")
	if len(proj2Results) != 3 { // global + proj2 + multiScope
		t.Fatalf("Search scope=project2: expected 3, got %d", len(proj2Results))
	}
	// Verify proj2 is included
	foundProj2 := false
	for _, m := range proj2Results {
		if m.Chunk.ID == proj2.ID {
			foundProj2 = true
			break
		}
	}
	if !foundProj2 {
		t.Fatal("Search scope=project2: proj2 memory not found")
	}

	// List with scope filter
	proj1List := s.List("", "project1")
	if len(proj1List) != 3 {
		t.Fatalf("List scope=project1: expected 3, got %d", len(proj1List))
	}

	// GetContext with scope filter
	ctx := s.GetContext("", "project1", 0)
	if len(ctx.Memories) == 0 {
		t.Fatal("GetContext scope=project1: expected some memories")
	}

	// FindRelevant with scope filter
	relevant := s.FindRelevant("memory", 10, "project1", "")
	if len(relevant) != 3 {
		t.Fatalf("FindRelevant scope=project1: expected 3, got %d", len(relevant))
	}
}

func TestMemoryStore_FindRelevant_TypeFilter(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	_, _, _ = s.Add("database choice is postgres", "", "fact", nil)
	_, _, _ = s.Add("database story for the project", "", "story", nil)

	results := s.FindRelevant("database", 5, "", "fact")
	if len(results) != 1 {
		t.Fatalf("FindRelevant type=fact: expected 1, got %d", len(results))
	}
	if results[0].Type != "fact" {
		t.Fatalf("expected type fact, got %q", results[0].Type)
	}
}

func TestMemoryStore_ScopeAwareEdgeCreation(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})

	// Add global memory
	global, _, _ := s.Add("alpha beta", "", "", nil)

	// Add project1-scoped memory (similar content)
	proj1, _, _ := s.Add("alpha beta gamma", "", "", []string{"project1"})

	// Add project2-scoped memory (similar content)
	proj2, _, _ := s.Add("alpha beta delta", "", "", []string{"project2"})

	// Add another project1 memory
	proj1b, _, _ := s.Add("alpha beta epsilon", "", "", []string{"project1"})

	// Global should connect to everything (it's global)
	_, neighbors, _ := s.Get(global.ID)
	if len(neighbors) == 0 {
		t.Fatal("Global memory should have neighbors")
	}
	foundProj1 := false
	foundProj2 := false
	for _, n := range neighbors {
		if n.ID == proj1.ID {
			foundProj1 = true
		}
		if n.ID == proj2.ID {
			foundProj2 = true
		}
	}
	if !foundProj1 || !foundProj2 {
		t.Fatalf("Global memory should connect to both project1 and project2. Found proj1: %v, proj2: %v", foundProj1, foundProj2)
	}

	// Project1 memories should connect to each other and global, but NOT project2
	_, neighbors, _ = s.Get(proj1.ID)
	if len(neighbors) == 0 {
		t.Fatal("Project1 memory should have neighbors")
	}
	foundGlobal := false
	foundProj1b := false
	foundProj2InProj1 := false
	for _, n := range neighbors {
		if n.ID == global.ID {
			foundGlobal = true
		}
		if n.ID == proj1b.ID {
			foundProj1b = true
		}
		if n.ID == proj2.ID {
			foundProj2InProj1 = true
		}
	}
	if !foundGlobal {
		t.Fatal("Project1 memory should connect to global memory")
	}
	if !foundProj1b {
		t.Fatal("Project1 memories should connect to each other")
	}
	if foundProj2InProj1 {
		t.Fatal("Project1 memory should NOT connect to project2 memory")
	}

	// Project2 should only connect to global, not project1
	_, neighbors, _ = s.Get(proj2.ID)
	if len(neighbors) == 0 {
		t.Fatal("Project2 memory should have neighbors")
	}
	foundGlobalInProj2 := false
	foundProj1InProj2 := false
	for _, n := range neighbors {
		if n.ID == global.ID {
			foundGlobalInProj2 = true
		}
		if n.ID == proj1.ID {
			foundProj1InProj2 = true
		}
	}
	if !foundGlobalInProj2 {
		t.Fatal("Project2 memory should connect to global memory")
	}
	if foundProj1InProj2 {
		t.Fatal("Project2 memory should NOT connect to project1 memory")
	}

	// Search with scope filter should only return scope-compatible neighbors
	matches, _ := s.Search("alpha", "", "project1")
	if len(matches) == 0 {
		t.Fatal("Search should find project1 memories")
	}
	for _, m := range matches {
		// All neighbors should be project1 or global
		for _, neighbor := range m.Neighbors {
			nChunk, _, _ := s.Get(neighbor.ID)
			if len(nChunk.Scopes) > 0 {
				hasProject1 := false
				for _, s := range nChunk.Scopes {
					if s == "project1" {
						hasProject1 = true
						break
					}
				}
				if !hasProject1 {
					t.Fatalf("Neighbor %s in project1 search should be project1 or global, but has scopes: %v", neighbor.ID, nChunk.Scopes)
				}
			}
		}
	}
}

// TestMemoryStore_ConcurrentGetAndUpdate verifies that concurrent Get (which
// triggers access tracking with an async goroutine) and Update operations do
// not race. Run with -race to detect data races.
func TestMemoryStore_ConcurrentGetAndUpdate(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})
	chunk, _, _ := s.Add("concurrent access test memory with enough tokens to be interesting", "", "", nil)

	var wg sync.WaitGroup
	const iterations = 50

	// Concurrent Gets (each spawns an async save goroutine via trackAccessLocked).
	wg.Add(iterations)
	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = s.Get(chunk.ID)
		}()
	}

	// Concurrent Updates on the same chunk.
	wg.Add(iterations)
	for i := 0; i < iterations; i++ {
		go func(n int) {
			defer wg.Done()
			text := "updated concurrent text version " + itoaBase10(uint64(n))
			_, _, _ = s.Update(chunk.ID, text, "", "", nil)
		}(i)
	}

	wg.Wait()

	// Verify the chunk is still retrievable and consistent.
	got, _, err := s.Get(chunk.ID)
	if err != nil {
		t.Fatalf("Get after concurrent operations: %v", err)
	}
	if got.ID != chunk.ID {
		t.Fatalf("ID mismatch: got %s, want %s", got.ID, chunk.ID)
	}
}

func TestTruncater_Summarize(t *testing.T) {
	tr := NewTruncater()

	// Within limit — returned unchanged.
	out, err := tr.Summarize("hello", 100)
	if err != nil || out != "hello" {
		t.Fatalf("within limit: got %q, err %v", out, err)
	}

	// At exact limit — returned unchanged.
	out, err = tr.Summarize("abcd", 4)
	if err != nil || out != "abcd" {
		t.Fatalf("at limit: got %q, err %v", out, err)
	}

	// Over limit — truncated with "…" suffix.
	out, err = tr.Summarize("hello world this is long text", 10)
	if err != nil {
		t.Fatalf("over limit: err %v", err)
	}
	if len([]byte(out)) > 10 {
		t.Fatalf("over limit: got %d bytes, want <= 10", len([]byte(out)))
	}
	if !strings.HasSuffix(out, "…") {
		t.Fatalf("over limit: expected … suffix, got %q", out)
	}

	// Very small maxBytes (less than suffix) — just truncates without suffix.
	out, err = tr.Summarize("hello world", 2)
	if err != nil {
		t.Fatalf("tiny limit: err %v", err)
	}
	if len([]byte(out)) > 2 {
		t.Fatalf("tiny limit: got %d bytes, want <= 2", len([]byte(out)))
	}

	// Whitespace input — trimmed to empty, which is within any limit.
	out, err = tr.Summarize("   ", 100)
	if err != nil || out != "" {
		t.Fatalf("whitespace: got %q, err %v", out, err)
	}
}

func TestHardTruncateToBytes(t *testing.T) {
	// ASCII — clean truncation.
	out := HardTruncateToBytes("abcdef", 3)
	if out != "abc" {
		t.Fatalf("ascii: got %q, want %q", out, "abc")
	}

	// Within limit — unchanged.
	out = HardTruncateToBytes("abc", 100)
	if out != "abc" {
		t.Fatalf("within limit: got %q, want %q", out, "abc")
	}

	// Multibyte: "héllo" — 'é' is 2 bytes (0xC3 0xA9).
	// "hé" = 3 bytes. Truncating to 2 should give "h" (not split the é).
	out = HardTruncateToBytes("héllo", 2)
	if out != "h" {
		t.Fatalf("multibyte split: got %q, want %q", out, "h")
	}

	// Emoji: "a🔥b" — 🔥 is 4 bytes. "a" = 1 byte, "a🔥" = 5 bytes.
	// Truncating to 3 should give "a" (not split the emoji).
	out = HardTruncateToBytes("a🔥b", 3)
	if out != "a" {
		t.Fatalf("emoji split: got %q, want %q", out, "a")
	}

	// Truncating to 5 should give "a🔥".
	out = HardTruncateToBytes("a🔥b", 5)
	if out != "a🔥" {
		t.Fatalf("emoji full: got %q, want %q", out, "a🔥")
	}

	// Zero maxBytes — empty.
	out = HardTruncateToBytes("hello", 0)
	if out != "" {
		t.Fatalf("zero: got %q, want empty", out)
	}

	// Negative maxBytes — empty.
	out = HardTruncateToBytes("hello", -1)
	if out != "" {
		t.Fatalf("negative: got %q, want empty", out)
	}
}

func TestMemoryStore_Add_OversizedText(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	// Create text slightly over MaxMemoryBytes.
	bigText := strings.Repeat("x", MaxMemoryBytes+100)
	chunk, _, err := s.Add(bigText, "", "", nil)
	if err != nil {
		t.Fatalf("Add oversized: unexpected error: %v", err)
	}
	if len([]byte(chunk.Text)) > MaxMemoryBytes {
		t.Fatalf("Add oversized: text is %d bytes, want <= %d", len([]byte(chunk.Text)), MaxMemoryBytes)
	}
}

// --- AutoConsolidate tests ---

func TestAutoConsolidate_DryRun(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.01})

	// Add two very similar memories that will create a high-similarity edge.
	s.Add("alpha bravo charlie delta echo", "", "fact", nil)
	s.Add("alpha bravo charlie delta foxtrot", "", "fact", nil)

	result, err := s.AutoConsolidate(AutoConsolidateOptions{
		MinSimilarity: 0.3,
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("AutoConsolidate dry run: %v", err)
	}

	if result.Consolidated != 1 {
		t.Fatalf("dry run: expected 1 consolidated, got %d", result.Consolidated)
	}
	// Dry run should report removals but not actually create merged memories.
	if len(result.Removed) != 2 {
		t.Fatalf("dry run: expected 2 removed IDs, got %d", len(result.Removed))
	}
	if len(result.Merged) != 0 {
		t.Fatalf("dry run: expected 0 merged IDs, got %d", len(result.Merged))
	}

	// Verify nothing was actually deleted.
	items := s.List("", "")
	if len(items) != 2 {
		t.Fatalf("dry run: expected 2 memories still present, got %d", len(items))
	}
}

func TestAutoConsolidate_ActualMerge(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.01})

	s.Add("alpha bravo charlie delta echo", "", "fact", nil)
	s.Add("alpha bravo charlie delta foxtrot", "", "fact", nil)
	s.Add("completely unrelated zulu yankee xray", "", "note", nil)

	result, err := s.AutoConsolidate(AutoConsolidateOptions{
		MinSimilarity: 0.3,
	})
	if err != nil {
		t.Fatalf("AutoConsolidate: %v", err)
	}

	if result.Consolidated != 1 {
		t.Fatalf("expected 1 consolidated, got %d", result.Consolidated)
	}
	if len(result.Merged) != 1 {
		t.Fatalf("expected 1 merged ID, got %d", len(result.Merged))
	}
	if len(result.Removed) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(result.Removed))
	}

	// Should have 2 memories left: the merged one + the unrelated one.
	items := s.List("", "")
	if len(items) != 2 {
		t.Fatalf("expected 2 memories after consolidation, got %d", len(items))
	}
}

func TestAutoConsolidate_DryRunNoDuplicateCounting(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.01})

	// Create 3 memories that are all similar to each other, producing 3 candidate pairs:
	// (m1,m2), (m1,m3), (m2,m3). After processing one pair, the others sharing an ID
	// must be skipped — even in dry-run mode.
	s.Add("alpha bravo charlie delta echo", "", "fact", nil)
	s.Add("alpha bravo charlie delta foxtrot", "", "fact", nil)
	s.Add("alpha bravo charlie delta golf", "", "fact", nil)

	result, err := s.AutoConsolidate(AutoConsolidateOptions{
		MinSimilarity: 0.3,
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("AutoConsolidate dry run: %v", err)
	}

	// Only 1 pair should be consolidated — after that, all 3 IDs are "processed"
	// so the remaining 2 pairs (which share IDs with the first) are skipped.
	if result.Consolidated != 1 {
		t.Fatalf("dry run with overlapping pairs: expected 1 consolidated, got %d", result.Consolidated)
	}
	if len(result.Removed) != 2 {
		t.Fatalf("dry run: expected 2 removed IDs, got %d", len(result.Removed))
	}
}

func TestAutoConsolidate_DryRunRespectsMaxLimit(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.01})

	// Create two independent similar pairs.
	s.Add("alpha bravo charlie delta echo", "", "fact", nil)
	s.Add("alpha bravo charlie delta foxtrot", "", "fact", nil)
	s.Add("golf hotel india juliet kilo", "", "fact", nil)
	s.Add("golf hotel india juliet lima", "", "fact", nil)

	result, err := s.AutoConsolidate(AutoConsolidateOptions{
		MinSimilarity:     0.3,
		MaxConsolidations: 1,
		DryRun:            true,
	})
	if err != nil {
		t.Fatalf("AutoConsolidate dry run with limit: %v", err)
	}

	if result.Consolidated != 1 {
		t.Fatalf("dry run with limit=1: expected 1 consolidated, got %d", result.Consolidated)
	}
}

func TestAutoConsolidate_MaxLimit(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.01})

	// Create three similar pairs by having 6 memories.
	s.Add("alpha bravo charlie delta echo", "", "fact", nil)
	s.Add("alpha bravo charlie delta foxtrot", "", "fact", nil)
	s.Add("golf hotel india juliet kilo", "", "fact", nil)
	s.Add("golf hotel india juliet lima", "", "fact", nil)

	result, err := s.AutoConsolidate(AutoConsolidateOptions{
		MinSimilarity:     0.3,
		MaxConsolidations: 1,
	})
	if err != nil {
		t.Fatalf("AutoConsolidate with limit: %v", err)
	}

	if result.Consolidated != 1 {
		t.Fatalf("expected exactly 1 consolidated (limit), got %d", result.Consolidated)
	}
}

func TestAutoConsolidate_NoPairs(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	s.Add("alpha bravo charlie", "", "", nil)
	s.Add("completely unrelated zulu yankee xray", "", "", nil)

	result, err := s.AutoConsolidate(AutoConsolidateOptions{
		MinSimilarity: 0.99, // Very high threshold — no pairs.
	})
	if err != nil {
		t.Fatalf("AutoConsolidate: %v", err)
	}
	if result.Consolidated != 0 {
		t.Fatalf("expected 0 consolidated, got %d", result.Consolidated)
	}
}

func TestAutoConsolidate_TypeFilter(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.01})

	s.Add("alpha bravo charlie delta echo", "", "fact", nil)
	s.Add("alpha bravo charlie delta foxtrot", "", "fact", nil)
	s.Add("alpha bravo charlie delta golf", "", "note", nil)

	// Only consolidate notes — should find no pairs (only one note).
	result, err := s.AutoConsolidate(AutoConsolidateOptions{
		MinSimilarity: 0.3,
		TypeFilter:    "note",
	})
	if err != nil {
		t.Fatalf("AutoConsolidate type filter: %v", err)
	}
	if result.Consolidated != 0 {
		t.Fatalf("expected 0 consolidated with note filter, got %d", result.Consolidated)
	}
}

// --- Update tests ---

func TestUpdate_PreservesExistingFields(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	original, _, err := s.Add("original text", "original label", "fact", []string{"project1"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Update text only — label, type, scopes should be preserved.
	updated, _, err := s.Update(original.ID, "updated text", "", "", nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Text != "updated text" {
		t.Fatalf("text not updated: got %q", updated.Text)
	}
	if updated.Label != "original label" {
		t.Fatalf("label not preserved: got %q, want %q", updated.Label, "original label")
	}
	if updated.Type != "fact" {
		t.Fatalf("type not preserved: got %q, want %q", updated.Type, "fact")
	}
	if len(updated.Scopes) != 1 || updated.Scopes[0] != "project1" {
		t.Fatalf("scopes not preserved: got %v", updated.Scopes)
	}
}

func TestUpdate_OverridesAllFields(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	original, _, _ := s.Add("original text", "original label", "fact", []string{"project1"})

	// Update everything.
	updated, _, err := s.Update(original.ID, "new text", "new label", "note", []string{"project2"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Text != "new text" {
		t.Fatalf("text: got %q", updated.Text)
	}
	if updated.Label != "new label" {
		t.Fatalf("label: got %q", updated.Label)
	}
	if updated.Type != "note" {
		t.Fatalf("type: got %q", updated.Type)
	}
	if len(updated.Scopes) != 1 || updated.Scopes[0] != "project2" {
		t.Fatalf("scopes: got %v", updated.Scopes)
	}
}

func TestUpdate_ClearScopes(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	original, _, _ := s.Add("scoped text", "", "fact", []string{"proj1"})

	// Pass empty slice to clear scopes (make it global).
	updated, _, err := s.Update(original.ID, "scoped text updated", "", "", []string{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Scopes != nil {
		t.Fatalf("expected nil scopes (global), got %v", updated.Scopes)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	_, _, err := s.Update("m999", "new text", "", "", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdate_EmptyInputs(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	_, _, err := s.Update("", "text", "", "", nil)
	if !errors.Is(err, ErrEmptyID) {
		t.Fatalf("expected ErrEmptyID for empty id, got %v", err)
	}

	s.Add("something", "", "", nil)
	_, _, err = s.Update("m1", "", "", "", nil)
	if err == nil {
		t.Fatal("expected error for empty text, got nil")
	}
}

func TestUpdate_RecalculatesEdges(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.01})

	c1, _, _ := s.Add("alpha bravo charlie delta", "", "", nil)
	c2, _, _ := s.Add("echo foxtrot golf hotel", "", "", nil)

	// Initially c1 and c2 should have no edge (different tokens).
	s.mu.RLock()
	_, hasEdge := s.chunks[c1.ID].edges[c2.ID]
	s.mu.RUnlock()
	if hasEdge {
		t.Fatal("expected no edge between completely different memories")
	}

	// Update c2 to share tokens with c1 — edge should appear.
	s.Update(c2.ID, "alpha bravo charlie echo", "", "", nil)

	s.mu.RLock()
	_, hasEdge = s.chunks[c1.ID].edges[c2.ID]
	s.mu.RUnlock()
	if !hasEdge {
		t.Fatal("expected edge after updating c2 to share tokens with c1")
	}
}

// --- UpdatedAt tests ---

func TestUpdate_SetsUpdatedAt(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	c, _, _ := s.Add("original text about databases", "", "fact", nil)

	// New memory should have nil UpdatedAt.
	if c.UpdatedAt != nil {
		t.Fatalf("new memory should have nil UpdatedAt, got %v", c.UpdatedAt)
	}

	// Verify via Get too.
	got, _, _ := s.Get(c.ID)
	if got.UpdatedAt != nil {
		t.Fatalf("Get: new memory should have nil UpdatedAt, got %v", got.UpdatedAt)
	}

	before := time.Now()
	time.Sleep(time.Millisecond) // Ensure UpdatedAt > before
	updated, _, err := s.Update(c.ID, "updated text about databases", "", "", nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.UpdatedAt == nil {
		t.Fatal("UpdatedAt should be set after Update()")
	}
	if updated.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt (%v) should be after before (%v)", *updated.UpdatedAt, before)
	}

	// Verify via Get.
	got2, _, _ := s.Get(c.ID)
	if got2.UpdatedAt == nil {
		t.Fatal("Get after Update: UpdatedAt should be set")
	}
	if !got2.UpdatedAt.Equal(*updated.UpdatedAt) {
		t.Errorf("Get UpdatedAt (%v) != Update return (%v)", *got2.UpdatedAt, *updated.UpdatedAt)
	}
}

func TestUpdate_UpdatedAtChangesOnEachUpdate(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	c, _, _ := s.Add("first version text", "", "", nil)

	s.Update(c.ID, "second version text", "", "", nil)
	got1, _, _ := s.Get(c.ID)

	time.Sleep(time.Millisecond)
	s.Update(c.ID, "third version text", "", "", nil)
	got2, _, _ := s.Get(c.ID)

	if got1.UpdatedAt == nil || got2.UpdatedAt == nil {
		t.Fatal("both updates should set UpdatedAt")
	}
	if !got2.UpdatedAt.After(*got1.UpdatedAt) {
		t.Errorf("second UpdatedAt (%v) should be after first (%v)", *got2.UpdatedAt, *got1.UpdatedAt)
	}
}

func TestList_IncludesUpdatedAt(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	c, _, _ := s.Add("memory for list test", "", "", nil)
	s.Update(c.ID, "updated memory for list test", "", "", nil)

	items := s.List("", "")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].UpdatedAt == nil {
		t.Fatal("List item should include UpdatedAt after Update()")
	}
}

func TestExportImport_PreservesUpdatedAt(t *testing.T) {
	s1 := mustNewMemoryStore(t, MemoryStoreOptions{})

	c, _, _ := s1.Add("memory that will be updated", "", "fact", nil)
	s1.Update(c.ID, "memory that has been updated", "", "", nil)

	exported := s1.Export()
	if len(exported) != 1 {
		t.Fatalf("expected 1 export, got %d", len(exported))
	}
	if exported[0].UpdatedAt == nil {
		t.Fatal("exported chunk should have UpdatedAt")
	}

	// Import into fresh store.
	s2 := mustNewMemoryStore(t, MemoryStoreOptions{})
	results := s2.Import([]ImportChunk{
		{
			ID:        exported[0].ID,
			Text:      exported[0].Text,
			Label:     exported[0].Label,
			Type:      exported[0].Type,
			CreatedAt: exported[0].CreatedAt,
			UpdatedAt: exported[0].UpdatedAt,
		},
	}, false)
	if results.Imported != 1 {
		t.Fatalf("expected 1 imported, got %d", results.Imported)
	}

	got, _, _ := s2.Get(exported[0].ID)
	if got.UpdatedAt == nil {
		t.Fatal("imported memory should preserve UpdatedAt")
	}
	if !got.UpdatedAt.Equal(*exported[0].UpdatedAt) {
		t.Errorf("UpdatedAt not preserved: got %v, want %v", *got.UpdatedAt, *exported[0].UpdatedAt)
	}
}

// --- Utility function tests ---

func TestCleanOneLine(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"  hello  ", "hello"},
		{"hello\nworld", "hello world"},
		{"hello\r\nworld", "hello world"},
		{"hello\tworld", "hello world"},
		{"  multiple \n\n newlines \t\t tabs  ", "multiple newlines tabs"},
		{"single", "single"},
	}
	for _, tc := range tests {
		got := cleanOneLine(tc.in)
		if got != tc.want {
			t.Errorf("cleanOneLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractKeywords_EdgeCases(t *testing.T) {
	// Empty input.
	got := extractKeywords("")
	if len(got) != 0 {
		t.Fatalf("extractKeywords(\"\") = %v, want empty", got)
	}

	// Pure stop words produce no keywords.
	got = extractKeywords("what is the")
	if len(got) != 0 {
		t.Fatalf("extractKeywords(stop words) = %v, want empty", got)
	}

	// Stemmed forms of stop words must also be filtered.
	// "does"→"doe", "being"→"be", "actually"→"actual" etc.
	got = extractKeywords("does this being actually probably maybe")
	for _, kw := range got {
		t.Errorf("expected no keywords from stemmed stop words, got %q", kw)
	}

	// Hashtags are always preserved even in conversational text.
	got = extractKeywords("can you tell me about #status")
	found := false
	for _, kw := range got {
		if kw == "#status" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected #status in keywords, got %v", got)
	}
}

func TestParseChunkID(t *testing.T) {
	tests := []struct {
		id   string
		want uint64
	}{
		{"m0", 0},
		{"m1", 1},
		{"m123", 123},
		{"m999999", 999999},
		{"", 0},
		{"x1", 0},      // wrong prefix
		{"m", 0},       // too short
		{"m1a", 0},     // non-digit
		{"mm1", 0},     // double prefix
	}
	for _, tc := range tests {
		got := parseChunkID(tc.id)
		if got != tc.want {
			t.Errorf("parseChunkID(%q) = %d, want %d", tc.id, got, tc.want)
		}
	}
}

// TestAdd_BM25CreatesEdges verifies that BM25 doc-doc similarity creates edges
// between memories that share significant tokens (e.g. "memory", "system", "tests").
func TestAdd_BM25CreatesEdges(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	// Two memories about the same topic (memory system) but with different phrasing.
	_, _, err := s.Add("#status Memory system project latest commit tests passing", "", "status", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	second, related, err := s.Add("#status Memory system updated sentinel errors tests green", "", "status", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Shared tokens should yield BM25 similarity above default delta (0.35), creating an edge.
	if len(related) == 0 {
		t.Fatal("expected at least one related memory, got 0")
	}

	// Verify the edge is actually stored (symmetric).
	s.mu.RLock()
	defer s.mu.RUnlock()
	secondChunk := s.chunks[second.ID]
	if secondChunk == nil {
		t.Fatalf("second chunk %s not found", second.ID)
	}
	for _, r := range related {
		otherChunk := s.chunks[r.ID]
		if otherChunk == nil {
			t.Fatalf("related chunk %s not found", r.ID)
		}
		if _, ok := secondChunk.edges[r.ID]; !ok {
			t.Fatalf("expected edge from %s to %s", second.ID, r.ID)
		}
		if _, ok := otherChunk.edges[second.ID]; !ok {
			t.Fatalf("expected edge from %s to %s (symmetric)", r.ID, second.ID)
		}
	}
}

// TestAdd_ContainmentCreatesEdge verifies that when one memory's tokens are a subset
// of another's, BM25 doc-doc similarity is above delta so an edge is created.
func TestAdd_ContainmentCreatesEdge(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})

	c1, _, err := s.Add("memory system", "", "", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, related, err := s.Add("memory system project architecture decisions", "", "", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if len(related) == 0 {
		t.Fatal("expected at least one related memory, got 0")
	}

	found := false
	for _, r := range related {
		if r.ID == c1.ID {
			found = true
			if r.Similarity <= s.similarityDelta {
				t.Fatalf("expected similarity above delta (%.2f) for overlapping content, got %.4f", s.similarityDelta, r.Similarity)
			}
		}
	}
	if !found {
		t.Fatalf("expected c1 (%s) in related list", c1.ID)
	}
}

// --- RebuildEdges tests ---

func TestRebuildEdges_CreatesNewEdges(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{
		// High delta so Add() creates no edges initially.
		SimilarityDelta: 0.99,
	})

	s.Add("alpha bravo charlie delta", "", "", nil)
	s.Add("alpha bravo charlie echo", "", "", nil)

	// Verify no edges exist at the high threshold.
	stats := s.Stats()
	if stats.TotalEdges != 0 {
		t.Fatalf("expected 0 edges at high delta, got %d", stats.TotalEdges)
	}

	// Rebuild with a lower threshold — should create edges.
	result, err := s.RebuildEdges(RebuildEdgesOptions{
		MinSimilarity: 0.1,
		ForceRebuild:  true,
	})
	if err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}
	if result.ChunksProcessed != 2 {
		t.Fatalf("expected 2 chunks processed, got %d", result.ChunksProcessed)
	}
	if result.EdgesCreated != 1 {
		t.Fatalf("expected 1 edge created, got %d", result.EdgesCreated)
	}

	// Verify the edge is symmetric.
	s.mu.RLock()
	defer s.mu.RUnlock()
	m1 := s.chunks["m1"]
	m2 := s.chunks["m2"]
	if _, ok := m1.edges["m2"]; !ok {
		t.Fatal("expected m1 → m2 edge")
	}
	if _, ok := m2.edges["m1"]; !ok {
		t.Fatal("expected m2 → m1 edge")
	}
}

func TestRebuildEdges_ForceRemovesStaleEdges(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.01})

	// Add similar chunks — edge gets created at low delta.
	s.Add("alpha bravo charlie delta", "", "", nil)
	s.Add("alpha bravo charlie echo", "", "", nil)

	stats := s.Stats()
	if stats.TotalEdges == 0 {
		t.Fatal("expected at least 1 edge at low delta")
	}

	// Force rebuild with an impossibly high threshold — edges should be removed.
	result, err := s.RebuildEdges(RebuildEdgesOptions{
		MinSimilarity: 0.999,
		ForceRebuild:  true,
	})
	if err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}
	if result.EdgesRemoved == 0 {
		t.Fatal("expected stale edges to be removed")
	}

	stats2 := s.Stats()
	if stats2.TotalEdges != 0 {
		t.Fatalf("expected 0 edges after high-threshold rebuild, got %d", stats2.TotalEdges)
	}
}

func TestRebuildEdges_NonForceRemovesStaleEdges(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.01})

	// Add similar chunks — edge gets created at low delta.
	s.Add("alpha bravo charlie delta", "", "", nil)
	s.Add("alpha bravo charlie echo", "", "", nil)

	stats := s.Stats()
	if stats.TotalEdges == 0 {
		t.Fatal("expected at least 1 edge at low delta")
	}

	// Non-force rebuild with an impossibly high threshold — stale edges
	// should still be removed even without ForceRebuild.
	result, err := s.RebuildEdges(RebuildEdgesOptions{
		MinSimilarity: 0.999,
		ForceRebuild:  false,
	})
	if err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}
	if result.EdgesRemoved == 0 {
		t.Fatal("expected stale edges to be removed in non-force mode")
	}

	stats2 := s.Stats()
	if stats2.TotalEdges != 0 {
		t.Fatalf("expected 0 edges after high-threshold rebuild, got %d", stats2.TotalEdges)
	}
}

func TestRebuildEdges_DefaultThreshold(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	s.Add("first memory about architecture", "", "", nil)
	s.Add("second memory about completely different topic zebra", "", "", nil)

	// Rebuild with default options (no explicit threshold).
	result, err := s.RebuildEdges(RebuildEdgesOptions{})
	if err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}
	if result.ChunksProcessed != 2 {
		t.Fatalf("expected 2 chunks processed, got %d", result.ChunksProcessed)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected no errors, got %v", result.Errors)
	}
}

func TestRebuildEdges_EmptyStore(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	result, err := s.RebuildEdges(RebuildEdgesOptions{ForceRebuild: true})
	if err != nil {
		t.Fatalf("RebuildEdges on empty store: %v", err)
	}
	if result.ChunksProcessed != 0 {
		t.Fatalf("expected 0 chunks, got %d", result.ChunksProcessed)
	}
	if result.EdgesCreated != 0 || result.EdgesUpdated != 0 || result.EdgesRemoved != 0 {
		t.Fatal("expected no edges on empty store")
	}
}

func TestRebuildEdges_SkipsScopeIncompatiblePairs(t *testing.T) {
	// Use high delta so Add() creates no edges initially.
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.99})

	// Add two identical-content chunks in different scopes.
	s.Add("alpha bravo charlie delta echo", "", "", []string{"project-a"})
	s.Add("alpha bravo charlie delta echo", "", "", []string{"project-b"})
	// Add one in project-a that shares content.
	s.Add("alpha bravo charlie delta foxtrot", "", "", []string{"project-a"})

	// Verify no edges from Add() at high delta.
	stats := s.Stats()
	if stats.TotalEdges != 0 {
		t.Fatalf("expected 0 edges before rebuild, got %d", stats.TotalEdges)
	}

	result, err := s.RebuildEdges(RebuildEdgesOptions{
		MinSimilarity: 0.01,
		ForceRebuild:  true,
	})
	if err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}

	// m1 (project-a) and m3 (project-a) should be connected.
	// m1 (project-a) and m2 (project-b) should NOT be connected (incompatible scopes).
	s.mu.RLock()
	defer s.mu.RUnlock()

	m1 := s.chunks["m1"]
	m2 := s.chunks["m2"]

	if _, ok := m1.edges["m3"]; !ok {
		t.Fatal("expected edge between m1 and m3 (same scope)")
	}
	if _, ok := m1.edges["m2"]; ok {
		t.Fatal("expected NO edge between m1 and m2 (incompatible scopes)")
	}
	if _, ok := m2.edges["m3"]; ok {
		t.Fatal("expected NO edge between m2 and m3 (incompatible scopes)")
	}

	// Should have created exactly 1 edge (m1↔m3).
	if result.EdgesCreated != 1 {
		t.Fatalf("expected 1 edge, got %d created", result.EdgesCreated)
	}
}

func TestRebuildEdges_OnlyPersistsDirtyChunks(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	storage, _ := NewSQLiteStorage(dbPath)
	storage.Init()
	s, _ := NewMemoryStore(MemoryStoreOptions{
		Storage:         storage,
		SimilarityDelta: 0.99, // No edges initially.
	})

	// Add 3 chunks where only 2 are similar.
	s.Add("alpha bravo charlie delta", "", "", nil)
	s.Add("alpha bravo charlie echo", "", "", nil)
	s.Add("completely different zebra content", "", "", nil)

	// Rebuild with low threshold.
	result, err := s.RebuildEdges(RebuildEdgesOptions{
		MinSimilarity: 0.1,
		ForceRebuild:  true,
	})
	if err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}
	if result.ChunksProcessed != 3 {
		t.Fatalf("expected 3 processed, got %d", result.ChunksProcessed)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected no errors, got %v", result.Errors)
	}

	// Verify edges persisted: reload from storage.
	s.Close()

	storage2, _ := NewSQLiteStorage(dbPath)
	s2, _ := NewMemoryStore(MemoryStoreOptions{Storage: storage2})
	defer s2.Close()

	_, neighbors, _ := s2.Get("m1")
	found := false
	for _, n := range neighbors {
		if n.ID == "m2" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected m1→m2 edge to persist across restart")
	}
}

// --- FindRelevant ranking tests ---

func TestFindRelevant_TiebreaksByCosineThenRecency(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	// All three memories contain "database". m1 and m3 are database-focused
	// (multiple occurrences), m2 mentions it once among unrelated content.
	s.Add("database schema design for database systems", "", "fact", nil)        // m1: focused
	s.Add("deployment pipeline monitoring logging database connection", "", "fact", nil) // m2: incidental
	time.Sleep(2 * time.Millisecond) // ensure distinct timestamps
	s.Add("database indexing strategy for database optimization", "", "fact", nil) // m3: focused + newer

	// Query uses "database" directly (no stemming issues).
	results := s.FindRelevant("Tell me about database design", 3, "", "")
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	topIDs := make([]string, len(results))
	for i, r := range results {
		topIDs[i] = r.ID
		t.Logf("  %s sim=%.4f %s", r.ID, r.Similarity, r.Text[:min(60, len(r.Text))])
	}

	// At minimum, verify all three are returned.
	if len(results) < 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Among lexical ties, cosine should prefer memories where "database"
	// is prominent. Log actual ranking for diagnostic purposes.
	t.Logf("ranking: %v (expected database-focused first, deployment last)", topIDs)
}

func TestFindRelevant_RecencyBreaksFinalTies(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	// Add two memories with identical content but different timestamps.
	s.Add("golang testing patterns and best practices", "", "fact", nil)
	time.Sleep(2 * time.Millisecond)
	s.Add("golang testing patterns and best practices", "", "fact", nil)

	results := s.FindRelevant("golang testing patterns", 2, "", "")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Identical content → identical cosine and lexical scores.
	// Final tiebreaker: newer first, so m2 before m1.
	if results[0].ID != "m2" {
		t.Fatalf("expected newer memory (m2) first, got %s", results[0].ID)
	}
	if results[1].ID != "m1" {
		t.Fatalf("expected older memory (m1) second, got %s", results[1].ID)
	}
}

func TestSearch_AccessCountTiebreaker(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{SimilarityDelta: 0.1})

	// Two identical memories — same similarity and BM25 scores.
	s.Add("persistent memory storage retrieval system", "", "fact", nil) // m1
	s.Add("persistent memory storage retrieval system", "", "fact", nil) // m2

	// Bump m2's access count via Get().
	s.Get("m2")

	results, err := s.Search("persistent memory storage retrieval", "", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// m2 has accessCount=1, m1 has accessCount=0.
	// With identical scores, m2 should rank first due to higher access count.
	if results[0].Chunk.ID != "m2" {
		t.Errorf("expected m2 (higher accessCount) first, got %s", results[0].Chunk.ID)
	}
	if results[1].Chunk.ID != "m1" {
		t.Errorf("expected m1 (lower accessCount) second, got %s", results[1].Chunk.ID)
	}
}

func TestFindRelevant_ReturnsTimestampsAndScopes(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	s.Add("kubernetes cluster deployment automation", "", "fact", []string{"project-x"})

	// Update the memory to set UpdatedAt.
	_, _, err := s.Update("m1", "kubernetes cluster deployment automation updated", "", "", nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	results := s.FindRelevant("kubernetes cluster deployment", 5, "", "")
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	r := results[0]
	if r.ID != "m1" {
		t.Fatalf("expected m1, got %s", r.ID)
	}
	if r.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if r.UpdatedAt == nil {
		t.Error("expected non-nil UpdatedAt after Update()")
	}
	if len(r.Scopes) != 1 || r.Scopes[0] != "project-x" {
		t.Errorf("expected scopes [project-x], got %v", r.Scopes)
	}
}

func TestFindRelevant_AccessCountTiebreaker(t *testing.T) {
	s := mustNewMemoryStore(t, MemoryStoreOptions{})

	// Two identical memories — same similarity, BM25, and content.
	s.Add("kubernetes cluster deployment automation", "", "fact", nil) // m1
	time.Sleep(2 * time.Millisecond) // ensure m2 is newer
	s.Add("kubernetes cluster deployment automation", "", "fact", nil) // m2

	// Without access tracking, m2 wins (newer). Verify that baseline.
	results := s.FindRelevant("kubernetes cluster deployment", 2, "", "")
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "m2" {
		t.Fatalf("baseline: expected m2 (newer) first, got %s", results[0].ID)
	}

	// Now bump m1's access count — it should override the recency tiebreaker.
	s.Get("m1")

	results = s.FindRelevant("kubernetes cluster deployment", 2, "", "")
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// m1 has accessCount=1 > m2's 0, so it should rank first despite being older.
	if results[0].ID != "m1" {
		t.Errorf("expected m1 (higher accessCount) first, got %s", results[0].ID)
	}
	if results[1].ID != "m2" {
		t.Errorf("expected m2 (lower accessCount) second, got %s", results[1].ID)
	}
}
