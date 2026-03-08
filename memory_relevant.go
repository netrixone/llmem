package main

import (
	"log"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RelevantMemory is a simplified match for mid-conversation relevance.
// Includes timestamps so agents can assess memory freshness.
type RelevantMemory struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Type       string     `json:"type,omitempty"`
	Text       string     `json:"text"`
	Scopes     []string   `json:"scopes,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
	Similarity float64    `json:"similarity"`
}

// conversationalStopWords are filtered out when extracting keywords from user messages.
// These are common in questions/requests but don't help find relevant memories.
// Both raw and Porter-stemmed forms must be present because extractKeywords receives
// already-stemmed tokens from tokenize(). Without stemmed forms, words like "does"→"doe"
// or "being"→"be" leak through and pollute the query.
var conversationalStopWords = map[string]struct{}{
	// Question words
	"what": {}, "how": {}, "why": {}, "when": {}, "where": {}, "which": {}, "who": {},
	// Common verbs in requests (raw + stemmed where they differ)
	"can": {}, "could": {}, "would": {}, "should": {}, "will": {}, "do": {}, "does": {}, "doe": {},
	"is": {}, "are": {}, "ar": {}, "was": {}, "wa": {}, "were": {}, "be": {}, "been": {}, "being": {},
	"have": {}, "has": {}, "ha": {}, "had": {}, "let": {}, "make": {}, "want": {}, "need": {},
	"tell": {}, "show": {}, "help": {}, "please": {}, "pleas": {}, "think": {}, "know": {},
	// Pronouns (raw + stemmed where they differ)
	"i": {}, "you": {}, "we": {}, "they": {}, "thei": {}, "he": {}, "she": {}, "it": {},
	"me": {}, "my": {}, "your": {}, "our": {}, "their": {}, "this": {}, "thi": {}, "that": {},
	// Articles and prepositions
	"a": {}, "an": {}, "the": {}, "of": {}, "to": {}, "in": {}, "for": {}, "on": {},
	"with": {}, "at": {}, "by": {}, "from": {}, "about": {}, "into": {}, "through": {},
	// Conjunctions (raw + stemmed where they differ)
	"and": {}, "or": {}, "but": {}, "if": {}, "then": {}, "so": {}, "because": {}, "becaus": {},
	// Other common words (raw + stemmed where they differ)
	"just": {}, "also": {}, "some": {}, "any": {}, "ani": {}, "all": {}, "more": {}, "most": {},
	"very": {}, "veri": {}, "really": {}, "realli": {}, "actually": {}, "actual": {},
	"maybe": {}, "mayb": {}, "probably": {}, "probabl": {},
}

var hashtagPattern = regexp.MustCompile(`#[a-z0-9_-]+`)

// extractKeywords pulls meaningful content words from conversational text.
// Preserves hashtags (important for memory categorization).
func extractKeywords(text string) []string {
	lower := strings.ToLower(text)
	tokens := tokenize(lower)
	hashtags := hashtagPattern.FindAllString(lower, -1)

	keywords := make([]string, 0, len(tokens)+len(hashtags))
	seen := make(map[string]struct{}, len(tokens)+len(hashtags))

	// Always keep hashtags (important for memory categories), even if tokenizer drops them.
	for _, tag := range hashtags {
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		keywords = append(keywords, tag)
	}

	for _, tok := range tokens {
		tok = strings.Trim(tok, ".,;:!?\"'()[]{}")
		if tok == "" {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}

		// Skip very short words (likely not meaningful)
		if len(tok) < 3 {
			continue
		}

		// Skip conversational stop words
		if _, isStop := conversationalStopWords[tok]; isStop {
			continue
		}

		seen[tok] = struct{}{}
		keywords = append(keywords, tok)
	}

	return keywords
}

// FindRelevant searches for memories relevant to a user message.
// Uses a lower similarity threshold and extracts keywords from conversational text.
// Returns up to `limit` most relevant memories (0 means default of 5).
// If scope is non-empty, only chunks matching that scope (or global) are considered.
// If typeFilter is non-empty, only chunks with that type are considered.
func (s *MemoryStore) FindRelevant(userMessage string, limit int, scope string, typeFilter string) []RelevantMemory {
	if limit <= 0 {
		limit = 5
	}

	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" {
		return []RelevantMemory{}
	}

	// Extract meaningful keywords from the conversational message.
	// Keywords are already stemmed (via tokenize), so we build vectors
	// directly to avoid double-stemming.
	keywords := extractKeywords(userMessage)
	if len(keywords) == 0 {
		return []RelevantMemory{}
	}

	// Build query vector and token set directly from stemmed keywords.
	qVec := make(map[string]float64, len(keywords))
	for _, kw := range keywords {
		qVec[kw] += 1
	}
	qTokens := make(map[string]struct{}, len(keywords))
	for _, kw := range keywords {
		qTokens[kw] = struct{}{}
	}

	// Use a lower threshold for relevance (0.2 vs 0.35 for regular search).
	// User messages are noisier, so we want to be more permissive.
	const relevanceThreshold = 0.2

	s.mu.RLock()
	defer s.mu.RUnlock()

	matches := s.scoreChunksLocked(qVec, qTokens, relevanceThreshold, typeFilter, scope)

	// Sort by similarity desc; break ties using raw BM25
	// (discriminates content richness — primary topic scores higher than incidental mention),
	// then accessCount (frequently accessed = more likely important),
	// then recency.
	sort.Slice(matches, func(i, j int) bool {
		if math.Abs(matches[i].sim-matches[j].sim) < simEpsilon {
			if math.Abs(matches[i].bm25Raw-matches[j].bm25Raw) < simEpsilon {
				if matches[i].chunk.accessCount != matches[j].chunk.accessCount {
					return matches[i].chunk.accessCount > matches[j].chunk.accessCount
				}
				return matches[i].chunk.CreatedAt.After(matches[j].chunk.CreatedAt)
			}
			return matches[i].bm25Raw > matches[j].bm25Raw
		}
		return matches[i].sim > matches[j].sim
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}

	result := make([]RelevantMemory, len(matches))
	for i, m := range matches {
		result[i] = RelevantMemory{
			ID:         m.chunk.ID,
			Label:      m.chunk.Label,
			Type:       m.chunk.Type,
			Text:       m.chunk.Text,
			Scopes:     m.chunk.Scopes,
			CreatedAt:  m.chunk.CreatedAt,
			UpdatedAt:  m.chunk.UpdatedAt,
			Similarity: m.sim,
		}
	}

	log.Printf("MEMORY: RELEVANT '%s'", ExcerptForLog(userMessage, logExcerptLen))
	return result
}
