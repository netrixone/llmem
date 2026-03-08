# Stemming Implementation — Porter Stemmer (2026-02-06)

> **Historical snapshot.** This document records the state of the system at the time of writing. Do not update it to reflect later changes.

Summary of the Porter stemmer integration into llmem’s tokenization pipeline and its impact on retrieval quality.

## Implementation

- **Algorithm:** Porter stemmer (English), pure Go, zero dependencies.
- **Location:** `stemmer.go` (new); integration in `utils.go` (`tokenize()`).
- **Scope:** All tokens produced by `tokenize()` are stemmed before use in vectors, token sets, and the token index. Hashtags (e.g. `#status`, `#goal`) are preserved without stemming.
- **Punctuation:** Trailing/leading punctuation is stripped from tokens before stemming so the stemmer sees clean word forms (e.g. `metrics.` → `metrics` → `metric`).
- **Persistence:** On load, chunks are re-tokenized from stored text so existing data uses the current (stemmed) tokenization.
- **FindRelevant fix:** Query vector and token set are built directly from the already-stemmed keyword list to avoid double-stemming.

## Retrieval Quality: Before vs After

Same benchmark as baseline: 65 memories, 45 queries (15 easy, 15 medium, 15 hard).  
Baseline = TF-IDF without stemming; After = TF-IDF + Porter stemming.

### Search

| Tier    | MRR (before → after) | R@3 (before → after) | R@5 (before → after) | P@3 (before → after) | N |
|---------|------------------------|------------------------|------------------------|-------------------------|---|
| easy    | 1.00 → 1.00           | 1.00 → 1.00           | 1.00 → 1.00           | 0.33 → 0.33             | 15 |
| medium  | 0.67 → **0.83**       | 0.67 → **0.87**       | 0.67 → **0.87**       | 0.24 → **0.31**         | 15 |
| hard    | 0.00 → 0.00           | 0.00 → 0.00           | 0.00 → 0.00           | 0.00 → 0.00             | 15 |
| overall | 0.56 → **0.61**       | 0.56 → **0.62**       | 0.56 → **0.62**       | 0.19 → 0.21             | 45 |

### FindRelevant

| Tier    | MRR (before → after) | R@3 (before → after) | R@5 (before → after) | P@3 (before → after) | N |
|---------|------------------------|------------------------|------------------------|-------------------------|---|
| easy    | 1.00 → 1.00           | 1.00 → 1.00           | 1.00 → 1.00           | 0.33 → 0.33             | 15 |
| medium  | 0.87 → **0.90**       | 0.87 → **0.93**       | 0.87 → **0.93**       | 0.33 → 0.36             | 15 |
| hard    | 0.07 → **0.13**       | 0.07 → **0.13**       | 0.07 → **0.13**       | 0.02 → 0.04             | 15 |
| overall | 0.64 → **0.68**       | 0.64 → **0.69**       | 0.64 → **0.69**       | 0.23 → 0.24             | 45 |

## Observations

1. **Medium tier gains are largest.** Stemming fixes morphological mismatches (e.g. credentials/credential, throttling/throttle). Search medium MRR +24% (0.67→0.83); FindRelevant medium MRR +3% (0.87→0.90).
2. **Easy tier unchanged.** Queries already matched; stemming does not change behavior.
3. **Hard tier still near zero for Search.** Those queries have little or no shared root words; lexical-only retrieval cannot fix them.
4. **FindRelevant hard tier improves slightly** (0.07→0.13 MRR), reflecting a few extra matches when query and memory share stemmed roots.

## Stemmer Performance

- **Benchmark:** `BenchmarkStem` (10 words per op).
- **Result:** ~2.5 µs/op, 80 B/op, 10 allocs/op.
- **Impact:** Negligible relative to tokenization and similarity scoring.

## References

- Baseline (no stemming): `doc/20260206_bench_retrieval_tf_idf.md`
- Benchmark: `go test -v -run TestRetrievalQuality`
- Stemmer tests: `go test -run TestStem`
