package main

import (
	"strings"
	"testing"
)

// TestSynonymIndexLoaded verifies the WordNet synonym data was parsed correctly.
func TestSynonymIndexLoaded(t *testing.T) {
	if len(synonymIndex) == 0 {
		t.Fatal("synonymIndex is empty — wordnet_synonyms.txt not loaded")
	}
	t.Logf("synonymIndex contains %d terms", len(synonymIndex))

	// Spot-check some well-known WordNet synsets.
	checks := []struct {
		term   string
		hasSyn string
	}{
		{"shut", "close"},    // shut/close are in same synset
		{"big", "large"},     // big/large
		{"happi", "glad"},    // happy/glad (stemmed)
		{"fast", "quick"},    // fast/quick
		{"begin", "start"},   // begin/start
		{"car", "automobil"}, // car/automobile (stemmed)
	}
	for _, c := range checks {
		st := stem(c.term)
		se := stem(c.hasSyn)
		syns, ok := synonymIndex[st]
		if !ok {
			t.Errorf("term %q (stem %q) not in synonymIndex", c.term, st)
			continue
		}
		found := false
		for _, s := range syns {
			if s == se {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("synonymIndex[%q] does not contain %q (stem of %q); has: %v",
				st, se, c.hasSyn, truncSlice(syns, 10))
		}
	}
}

// TestSynonymAllTermsStemmed verifies every term in the embedded data
// is idempotent under stem() — i.e., already in Porter-stemmed form.
func TestSynonymAllTermsStemmed(t *testing.T) {
	lines := strings.Split(wordnetSynonyms, "\n")
	failures := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, term := range strings.Split(line, "\t") {
			if s := stem(term); s != term {
				if failures < 20 {
					t.Errorf("term %q stems to %q (not idempotent)", term, s)
				}
				failures++
			}
		}
	}
	if failures > 20 {
		t.Errorf("... and %d more non-idempotent terms", failures-20)
	}
}

// mockCorpus builds a fake tokenIndex containing the given terms.
func mockCorpus(terms ...string) map[string]map[string]struct{} {
	idx := make(map[string]map[string]struct{}, len(terms))
	for _, t := range terms {
		idx[t] = map[string]struct{}{"doc1": {}}
	}
	return idx
}

// TestSynonymExpansionVector verifies expandQueryVectorCorpus preserves original
// weights and adds synonyms at the reduced weight.
func TestSynonymExpansionVector(t *testing.T) {
	term := stem("shut")
	// Build a corpus that contains "close" (a synonym of "shut").
	closeStem := stem("close")
	corpus := mockCorpus(term, closeStem)

	qVec := map[string]float64{term: 2.0}
	expanded := expandQueryVectorCorpus(qVec, corpus)

	if expanded[term] != 2.0 {
		t.Errorf("original token weight: got %f, want 2.0", expanded[term])
	}

	// "close" should be added since it's in the corpus.
	want := 2.0 * synonymExpansionWeight
	if expanded[closeStem] != want {
		t.Errorf("synonym %q weight: got %f, want %f", closeStem, expanded[closeStem], want)
	}
}

// TestSynonymExpansionCorpusFiltering verifies synonyms NOT in the corpus are excluded.
func TestSynonymExpansionCorpusFiltering(t *testing.T) {
	term := stem("shut")
	// Empty corpus — no synonyms should be added.
	emptyCorpus := map[string]map[string]struct{}{}

	qVec := map[string]float64{term: 1.0}
	expanded := expandQueryVectorCorpus(qVec, emptyCorpus)
	if len(expanded) != 1 {
		t.Errorf("expected 1 token with empty corpus, got %d: %v", len(expanded), expanded)
	}

	tokens := map[string]struct{}{term: {}}
	expandedSet := expandTokenSetCorpus(tokens, emptyCorpus)
	if len(expandedSet) != 1 {
		t.Errorf("expected 1 token in set with empty corpus, got %d", len(expandedSet))
	}
}

// TestSynonymExpansionTokenSet verifies expandTokenSetCorpus includes corpus-present synonyms.
func TestSynonymExpansionTokenSet(t *testing.T) {
	term := stem("shut")
	closeStem := stem("close")
	corpus := mockCorpus(term, closeStem)

	tokens := map[string]struct{}{term: {}}
	expanded := expandTokenSetCorpus(tokens, corpus)

	if _, ok := expanded[term]; !ok {
		t.Error("expanded set missing original token")
	}
	if _, ok := expanded[closeStem]; !ok {
		t.Errorf("expanded set missing corpus-present synonym %q", closeStem)
	}
}

// TestSynonymExpansionNoOp verifies unknown tokens pass through unchanged.
func TestSynonymExpansionNoOp(t *testing.T) {
	corpus := mockCorpus("anything")

	qVec := map[string]float64{"xyzunknown": 1.0}
	expanded := expandQueryVectorCorpus(qVec, corpus)
	if len(expanded) != 1 {
		t.Errorf("expected 1 token, got %d", len(expanded))
	}

	tokens := map[string]struct{}{"xyzunknown": {}}
	expandedSet := expandTokenSetCorpus(tokens, corpus)
	if len(expandedSet) != 1 {
		t.Errorf("expected 1 token in set, got %d", len(expandedSet))
	}
}

func truncSlice(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
