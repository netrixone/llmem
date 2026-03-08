---
name: llmem-usage
description: Guide for using llmem MCP tools for persistent memory across sessions. Use when memory_add, memory_get, memory_search, memory_context, memory_relevant, memory_update, memory_list, memory_stats, memory_consolidate, or any memory_ tool is available.
---

# Using llmem Tools

Your context resets every session. These tools are your only continuity between conversations.

## Session Lifecycle

### 1. Start — Restore Context
Call `memory_context` immediately. Returns your most important memories by hashtag category
(#self, #goal, #relationship, #status, #principle, #thought) — up to 2 per category by default, newest first.
Use `per_category` to increase (e.g. 3) when you want more principles or thoughts.

### 2. Mid-Conversation — Recall
Call `memory_relevant` when the user mentions a topic that might have stored context.
It extracts keywords and searches with a permissive threshold. Use reactively, not on every message.

### 3. Milestones — Store
Use `memory_add` for new knowledge. Use `memory_update` to refresh existing memories (especially #status).

## Hashtag Prefixes

`memory_context` finds memories whose text **starts with** these hashtags:

| Tag | Purpose |
|-----|---------|
| `#self` | Your identity and continuity |
| `#goal` | Objectives, operating protocols |
| `#relationship` | Key people, preferences, trust |
| `#status` | Project state, latest commits, pending work |
| `#principle` | Hard-won lessons and guiding rules |
| `#thought` | Reflections and observations |

Write `#status Project is clean`, not `Project is clean #status` — prefix matching only.

## Memory Types

Set `type` on every memory for filtering in list/search/context/relevant:

- `fact` — Technical knowledge, architecture, patterns, gotchas
- `decision` — Why something was chosen over alternatives
- `principle` — Guiding rules learned from experience
- `status` — Current project state (update in place, don't duplicate)
- `thought` — Observations, reflections worth preserving
- `note` — Relationship context, protocols, preferences
- `story` / `artifact` — Narratives, generated artifacts

## Tool Quick Reference

| Tool | When |
|------|------|
| `memory_context` | Session start |
| `memory_relevant` | User mentions topic that might have context |
| `memory_search` | Need specific memories by content or hashtag |
| `memory_add` | Store new knowledge, observations, decisions |
| `memory_update` | Refresh existing memory (preserves label/type if omitted) |
| `memory_delete` | Remove obsolete or incorrect memories |
| `memory_get` | Retrieve specific memory by ID |
| `memory_list` | Browse all memories (lightweight: IDs, labels, types) |
| `memory_stats` | Check totals, oldest/newest |
| `memory_consolidate` | Merge 2+ redundant memories into one |
| `memory_auto_consolidate` | Auto-merge near-duplicates (`dry_run: true` to preview) |
| `memory_consolidation_candidates` | List similar pairs worth merging (read-only preview) |
| `memory_rebuild_edges` | Rebuild similarity edges (maintenance after threshold changes) |
| `memory_export` / `memory_import` | Backup and restore |

## Best Practices

1. **Update, don't duplicate.** One living `#status` memory beats ten stale ones.
2. **Use types consistently.** Always set the `type` field.
3. **Labels are display-only.** Search uses content only. Omit label for auto-generation.
4. **Scopes for multi-project.** Set `scopes` to isolate project-specific memories. No scopes = global.
5. **Store freely.** The system handles thousands of memories. Don't overthink what's "worth" storing.
6. **Don't store source code.** Store architecture, decisions, gotchas — things that require understanding.
7. **Consolidate periodically.** Use `memory_list` to spot duplicates, `memory_consolidate` to merge.
