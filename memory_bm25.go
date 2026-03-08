package main

import "math"

// BM25 parameters (standard defaults).
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// bm25IdfLocked returns the BM25 IDF for a token: log((N - df + 0.5)/(df + 0.5) + 1).
// Caller must hold at least a read lock.
func (s *MemoryStore) bm25IdfLocked(tok string) float64 {
	df := float64(s.tokenDocFreq[tok])
	N := float64(s.totalDocs)
	if N <= 0 {
		return 0
	}
	idf := math.Log((N-df+0.5)/(df+0.5) + 1)
	if idf < 0 {
		return 0
	}
	return idf
}

// bm25MemSimilarityLocked returns a symmetric doc-doc similarity in [0,1) using BM25.
// score = max(bm25(a→b), bm25(b→a)) then normalized as raw/(1+raw) for threshold comparison.
// Caller must hold at least a read lock.
func (s *MemoryStore) bm25MemSimilarityLocked(a, b *storedChunk) float64 {
	if a == nil || b == nil {
		return 0
	}
	scoreAB := s.bm25ScoreLocked(a.vector, b)
	scoreBA := s.bm25ScoreLocked(b.vector, a)
	raw := scoreAB
	if scoreBA > raw {
		raw = scoreBA
	}
	if raw <= 0 {
		return 0
	}
	return raw / (1 + raw)
}

// bm25ScoreLocked returns the BM25 score of the document for the query vector.
// Query term weights (qVec values) are incorporated: each term's contribution
// is scaled by its query weight, so synonym-expanded terms at reduced weight
// contribute proportionally less than original query terms.
// Caller must hold at least a read lock.
func (s *MemoryStore) bm25ScoreLocked(qVec map[string]float64, doc *storedChunk) float64 {
	if len(qVec) == 0 || doc == nil || len(doc.vector) == 0 {
		return 0
	}
	if s.totalDocs == 0 || s.totalDocLen <= 0 {
		return 0
	}
	avgDocLen := s.totalDocLen / float64(s.totalDocs)
	if avgDocLen <= 0 {
		return 0
	}
	if doc.docLen <= 0 {
		return 0
	}
	var score float64
	docLenNorm := 1 - bm25B + bm25B*(doc.docLen/avgDocLen)
	for tok, qw := range qVec {
		dtf, ok := doc.vector[tok]
		if !ok || dtf <= 0 {
			continue
		}
		idf := s.bm25IdfLocked(tok)
		score += qw * idf * (dtf * (bm25K1 + 1)) / (dtf + bm25K1*docLenNorm)
	}
	return score
}

// docLenFromVector returns the sum of term frequencies (total token count) for BM25.
func docLenFromVector(vec map[string]float64) float64 {
	var sum float64
	for _, tf := range vec {
		sum += tf
	}
	return sum
}
