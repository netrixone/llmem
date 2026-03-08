# Agent guide — llmem

This document gives AI agents (and humans) the patterns, style, and constraints of this codebase so changes stay consistent.

## What this project is

- **llmem** (binary `llmem`): persistent memory service for AI agents. Store, search, and retrieve memories with similarity matching (BM25 throughout; optional lexical boost for query–doc). Porter stemming. Built for Cursor via MCP; also exposes a REST API.
- **Tech**: Go 1.24+, single `main` package. HTTP: Gin. MCP: `modelcontextprotocol/go-sdk`. Storage: SQLite (`modernc.org/sqlite`). Tokenization: `rekram1-node/tokenizer`; stemming: in-house Porter in `stemmer.go`.

## Development patterns

### Layout and files

- One main concern per file: `memory.go` (core store), `memory_add.go`, `memory_get.go`, `memory_search.go`, `memory_relevant.go`, `memory_context.go`, etc. BM25 helpers in `memory_bm25.go`; storage interface and SQLite in `storage.go`; API and MCP in `api.go` and `mcp_server.go`.
- Options structs for construction: `MemoryStoreOptions`, `TgptSummarizerOptions`. Use them instead of long argument lists.
- Internal entry points when the same behavior is needed from two callers: e.g. `addInternal(..., addOpts)` used by `Add` and `Import` so Import can preserve ID and timestamps without changing the public `Add` API.

### Concurrency and locking

- All store reads/writes go through `MemoryStore.mu` (RWMutex). Writes use `s.mu.Lock()`, reads use `s.mu.RLock()`. Methods that assume the lock is held are named with a `Locked` suffix (e.g. `bm25MemSimilarityLocked`, `neighborsLocked`, `bm25ScoreLocked`).
- Persistence: `toStorageData()` does a **deep copy** of maps/slices so the snapshot is safe after releasing the lock. When adding new map/slice fields to `storedChunk`, they must be copied in `toStorageData()` too.
- Async writes (e.g. access tracking) use `s.wg.Add(1)` before the goroutine and `defer s.wg.Done()` inside. `Close()` calls `s.wg.Wait()` before closing storage so in-flight writes finish.
- SQLite: single connection (`SetMaxOpenConns(1)`), WAL mode, `busy_timeout` to avoid SQLITE_BUSY.

### Errors and API

- Sentinel errors in `memory.go`: `ErrNotFound`, `ErrEmptyID`, `ErrIncompatibleTypes`, `ErrTooFewIDs`. Handlers use `errors.Is(err, ErrNotFound)` to map to HTTP 404; others typically 400/500.
- REST: request/response types per endpoint (e.g. `createMemoryRequest`, `createMemoryResponse`). Bind with `c.ShouldBindJSON(&req)`. Return errors as `errorResponse{Error: err.Error()}`. Always trim and validate IDs and query params (e.g. `strings.TrimSpace(c.Param("id"))`).
- Ensure JSON arrays are never null when it matters: e.g. `if related == nil { related = []RelatedMemory{} }` before sending.

### Similarity and retrieval

- **Doc–doc edges** (who is similar to whom): BM25 in `memory_bm25.go` — `bm25MemSimilarityLocked(a, b)` = max(bm25(a→b), bm25(b→a)) normalized as `score/(1+score)` to [0,1). Used by Add, Update, RebuildEdges. Threshold: `DefaultSimilarityDelta` (0.35).
- **Query–doc (Search / FindRelevant)**: shared core in `scoreChunksLocked` (`memory.go`) — BM25 via `bm25ScoreLocked` + optional lexical boost via `lexicalOverlapScoreLocked` / `idfLocked`, with corpus-aware synonym expansion. Search sort: sim → accessCount → ID. FindRelevant: lower threshold (0.2); sort sim → BM25 raw → accessCount → recency.
- **Context restoration** (`memory_context`): six priority hashtags at **start** of text: `#self`, `#goal`, `#relationship`, `#status`, `#principle`, `#thought`. Sorted by `effectiveTime = max(UpdatedAt, CreatedAt)`; configurable `per_category`.

### Storage and schema

- `Storage` interface: `Init`, `LoadAll`, `Save`, `Delete`, `Close`. `storedChunkData` is the serializable shape; it includes vectors, edges, tokens, access fields. SQL schema has legacy columns (`label_vector_json`, `label_norm`); code writes harmless defaults and ignores them on load. Do not remove columns; keep LoadAll/Save in sync with any new fields.

### Tests and tooling

- `make test` and `make test-race` must pass before considering a change done. Tests use `NullStorage` and a `fakeSummarizer` where appropriate. Helpers like `mustNewMemoryStore(t, opts)` for setup. Prefer focused tests and table-driven style where it clarifies cases.
- Do **not** restart the llmem service unless strictly necessary: restarting breaks the MCP client connection in Cursor and makes the memory tools appear broken.

## Code style

- Prefer concise, readable code; comment when the “why” or concurrency/data flow is non-obvious.
- Constants for magic numbers: `MaxMemoryBytes`, `DefaultSimilarityDelta`, `bm25K1`, `bm25B`. Config via `envOr` / `envOrDuration` (e.g. `LLMEM_PORT`, `LLMEM_DB`, `LLMEM_AUTO_CONSOLIDATE_INTERVAL`).
- Similarity threshold: `MemoryStore.similarityDelta`; set via `MemoryStoreOptions.SimilarityDelta`.
- IDs: generated with `newID()` (atomic counter, format `m1`, `m2`, …). `parseChunkID` for numeric part; when importing, advance `nextID` so future IDs don’t collide.
- Scopes: `normalizeScopes` for trim/dedup; `matchesScope` / `scopesCompatible` for filtering and edge creation. Global = nil/empty scopes.

## Things to avoid

- Adding new map/slice fields to `storedChunk` without updating `toStorageData()` (risk of races with async persistence).
- Using `Get` inside consolidation when you only need to read chunk data (it increments access count); read under RLock from `s.chunks` instead.
- Holding `s.mu` across I/O or external calls (storage, summarizer); snapshot under lock, then release before calling out.
- Adding a “Versioning” section to the README (explicitly removed by the maintainer).
- Restarting the service from an agent workflow when you can avoid it (MCP in Cursor depends on a long-lived process).

## Connecting to memory MCP tools

The llmem service exposes 15 MCP tools (e.g. `memory_add`, `memory_search`, `memory_context`). In Cursor, the tool names are prefixed by the MCP server identifier — but the **exact prefix varies between sessions and Cursor versions**:

- `user-llmem-memory_*` (e.g. `user-llmem-memory_stats`)
- `mcp_llmem_memory_*` (e.g. `mcp_llmem_memory_stats`)

If tools return "Tool not found", try the other prefix before falling back to the REST API.

**REST fallback** — The same operations are always available via HTTP at `localhost:9980`:
```
curl http://localhost:9980/v1/memories/context?per_category=2
curl http://localhost:9980/v1/memories/relevant?message=your+query&limit=5
curl http://localhost:9980/v1/stats
```

## Always do

- With every prompt regarding this project, automatically use the [llmem-usage skill](.cursor/skills/llmem-usage/SKILL.md) to refresh your context.

## Quick reference

| Area        | Location / pattern |
|------------|---------------------|
| Core store | `memory.go`: `MemoryStore`, `storedChunk`, `scoredChunk`, `toStorageData`, `loadFromStorage` |
| Scoring    | `memory.go`: `scoreChunksLocked` — shared BM25 + lexical scoring core used by both Search and FindRelevant |
| Add/Import | `memory_add.go`: `Add` → `addInternal` with `addOpts` |
| Search     | `memory_search.go`: calls `scoreChunksLocked`; sort by sim → accessCount → ID; Neighbors computed after limit |
| Relevant   | `memory_relevant.go`: keyword extraction → `scoreChunksLocked` (threshold 0.2); sort by sim → bm25Raw → accessCount → recency; `RelevantMemory` includes timestamps + scopes |
| Context    | `memory_context.go`: hashtags at start of text, per_category, effectiveTime |
| Edges      | `memory_bm25.go`: `bm25MemSimilarityLocked`; used in `memory_add.go`, `memory_update.go`, `memory_rebuild.go`; rebuild removes stale edges below threshold |
| REST       | `api.go`: Gin, `/v1/*`, request/response structs, `errorResponse` |
| MCP        | `mcp_server.go`: `AddTool` with typed in/out and jsonschema tags |
| Storage    | `storage.go`: `Storage` interface, `SQLiteStorage`, `NullStorage`, migrations via `ALTER TABLE` |

Use this file as the single source of truth for how to work in this repo; keep it updated when patterns or constraints change.
