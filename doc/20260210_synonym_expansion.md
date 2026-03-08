# Synonym Expansion (2026-02-10, updated 2026-02-16)

> **Historical snapshot.** This document records the state of the system at the time of writing. Do not update it to reflect later changes.

Summary of query-time synonym expansion for bridging vocabulary mismatch in Search and FindRelevant.

## Motivation

BM25 scoring achieves MRR 1.00 on easy queries but MRR 0.29 on hard-tier queries where the query uses different vocabulary than stored memories (e.g., "subroutine" vs "function", "orderly termination" vs "graceful shutdown"). Synonym expansion is a standard IR technique that bridges this vocabulary gap at query time with zero latency cost and no external dependencies.

## Implementation

- **Synonym source:** WordNet 3.1 synsets, pre-processed by `cmd/gensynonyms` into `wordnet_synonyms.txt` (22,726 groups, 31,712 terms, embedded via `//go:embed`).
- **Corpus-filtered expansion:** `expandQueryVectorCorpus` and `expandTokenSetCorpus` only inject synonyms that appear in the store's `tokenIndex`, avoiding noise from synonyms with no matching documents.
- **Query-weighted BM25:** `bm25ScoreLocked` multiplies each term's BM25 contribution by its query weight. Synonym tokens are injected at `0.3x` the original token's weight, so they contribute proportionally less than original query terms. This prevents polysemous WordNet entries (e.g., "run"→50 synonyms, "function"→21) from drowning the signal.
- **Expansion weight:** Tuned to 0.3 (sweep tested 0.1–0.5; 0.2–0.3 is optimal for this corpus).
- **Scope:** Query-time only. Stored document vectors and token indexes are unchanged.

## Code

- **`synonyms.go`:** `synonymIndex` (built from embedded `wordnet_synonyms.txt` at init), `expandQueryVectorCorpus()`, `expandTokenSetCorpus()`.
- **`synonyms_test.go`:** Tests for expansion, stem idempotency, no-op cases.
- **`memory_search.go`:** Expansion calls after `vectorize()`/`tokenSet()` inside the read lock.
- **`memory_relevant.go`:** Expansion calls after keyword extraction inside the read lock.
- **`memory_bm25.go`:** `bm25ScoreLocked` uses query weights (`qw`) when summing term contributions.

## Retrieval Quality: BM25 vs BM25+Synonyms

Same benchmark: 65 memories, 45 queries (15 easy, 15 medium, 15 hard).
Before = BM25 + stemming; After = BM25 + stemming + WordNet synonym expansion.

### Search

| Tier    | MRR (BM25 → +syn) | R@3  | R@5  | P@3  | N  |
|---------|--------------------|------|------|------|----|
| easy    | 1.00 → 1.00       | 1.00 | 1.00 | 0.33 | 15 |
| medium  | 0.93 → 0.93       | 0.93 | 0.93 | 0.33 | 15 |
| hard    | 0.29 → **0.35**   | 0.40 | 0.60 | 0.13 | 15 |
| overall | 0.74 → **0.76**   | 0.78 | 0.84 | 0.27 | 45 |

### FindRelevant

| Tier    | MRR (BM25 → +syn) | R@3  | R@5  | P@3  | N  |
|---------|--------------------|------|------|------|----|
| easy    | 1.00 → 1.00       | 1.00 | 1.00 | 0.33 | 15 |
| medium  | 0.93 → 0.93       | 0.93 | 0.93 | 0.33 | 15 |
| hard    | 0.29 → **0.35**   | 0.40 | 0.60 | 0.13 | 15 |
| overall | 0.74 → **0.76**   | 0.78 | 0.84 | 0.27 | 45 |

## Observations

1. **Improvement over BM25-only:** Search MRR 0.74 → 0.76; hard tier 0.29 → 0.35; R@5 0.82 → 0.84. No regression on easy or medium.
2. **Remaining hard misses (4/15 Search, 5/15 FindRelevant):** "optimal daily schedule" (no vocabulary bridge), "keeping version history clean" (too many word senses), "profiling resource consumption" (no synonym path), "orderly process termination" (FindRelevant: noisy expansion of "process"). These would require semantic embeddings to resolve.

## References

- BM25 doc: `doc/20260207_bm25_implementation.md`
- Stemming doc: `doc/20260206_stemming_implementation.md`
- Baseline (no stemming): `doc/20260206_bench_retrieval_tf_idf.md`
- Benchmark: `go test -v -run TestRetrievalQuality`
