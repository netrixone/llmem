package main

import (
	"context"
	_ "embed"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed VERSION
var rawVersion string

// NewMCPServer builds an MCP server that exposes the memory store as tools.
// The same server instance is used for all Streamable HTTP connections.
func NewMCPServer(store *MemoryStore) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "llmem",
		Version: strings.TrimSpace(rawVersion),
	}, nil)

	// memory_add: store a new memory; returns created chunk and related memories.
	type addIn struct {
		Text   string   `json:"text" jsonschema:"Content to store as a memory"`
		Label  string   `json:"label,omitempty" jsonschema:"Optional short label/title for the memory (display only; search uses content)"`
		Type   string   `json:"type,omitempty" jsonschema:"Optional type: story, fact, artifact, note, decision, etc."`
		Scopes []string `json:"scopes,omitempty" jsonschema:"Optional project scopes. Empty/nil = global (matches all queries). Non-empty = only matches queries with matching scope."`
	}
	type addOut struct {
		Chunk   MemoryChunk     `json:"chunk"`
		Related []RelatedMemory `json:"related"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_add",
		Description: "Store a new memory. Returns the created chunk and any related (similar) memories. Optional label is for display; search is by content only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, addOut, error) {
		chunk, related, err := store.Add(in.Text, in.Label, in.Type, in.Scopes)
		if err != nil {
			return nil, addOut{}, err
		}
		return nil, addOut{Chunk: chunk, Related: related}, nil
	})

	// memory_get: fetch a memory by ID and its neighbors.
	type getIn struct {
		Id string `json:"id" jsonschema:"Memory chunk ID (e.g. m1, m2)"`
	}
	type getOut struct {
		Chunk     MemoryChunk     `json:"chunk"`
		Neighbors []RelatedMemory `json:"neighbors"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_get",
		Description: "Get a memory by ID. Returns the chunk and its similar (neighbor) memories.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, getOut, error) {
		chunk, neighbors, err := store.Get(in.Id)
		if err != nil {
			return nil, getOut{}, err
		}
		return nil, getOut{Chunk: chunk, Neighbors: neighbors}, nil
	})

	// memory_search: search memories by text; returns matches with similarity and neighbors.
	type searchIn struct {
		Query string `json:"query" jsonschema:"Search text to find similar memories"`
		Type  string `json:"type,omitempty" jsonschema:"Optional filter: only search within memories of this type"`
		Scope string `json:"scope,omitempty" jsonschema:"Optional filter: only return memories matching this scope (or global memories)"`
	}
	type searchOut struct {
		Matches []MemoryMatch `json:"matches"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_search",
		Description: "Search memories by content (including hashtags). Optional type filter. Returns matching chunks with similarity scores and neighbors.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		matches, err := store.Search(in.Query, in.Type, in.Scope)
		if err != nil {
			return nil, searchOut{}, err
		}
		return nil, searchOut{Matches: matches}, nil
	})

	// memory_delete: remove a memory by ID.
	type deleteIn struct {
		Id string `json:"id" jsonschema:"Memory chunk ID to delete (e.g. m1, m2)"`
	}
	type deleteOut struct {
		Deleted MemoryChunk `json:"deleted"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_delete",
		Description: "Delete a memory by ID. Returns the deleted chunk.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteIn) (*mcp.CallToolResult, deleteOut, error) {
		chunk, err := store.Delete(in.Id)
		if err != nil {
			return nil, deleteOut{}, err
		}
		return nil, deleteOut{Deleted: chunk}, nil
	})

	// memory_update: modify an existing memory's text and optional label.
	type updateIn struct {
		Id     string   `json:"id" jsonschema:"Memory chunk ID to update (e.g. m1, m2)"`
		Text   string   `json:"text" jsonschema:"New content for the memory"`
		Label  string   `json:"label,omitempty" jsonschema:"Optional short label/title for the memory (display only; search uses content). If empty, keeps existing label."`
		Type   string   `json:"type,omitempty" jsonschema:"Optional type: story, fact, artifact, note, decision, etc. (empty keeps existing)"`
		Scopes []string `json:"scopes,omitempty" jsonschema:"Optional project scopes. If provided (even empty), updates scopes. If nil, keeps existing scopes."`
	}
	type updateOut struct {
		Chunk   MemoryChunk     `json:"chunk"`
		Related []RelatedMemory `json:"related"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_update",
		Description: "Update an existing memory's text and optional label. Re-calculates similarity edges from content. Returns updated chunk and new related memories. Optional label is for display; search is by content only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, updateOut, error) {
		chunk, related, err := store.Update(in.Id, in.Text, in.Label, in.Type, in.Scopes)
		if err != nil {
			return nil, updateOut{}, err
		}
		return nil, updateOut{Chunk: chunk, Related: related}, nil
	})

	// memory_stats: get aggregate statistics about the memory store.
	type statsIn struct{}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_stats",
		Description: "Get statistics about the memory store: total count, edges, oldest/newest, most connected memories.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in statsIn) (*mcp.CallToolResult, MemoryStats, error) {
		return nil, store.Stats(), nil
	})

	// memory_list: list all memories (lightweight, no full text).
	type listIn struct {
		Type  string `json:"type,omitempty" jsonschema:"Optional filter: only list memories of this type (e.g. fact, story)"`
		Scope string `json:"scope,omitempty" jsonschema:"Optional filter: only return memories matching this scope (or global memories)"`
	}
	type listOut struct {
		Memories []MemoryListItem `json:"memories"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_list",
		Description: "List all memories with IDs, labels, types, and edge counts. Optional type filter. Sorted by newest first. Lightweight alternative to export.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, listOut, error) {
		return nil, listOut{Memories: store.List(in.Type, in.Scope)}, nil
	})

	// memory_context: get important memories for context restoration.
	type contextIn struct {
		Type        string `json:"type,omitempty" jsonschema:"Optional filter: only include memories of this type"`
		Scope       string `json:"scope,omitempty" jsonschema:"Optional filter: only return memories matching this scope (or global memories)"`
		PerCategory int    `json:"per_category,omitempty" jsonschema:"Max memories per category (default 2). Increase to see more principles/thoughts."`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_context",
		Description: "Get important memories for context restoration. Returns memories tagged with #self, #goal, #relationship, #status, #principle, #thought - up to N per category (default 2), newest first. Optional type filter. Use at session start.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in contextIn) (*mcp.CallToolResult, ContextResult, error) {
		return nil, store.GetContext(in.Type, in.Scope, in.PerCategory), nil
	})

	// memory_export: export all memories for backup.
	type exportIn struct{}
	type exportOut struct {
		Memories []ExportChunk `json:"memories"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_export",
		Description: "Export all memories with their edges for backup. Returns array of all memory chunks.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in exportIn) (*mcp.CallToolResult, exportOut, error) {
		return nil, exportOut{Memories: store.Export()}, nil
	})

	// memory_import: import memories from backup.
	type importIn struct {
		Memories     []ImportChunk `json:"memories" jsonschema:"Array of memories to import, each with text field"`
		SkipExisting bool          `json:"skipExisting,omitempty" jsonschema:"If true, skip memories with IDs that already exist"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_import",
		Description: "Import memories from a backup or list. Returns import results with success/failure counts. Preserves original IDs and timestamps when provided (for lossless export/import round-trips).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in importIn) (*mcp.CallToolResult, ImportResults, error) {
		return nil, store.Import(in.Memories, in.SkipExisting), nil
	})

	// memory_relevant: find memories relevant to a user message.
	type relevantIn struct {
		Message string `json:"message" jsonschema:"User message to find relevant memories for"`
		Limit   int    `json:"limit,omitempty" jsonschema:"Maximum results to return (default 5)"`
		Scope   string `json:"scope,omitempty" jsonschema:"Optional filter: only return memories matching this scope (or global memories)"`
		Type    string `json:"type,omitempty" jsonschema:"Optional filter: only return memories with this type (e.g. fact, status)"`
	}
	type relevantOut struct {
		Keywords []string         `json:"keywords"`
		Memories []RelevantMemory `json:"memories"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_relevant",
		Description: "Find memories relevant to a user message. Extracts keywords from conversational text and searches with lower threshold. Use mid-conversation when user mentions topics that might have stored context.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in relevantIn) (*mcp.CallToolResult, relevantOut, error) {
		keywords := extractKeywords(in.Message)
		memories := store.FindRelevant(in.Message, in.Limit, in.Scope, in.Type)
		return nil, relevantOut{Keywords: keywords, Memories: memories}, nil
	})

	// memory_consolidation_candidates: list candidate memory pairs for consolidation.
	type consolidationCandidatesIn struct {
		MinSimilarity float64 `json:"min_similarity,omitempty" jsonschema:"Minimum similarity threshold (default 0.9). Only pairs with edge similarity >= this are returned."`
		Type          string  `json:"type,omitempty" jsonschema:"Optional filter: only consider pairs where both memories have this type (e.g. fact, status)."`
		Limit         int     `json:"limit,omitempty" jsonschema:"Maximum number of pairs to return (0 means no limit)."`
	}
	type consolidationCandidatesOut struct {
		Pairs []ConsolidationPair `json:"pairs"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_consolidation_candidates",
		Description: "List candidate memory pairs that are similar enough to consider consolidating. Uses existing similarity edges; read-only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in consolidationCandidatesIn) (*mcp.CallToolResult, consolidationCandidatesOut, error) {
		pairs, err := store.FindConsolidationPairs(ConsolidationParams{
			MinSimilarity: in.MinSimilarity,
			TypeFilter:    in.Type,
			Limit:         in.Limit,
		})
		if err != nil {
			return nil, consolidationCandidatesOut{}, err
		}
		return nil, consolidationCandidatesOut{Pairs: pairs}, nil
	})

	// memory_consolidate: merge multiple memories into one and (optionally) delete the sources.
	type consolidateIn struct {
		IDs           []string `json:"ids" jsonschema:"Memory IDs to consolidate; must contain at least two distinct IDs."`
		NewLabel      string   `json:"new_label,omitempty" jsonschema:"Optional label for the merged memory; if empty, derived from merged text."`
		NewType       string   `json:"new_type,omitempty" jsonschema:"Optional type for the merged memory; if empty, inferred from sources (must be compatible)."`
		DeleteSources *bool    `json:"delete_sources,omitempty" jsonschema:"If true (default), delete source memories after successful merge."`
		TextJoiner    string   `json:"text_joiner,omitempty" jsonschema:"Optional separator inserted between merged texts (default is a clear multi-line separator)."`
	}
	type consolidateOut struct {
		Merged     MemoryChunk `json:"merged"`
		RemovedIDs []string    `json:"removed_ids"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_consolidate",
		Description: "Merge multiple memories into a new one and optionally delete the sources. Useful for cleaning up redundant facts or statuses. You must decide which IDs to merge.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in consolidateIn) (*mcp.CallToolResult, consolidateOut, error) {
		merged, removed, err := store.Consolidate(in.IDs, ConsolidateOptions{
			NewLabel:      in.NewLabel,
			NewType:       in.NewType,
			DeleteSources: in.DeleteSources,
			TextJoiner:    in.TextJoiner,
		})
		if err != nil {
			return nil, consolidateOut{}, err
		}
		return nil, consolidateOut{
			Merged:     merged,
			RemovedIDs: removed,
		}, nil
	})

	// memory_auto_consolidate: automatically consolidate highly similar memories.
	type autoConsolidateIn struct {
		MinSimilarity     float64 `json:"min_similarity,omitempty" jsonschema:"Minimum similarity threshold (default 0.95). Only pairs with edge similarity >= this are consolidated."`
		TypeFilter        string  `json:"type_filter,omitempty" jsonschema:"Optional filter: only consolidate memories of this type."`
		MaxConsolidations int     `json:"max_consolidations,omitempty" jsonschema:"Maximum number of pairs to consolidate (0 means no limit)."`
		DryRun            bool    `json:"dry_run,omitempty" jsonschema:"If true, only reports what would be consolidated without making changes."`
	}
	type autoConsolidateOut struct {
		Consolidated int      `json:"consolidated"`
		Removed      []string `json:"removed"`
		Merged       []string `json:"merged"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_auto_consolidate",
		Description: "Automatically consolidate highly similar memories to prevent memory bloat. Merges redundant or near-duplicate memories. Use dry_run=true to preview changes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in autoConsolidateIn) (*mcp.CallToolResult, autoConsolidateOut, error) {
		result, err := store.AutoConsolidate(AutoConsolidateOptions{
			MinSimilarity:     in.MinSimilarity,
			TypeFilter:        in.TypeFilter,
			MaxConsolidations: in.MaxConsolidations,
			DryRun:            in.DryRun,
		})
		if err != nil {
			return nil, autoConsolidateOut{}, err
		}
		return nil, autoConsolidateOut{
			Consolidated: result.Consolidated,
			Removed:      result.Removed,
			Merged:       result.Merged,
		}, nil
	})

	// memory_rebuild_edges: rebuild similarity edges between all memories.
	type rebuildEdgesIn struct {
		ForceRebuild  bool    `json:"force_rebuild,omitempty" jsonschema:"If true, recalculates all edges even if they already exist. If false, only rebuilds missing edges. Stale edges (below threshold) are always removed regardless of this flag."`
		MinSimilarity float64 `json:"min_similarity,omitempty" jsonschema:"Minimum similarity threshold for creating edges. If zero, uses the store's default delta."`
	}
	type rebuildEdgesOut struct {
		EdgesCreated    int      `json:"edgesCreated"`
		EdgesUpdated    int      `json:"edgesUpdated"`
		EdgesRemoved    int      `json:"edgesRemoved"`
		ChunksProcessed int      `json:"chunksProcessed"`
		Errors          []string `json:"errors,omitempty"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "memory_rebuild_edges",
		Description: "Rebuild similarity edges between all memories. Useful for maintenance after changing the similarity delta or fixing edge corruption.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in rebuildEdgesIn) (*mcp.CallToolResult, rebuildEdgesOut, error) {
		result, err := store.RebuildEdges(RebuildEdgesOptions{
			ForceRebuild:  in.ForceRebuild,
			MinSimilarity: in.MinSimilarity,
		})
		if err != nil {
			return nil, rebuildEdgesOut{}, err
		}
		return nil, rebuildEdgesOut{
			EdgesCreated:    result.EdgesCreated,
			EdgesUpdated:    result.EdgesUpdated,
			EdgesRemoved:    result.EdgesRemoved,
			ChunksProcessed: result.ChunksProcessed,
			Errors:          result.Errors,
		}, nil
	})

	return s
}
