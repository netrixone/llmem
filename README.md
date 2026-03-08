# llmem

A persistent memory service for AI agents. Store, search, and retrieve contextual memories with BM25 similarity matching.

Built for [Claude Code](https://code.claude.com/), [Cursor](https://cursor.sh) and other local agents via [MCP](https://modelcontextprotocol.io) (Model Context Protocol).

## Features

- **Persistent Storage** — SQLite-backed, survives restarts
- **Semantic Search** — BM25 with optional WordNet synonym expansion
- **MCP Integration** — 15 tools exposed via Streamable HTTP
- **REST API** — Full CRUD operations on port 9980
- **Context Restoration** — Priority-tagged memories for session startup
- **Mid-conversation Relevance** — Extract keywords from natural language queries
- **Access Tracking** — Tracks memory access patterns for importance weighting
- **Auto-Consolidation** — Automatically merges highly similar memories to prevent bloat

## Quick Start

```bash
# Build and run
make run

# Or run in background
make run-bg

# Check status
make status
```

## Memory Management

### Memory Type (optional)

Memories can have an optional **type** (e.g. `story`, `fact`, `artifact`, `note`, `decision`). Type is separate from label and hashtags:

- **Type** — What kind of thing this is (used for filtering and future policies).
- **Label** — Short description (display only).
- **Hashtags** — Within-text categories (e.g. `#principle` inside a fact).

- **Add/Update**: Pass `type` in the request body (REST) or tool input (MCP). Empty means unspecified.
- **List / Search / Context / Relevant**: Pass optional `type` to filter (e.g. only facts, only stories). Empty = no filter.

Suggested type values: `story`, `fact`, `artifact`, `note`, `decision`. The backend does not enforce an enum.

### Project Scopes (optional)

Memories can be associated with specific **project scopes** to organize memories across multiple projects:

- **Global memories** (no scopes) — Match all queries, visible everywhere
- **Scoped memories** — Only match queries with matching scope

**Use cases:**
- Project-specific architecture decisions
- Codebase conventions that only apply to one project
- Status updates for specific projects

**How it works:**
- When creating/updating: Pass `scopes` array (e.g. `["project1", "shared-lib"]`)
- When searching/listing: Pass `scope` filter to only see matching memories
- Global memories (empty `scopes`) always appear in results

When querying with a scope, you'll get:
- All global memories (no scopes)
- All memories that include the query scope in their scopes array

### Memory Tagging Convention (optional)

Start memories with hashtags for categorization:

```
#self         — Identity and capabilities
#goal         — Current objectives
#relationship — People and their preferences  
#status       — Project/system state
#principle    — Learned guidelines
#thought      — Ideas and observations
```

The `memory_context` tool retrieves the most recent memories per category at session start (default: 2 per category, configurable via `per_category` parameter). Only these 6 tags are recognized (text must **start** with the tag). You can use other hashtags (e.g. `#decision`, `#pattern`) freely in text — they work with `memory_search` but won't appear in `memory_context`.

### Access Tracking

Memories automatically track access patterns:
- **Last Accessed** — Timestamp of most recent retrieval via `memory_get`
- **Access Count** — Number of times the memory has been retrieved

This data enables importance-based prioritization and helps identify frequently-used memories. Access tracking is persisted to the database and survives restarts.

### Memory Consolidation

Prevent memory bloat by consolidating redundant or highly similar memories:

**Find Candidates** — Discover pairs of similar memories:
```bash
# Via REST API
curl "http://localhost:9980/v1/consolidation/candidates?min_similarity=0.9"

# Via MCP
memory_consolidation_candidates(min_similarity=0.9, type="fact")
```

**Manual Consolidation** — Merge specific memories:
```bash
# Via REST API
curl -X POST http://localhost:9980/v1/consolidation/merge \
  -H "Content-Type: application/json" \
  -d '{"ids": ["m1", "m2"], "new_label": "Combined fact", "delete_sources": true}'

# Via MCP
memory_consolidate(ids=["m1", "m2"], delete_sources=true)
```

**Auto-Consolidation** — Automatically merge highly similar memories (default threshold: 0.95):
```bash
# Preview what would be consolidated (dry run)
curl -X POST http://localhost:9980/v1/consolidation/auto \
  -H "Content-Type: application/json" \
  -d '{"min_similarity": 0.95, "dry_run": true, "max_consolidations": 10}'

# Actually consolidate
curl -X POST http://localhost:9980/v1/consolidation/auto \
  -H "Content-Type: application/json" \
  -d '{"min_similarity": 0.95, "type_filter": "fact"}'

# Via MCP
memory_auto_consolidate(min_similarity=0.95, dry_run=false)
```

Auto-consolidation helps maintain a clean memory store by automatically merging near-duplicate memories, especially useful for AI agents that may create similar memories over time.

### Maintenance

**Rebuild Edges** — Rebuild similarity edges between all memories (useful after changing delta threshold):
```bash
# Via REST API
curl -X POST http://localhost:9980/v1/rebuild-edges \
  -H "Content-Type: application/json" \
  -d '{"force_rebuild": false, "min_similarity": 0.35}'

# Via MCP
memory_rebuild_edges(force_rebuild=false, min_similarity=0.35)
```

**Periodic Auto-Consolidation** — Enable automatic periodic consolidation:
```bash
# Set environment variable before starting
export LLMEM_AUTO_CONSOLIDATE_INTERVAL=24h
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `memory_add` | Store a new memory |
| `memory_get` | Retrieve by ID with neighbors (tracks access) |
| `memory_search` | Search by content similarity |
| `memory_relevant` | Find memories relevant to a user message |
| `memory_context` | Get priority memories for session restore |
| `memory_update` | Modify existing memory |
| `memory_delete` | Remove a memory |
| `memory_list` | List all memories (lightweight) |
| `memory_stats` | Aggregate statistics |
| `memory_export` | Export all for backup |
| `memory_import` | Import from backup |
| `memory_consolidation_candidates` | List candidate pairs for consolidation |
| `memory_consolidate` | Manually merge multiple memories |
| `memory_auto_consolidate` | Automatically consolidate highly similar memories |
| `memory_rebuild_edges` | Rebuild similarity edges between all memories |


**MCP Client Configuration**

```json
{
  "mcpServers": {
    "llmem": {
      "url": "http://localhost:9980/mcp"
    }
  }
}
```

## REST API

| Method | Path                        | Description | Example                                                                                                                                   |
|--------|-----------------------------|-------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| GET | `/v1/health`                | Health check | `curl localhost:9980/v1/health`                                                                                                           |
| GET | `/v1/stats`                 | Memory store statistics | `curl localhost:9980/v1/stats`                                                                                                            |
| POST | `/v1/memories`              | Create a memory | `curl -X POST localhost:9980/v1/memories -H 'Content-Type: application/json' -d '{"text": "...", "type": "fact", "label": "short title"}'` |
| GET | `/v1/memories/:id`          | Get memory by ID | `curl localhost:9980/v1/memories/m1`                                                                                                      |
| PUT | `/v1/memories/:id`          | Update memory | `curl -X PUT localhost:9980/v1/memories/m1 -H 'Content-Type: application/json' -d '{"text": "new text"}'`                                 |
| DELETE | `/v1/memories/:id`          | Delete memory | `curl -X DELETE localhost:9980/v1/memories/m1`                                                                                            |
| GET | `/v1/memories/list`         | List all memories | `curl 'localhost:9980/v1/memories/list?type=fact&scope=proj1'`                                                                            |
| GET | `/v1/memories/context`      | Priority memories for session restore | `curl 'localhost:9980/v1/memories/context?type=fact&scope=proj1&per_category=3'` — add `&format=prompt` for plain-text (cron-friendly) |
| GET | `/v1/memories/relevant`     | Find memories relevant to a message | `curl 'localhost:9980/v1/memories/relevant?message=how+does+auth+work&limit=5'`                                                           |
| GET | `/v1/memories/search`       | Search by content similarity | `curl 'localhost:9980/v1/memories/search?q=database&type=decision'`                                                                       |
| GET | `/v1/export`                | Export all memories | `curl localhost:9980/v1/export`                                                                                                           |
| POST | `/v1/import`                | Import memories | `curl -X POST localhost:9980/v1/import -H 'Content-Type: application/json' -d '{"memories": [{"text": "..."}]}'`                          |
| POST | `/v1/rebuild-edges`         | Rebuild similarity edges | `curl -X POST localhost:9980/v1/rebuild-edges -H 'Content-Type: application/json' -d '{"force_rebuild": true}'`                           |
| GET | `/v1/consolidation/candidates` | Find consolidation candidates | `curl 'localhost:9980/v1/consolidation/candidates?min_similarity=0.9&type=fact'`                                                          |
| POST | `/v1/consolidation/merge`   | Merge specific memories | `curl -X POST localhost:9980/v1/consolidation/merge -H 'Content-Type: application/json' -d '{"ids": ["m1","m2"]}'`                        |
| POST | `/v1/consolidation/auto`    | Auto-consolidate similar memories | `curl -X POST localhost:9980/v1/consolidation/auto -H 'Content-Type: application/json' -d '{"min_similarity": 0.95, "dry_run": true}'`    |

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `LLMEM_PORT` | `9980` | HTTP server port |
| `LLMEM_DB` | `~/.llmem/data.db` | SQLite database path |
| `LLMEM_AUTO_CONSOLIDATE_INTERVAL` | — | Enable periodic auto-consolidation (e.g. `1h`, `24h`) |
| `TGPT_BIN` | — | Path to tgpt binary; enables AI summarization for oversized texts (default: plain truncation) |
| `TGPT_PROVIDER` | — | LLM provider for tgpt |
| `TGPT_MODEL` | — | Model name |
| `TGPT_KEY` | — | API key |
| `TGPT_URL` | — | Custom API URL |

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    HTTP Server                      │
│                   (Gin, :9980)                      │
├─────────────────────┬───────────────────────────────┤
│     REST API        │         MCP Server            │
│      /v1/*          │          /mcp                 │
├─────────────────────┴───────────────────────────────┤
│                   MemoryStore                       │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐    │
│  │   BM25      │ │   Token     │ │    Edge     │    │
│  │  Vectors    │ │   Index     │ │   Graph     │    │
│  └─────────────┘ └─────────────┘ └─────────────┘    │
│  ┌─────────────┐ ┌─────────────┐                    │
│  │   Access    │ │  Auto       │                    │
│  │  Tracking   │ │ Consolidate │                    │
│  └─────────────┘ └─────────────┘                    │
├─────────────────────────────────────────────────────┤
│                  SQLite Storage                     │
│               (~/.llmem/data.db)                    │
└─────────────────────────────────────────────────────┘
```

## Development

```bash
make build         # Build binary
make test          # Run tests
make test-race     # Run tests with race detector
make run           # Build and run
make run-bg        # Build and run in background
make stop          # Stop background process
make logs          # Tail background logs
```

## License

MIT
