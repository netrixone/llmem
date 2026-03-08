# BM25 Implementation (2026-02-07)

> **Historical snapshot.** This document records the state of the system at the time of writing. Do not update it to reflect later changes.

Summary of the BM25 ranking integration for query–document scoring in Search and FindRelevant.

## Implementation

- **Formula:** BM25 with standard IDF: `IDF(t) = log((N - df + 0.5)/(df + 0.5) + 1)` and term component `(dtf * (k1+1)) / (dtf + k1 * ((1-b) + b * |D|/avgdl))`. Parameters: `k1 = 1.2`, `b = 0.75`.
- **Document length:** `|D|` = sum of term frequencies in the document (`docLen`). `avgdl` = `totalDocLen / totalDocs`, maintained on Add/Update/Delete and on load.
- **Scope:** Used only for **query–document** scoring (Search, FindRelevant). Doc–doc similarity (edges) still uses TF-IDF cosine + lexical overlap.
- **Normalization:** Raw BM25 is unbounded. For comparison with the existing delta threshold we use `score / (1 + score)` so the value lies in [0, 1).
- **Tie-break:** FindRelevant sorts by best similarity, then by raw BM25, then by recency.

## Code Changes

- **memory.go:** `storedChunk.docLen`, `MemoryStore.totalDocLen`, `docLenFromVector()`, `idfBM25Locked()`, `bm25ScoreLocked()`.
- **memory_add.go:** Set `docLen` on new chunks; update `totalDocLen`.
- **memory_update.go:** Adjust `totalDocLen` when chunk text (and thus vector) changes; set `sc.docLen`.
- **memory_delete.go:** Subtract chunk `docLen` from `totalDocLen`.
- **loadFromStorage:** Compute and set `docLen` per chunk; accumulate `totalDocLen`.
- **memory_search.go:** Content score = normalized BM25; lexical overlap still can override.
- **memory_relevant.go:** Same; tie-break by raw BM25 then recency.
- **bench_retrieval_test.go:** Benchmark store population sets `docLen` and `totalDocLen`.

## Retrieval Quality: Stemming vs Stemming+BM25

Same benchmark: 65 memories, 45 queries (15 easy, 15 medium, 15 hard).  
Before = TF-IDF cosine + stemming; After = BM25 (normalized) + stemming, lexical override unchanged.

### Search

| Tier    | MRR (cosine → BM25) | R@3  | R@5  | P@3  | N |
|---------|----------------------|------|------|------|---|
| easy    | 1.00 → 1.00          | 1.00 | 1.00 | 0.33 | 15 |
| medium  | 0.83 → **0.93**      | **0.93** | **0.93** | **0.36** | 15 |
| hard    | 0.00 → **0.29**      | **0.40** | **0.53** | **0.13** | 15 |
| overall | 0.61 → **0.74**      | **0.78** | **0.82** | **0.27** | 45 |

### FindRelevant

| Tier    | MRR (cosine → BM25) | R@3  | R@5  | P@3  | N |
|---------|----------------------|------|------|------|---|
| easy    | 1.00 → 1.00          | 1.00 | 1.00 | 0.33 | 15 |
| medium  | 0.90 → **0.93**      | **0.93** | **0.93** | **0.36** | 15 |
| hard    | 0.13 → **0.29**      | **0.40** | **0.53** | **0.13** | 15 |
| overall | 0.68 → **0.74**      | **0.78** | **0.82** | **0.27** | 45 |

## Observations

1. **Medium tier:** Search medium MRR 0.83 → 0.93; BM25 improves ranking when multiple docs match.
2. **Hard tier:** Large gain (0.00 → 0.29 Search, 0.13 → 0.29 FindRelevant). BM25’s length normalization and saturation help surface documents that share some roots with the query.
3. **Easy tier:** Unchanged (already at 1.00).
4. **Doc–doc edges:** Still use TF-IDF cosine + lexical; no BM25 there (BM25 is query–document asymmetric).

## References

- Stemming doc: `doc/20260206_stemming_implementation.md`
- Baseline (no stemming): `doc/20260206_bench_retrieval_tf_idf.md`
- Benchmark: `go test -v -run TestRetrievalQuality`
