# Retrieval Quality Baseline — TF-IDF (2026-02-06)

> **Historical snapshot.** This document records the state of the system at the time of writing. Do not update it to reflect later changes.

Baseline numbers for the current TF-IDF cosine similarity system,
measured with `bench_retrieval_test.go` (65 curated memories, 45 queries across 3 difficulty tiers).

## Retrieval Quality

### Search

| Tier    | MRR  | R@3  | R@5  | P@3  |  N |
|---------|------|------|------|------|----|
| easy    | 1.00 | 1.00 | 1.00 | 0.33 | 15 |
| medium  | 0.67 | 0.67 | 0.67 | 0.24 | 15 |
| hard    | 0.00 | 0.00 | 0.00 | 0.00 | 15 |
| overall | 0.56 | 0.56 | 0.56 | 0.19 | 45 |

### FindRelevant

| Tier    | MRR  | R@3  | R@5  | P@3  |  N |
|---------|------|------|------|------|----|
| easy    | 1.00 | 1.00 | 1.00 | 0.33 | 15 |
| medium  | 0.87 | 0.87 | 0.87 | 0.33 | 15 |
| hard    | 0.07 | 0.07 | 0.07 | 0.02 | 15 |
| overall | 0.64 | 0.64 | 0.64 | 0.23 | 45 |

### Misses (Search)

5 medium-tier and all 15 hard-tier queries returned no relevant result in the top results:

| Tier   | Query                                                           | Expected |
|--------|-----------------------------------------------------------------|----------|
| medium | best practices for managing sensitive credentials               | m45      |
| medium | who manages infrastructure and Kubernetes                       | m57      |
| medium | what are the current sprint objectives                          | m35      |
| medium | how is the web UI built and what framework does it use          | m12      |
| medium | request throttling and rate limit configuration                 | m13      |
| hard   | prevent repetitive information buildup in the store             | m30      |
| hard   | subroutine size guidelines                                      | m40      |
| hard   | inversion of control pattern for external integrations          | m42      |
| hard   | optimal daily schedule for focused coding                       | m7       |
| hard   | regular check-in cadence with the product owner                 | m58      |
| hard   | documentation and annotation philosophy for source files        | m5       |
| hard   | keeping version history clean and meaningful                     | m44      |
| hard   | orderly process termination under load                          | m47      |
| hard   | decomposing a unified codebase into independently deployable units | m27   |
| hard   | rationale for running the data store in-process                 | m21      |
| hard   | safeguarding runtime config values from leaking into repositories | m45    |
| hard   | service health visibility and uptime tracking                   | m53      |
| hard   | profiling resource consumption in the backend                   | m64      |
| hard   | what caused the slow endpoint responses and how was it resolved | m34      |
| hard   | finding related concepts even when phrasing differs             | m61      |

## Latency

CPU: Intel Core Ultra 7 164U, 3 runs each (`go test -bench BenchmarkRetrieval -benchmem -count=3`).

### Search

| Store Size | Latency (ns/op) | Allocs/op |
|------------|------------------|-----------|
| 100        | ~237,000 (237 µs) | 15       |
| 1,000      | ~2,837,000 (2.8 ms) | 15     |
| 10,000     | ~44,795,000 (44.8 ms) | 15   |

### FindRelevant

| Store Size | Latency (ns/op) | Allocs/op |
|------------|------------------|-----------|
| 100        | ~233,000 (233 µs) | 19       |
| 1,000      | ~3,200,000 (3.2 ms) | 19     |
| 10,000     | ~51,200,000 (51.2 ms) | 22   |

## Key Observations

1. **Easy tier is perfect.** Direct lexical overlap queries achieve MRR 1.00 — TF-IDF handles these flawlessly.
2. **Medium tier is decent but not great.** `FindRelevant` (0.87 MRR) outperforms `Search` (0.67 MRR). Failures occur when distractors share tokens with the query.
3. **Hard tier is nearly zero.** 0/15 queries succeed with `Search`, 1/15 with `FindRelevant`. Synonyms, paraphrases, and conceptual relationships have no lexical overlap for TF-IDF to exploit.
4. **Latency scales linearly** with store size (~45–50 ms at 10K memories, 15 allocs/op for Search). This exceeds the sub-10 ms goal at 10K memories stated in the Q2 2026 target.
5. **The hard tier is the primary improvement target.** Embedding-based retrieval should show the largest gains here, bridging the semantic gap that TF-IDF cannot cross.
