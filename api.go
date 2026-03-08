package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type createMemoryRequest struct {
	Text   string   `json:"text"`
	Label  string   `json:"label,omitempty"`
	Type   string   `json:"type,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

type createMemoryResponse struct {
	Chunk   MemoryChunk     `json:"chunk"`
	Related []RelatedMemory `json:"related"`
}

type getByIDResponse struct {
	Chunk     MemoryChunk     `json:"chunk"`
	Neighbors []RelatedMemory `json:"neighbors"`
}

type deleteResponse struct {
	Deleted MemoryChunk `json:"deleted"`
}

type updateMemoryRequest struct {
	Text   string   `json:"text"`
	Label  string   `json:"label,omitempty"`
	Type   string   `json:"type,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

type updateMemoryResponse struct {
	Chunk   MemoryChunk     `json:"chunk"`
	Related []RelatedMemory `json:"related"`
}

type exportResponse struct {
	Memories []ExportChunk `json:"memories"`
}

type importRequest struct {
	Memories     []ImportChunk `json:"memories"`
	SkipExisting bool          `json:"skipExisting,omitempty"`
}

type searchByLabelResponse struct {
	Matches []MemoryMatch `json:"matches"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type consolidationCandidatesResponse struct {
	Pairs []ConsolidationPair `json:"pairs"`
}

type mergeMemoriesRequest struct {
	IDs           []string `json:"ids"`
	NewLabel      string   `json:"new_label,omitempty"`
	NewType       string   `json:"new_type,omitempty"`
	DeleteSources *bool    `json:"delete_sources,omitempty"`
	TextJoiner    string   `json:"text_joiner,omitempty"`
}

type mergeMemoriesResponse struct {
	Merged     MemoryChunk `json:"merged"`
	RemovedIDs []string    `json:"removed_ids"`
}

type Api struct {
	port          int
	store         *MemoryStore
	engine        *gin.Engine
	srv           *http.Server
	baseCtxCancel context.CancelFunc
}

func NewAPI(store *MemoryStore) *Api {
	gin.SetMode(gin.ReleaseMode)

	port, err := strconv.Atoi(envOr("LLMEM_PORT", "9980"))
	if err != nil {
		log.Fatal(err)
	}

	api := &Api{
		port:   port,
		store:  store,
		engine: gin.New(),
	}

	api.engine.Use(gin.Recovery())

	// Use CORS middleware.
	api.engine.Use(api.corsMiddleware)

	v1 := api.engine.Group("/v1")
	v1.GET("/health", api.handleGetHealth)
	v1.GET("/stats", api.handleGetStats)
	v1.GET("/export", api.handleExport)
	v1.POST("/import", api.handleImport)
	v1.POST("/rebuild-edges", api.handleRebuildEdges)

	v1.POST("/memories", api.handleCreateMemory)
	v1.GET("/memories/:id", api.handleGetMemoryByID)
	v1.PUT("/memories/:id", api.handleUpdateMemory)
	v1.DELETE("/memories/:id", api.handleDeleteMemory)
	v1.GET("/memories/list", api.handleListMemories)
	v1.GET("/memories/context", api.handleGetContextMemories)
	v1.GET("/memories/relevant", api.handleFindRelevantMemories)
	v1.GET("/memories/search", api.handleSearchMemories)

	v1.GET("/consolidation/candidates", api.handleConsolidationCandidates)
	v1.POST("/consolidation/merge", api.handleConsolidationMerge)
	v1.POST("/consolidation/auto", api.handleConsolidationAuto)

	// MCP over Streamable HTTP (same store as REST).
	mcpServer := NewMCPServer(store)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)
	api.engine.Any("/mcp", gin.WrapH(mcpHandler))

	return api
}

func (api *Api) Start() {
	// Create a cancellable context for BaseContext
	ctx, cancel := context.WithCancel(context.Background())
	api.baseCtxCancel = cancel

	api.srv = &http.Server{
		Addr:              fmt.Sprintf(":%d", api.port),
		Handler:           api.engine,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	// Initializing the server in api goroutine so that
	// it won't block the graceful shutdown handling below.
	go api.listenAndServe()
}

func (api *Api) listenAndServe() {
	log.Printf("Starting HTTP server on %s", api.srv.Addr)
	if err := api.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Panicf("Could not listen on %d. Port used? err=%v", api.port, err)
	}
}

// Shutdown gracefully stops the HTTP server, draining in-flight requests.
// Disables keep-alives first so idle connections close immediately.
func (api *Api) Shutdown(ctx context.Context) error {
	if api.srv == nil {
		return nil
	}

	// Cancel the BaseContext first to signal all request handlers
	if api.baseCtxCancel != nil {
		api.baseCtxCancel()
	}

	// Disable keep-alives to close idle connections immediately.
	api.srv.SetKeepAlivesEnabled(false)
	return api.srv.Shutdown(ctx)
}

func (api *Api) handleGetHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (api *Api) handleGetStats(c *gin.Context) {
	c.JSON(http.StatusOK, api.store.Stats())
}

func (api *Api) handleListMemories(c *gin.Context) {
	typeFilter := strings.TrimSpace(c.Query("type"))
	scope := strings.TrimSpace(c.Query("scope"))
	c.JSON(http.StatusOK, gin.H{"memories": api.store.List(typeFilter, scope)})
}

func (api *Api) handleExport(c *gin.Context) {
	c.JSON(http.StatusOK, exportResponse{Memories: api.store.Export()})
}

func (api *Api) handleImport(c *gin.Context) {
	var req importRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid json body"})
		return
	}
	if len(req.Memories) == 0 {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "memories array is required"})
		return
	}
	c.JSON(http.StatusOK, api.store.Import(req.Memories, req.SkipExisting))
}

func (api *Api) handleCreateMemory(c *gin.Context) {
	var req createMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid json body"})
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "text is required"})
		return
	}

	chunk, related, err := api.store.Add(req.Text, strings.TrimSpace(req.Label), strings.TrimSpace(req.Type), req.Scopes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	if related == nil {
		related = []RelatedMemory{}
	}
	c.JSON(http.StatusCreated, createMemoryResponse{Chunk: chunk, Related: related})
}

func (api *Api) handleGetMemoryByID(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "id is required"})
		return
	}

	chunk, neighbors, err := api.store.Get(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, errorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	// Ensure Neighbors is never null in JSON.
	if neighbors == nil {
		neighbors = []RelatedMemory{}
	}

	c.JSON(http.StatusOK, getByIDResponse{Chunk: chunk, Neighbors: neighbors})
}

func (api *Api) handleUpdateMemory(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "id is required"})
		return
	}

	var req updateMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid json body"})
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "text is required"})
		return
	}

	chunk, related, err := api.store.Update(id, req.Text, strings.TrimSpace(req.Label), strings.TrimSpace(req.Type), req.Scopes)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, errorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	if related == nil {
		related = []RelatedMemory{}
	}
	c.JSON(http.StatusOK, updateMemoryResponse{Chunk: chunk, Related: related})
}

func (api *Api) handleDeleteMemory(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "id is required"})
		return
	}

	chunk, err := api.store.Delete(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, errorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, deleteResponse{Deleted: chunk})
}

func (api *Api) handleGetContextMemories(c *gin.Context) {
	typeFilter := strings.TrimSpace(c.Query("type"))
	scope := strings.TrimSpace(c.Query("scope"))
	perCategory, _, err := parseIntParam(c.Query("per_category"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "per_category must be an integer"})
		return
	}
	result := api.store.GetContext(typeFilter, scope, perCategory)
	if strings.ToLower(strings.TrimSpace(c.Query("format"))) == "prompt" {
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(result.FormatAsPrompt()))
		return
	}
	c.JSON(http.StatusOK, result)
}

func (api *Api) handleFindRelevantMemories(c *gin.Context) {
	message := strings.TrimSpace(c.Query("message"))
	if message == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "message query param is required"})
		return
	}
	typeFilter := strings.TrimSpace(c.Query("type"))
	scope := strings.TrimSpace(c.Query("scope"))
	limit, hasLimit, err := parseIntParam(c.Query("limit"))
	if err != nil || (hasLimit && limit < 0) {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "limit must be a non-negative integer"})
		return
	}

	keywords := extractKeywords(message)
	memories := api.store.FindRelevant(message, limit, scope, typeFilter)
	if memories == nil {
		memories = []RelevantMemory{}
	}
	c.JSON(http.StatusOK, gin.H{
		"keywords": keywords,
		"memories": memories,
	})
}

func (api *Api) handleSearchMemories(c *gin.Context) {
	text := strings.TrimSpace(c.Query("q"))
	if text == "" {
		text = strings.TrimSpace(c.Query("text"))
	}
	if text == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "text query param is required"})
		return
	}
	typeFilter := strings.TrimSpace(c.Query("type"))
	scope := strings.TrimSpace(c.Query("scope"))

	matches, err := api.store.Search(text, typeFilter, scope)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	// Ensure Neighbors is never null in JSON.
	for i := range matches {
		if matches[i].Neighbors == nil {
			matches[i].Neighbors = []RelatedMemory{}
		}
	}

	c.JSON(http.StatusOK, searchByLabelResponse{Matches: matches})
}

func (api *Api) handleConsolidationCandidates(c *gin.Context) {
	typeFilter := strings.TrimSpace(c.Query("type"))
	minSim, hasMinSim, err := parseFloatParam(c.Query("min_similarity"))
	if err != nil || (hasMinSim && (minSim < 0 || minSim > 1)) {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "min_similarity must be between 0 and 1"})
		return
	}
	limit, hasLimit, err := parseIntParam(c.Query("limit"))
	if err != nil || (hasLimit && limit < 0) {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "limit must be a non-negative integer"})
		return
	}

	pairs, err := api.store.FindConsolidationPairs(ConsolidationParams{
		MinSimilarity: minSim,
		TypeFilter:    typeFilter,
		Limit:         limit,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	if pairs == nil {
		pairs = []ConsolidationPair{}
	}
	c.JSON(http.StatusOK, consolidationCandidatesResponse{Pairs: pairs})
}

func (api *Api) handleConsolidationMerge(c *gin.Context) {
	var req mergeMemoriesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid json body"})
		return
	}
	if len(req.IDs) < 2 {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "ids must contain at least two items"})
		return
	}

	merged, removed, err := api.store.Consolidate(req.IDs, ConsolidateOptions{
		NewLabel:      strings.TrimSpace(req.NewLabel),
		NewType:       strings.TrimSpace(req.NewType),
		DeleteSources: req.DeleteSources,
		TextJoiner:    req.TextJoiner,
	})
	if err != nil {
		// Treat not found / bad input as 400; other errors as 500.
		if errors.Is(err, ErrNotFound) ||
			errors.Is(err, ErrEmptyID) ||
			errors.Is(err, ErrTooFewIDs) ||
			errors.Is(err, ErrIncompatibleTypes) {
			c.JSON(http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	if removed == nil {
		removed = []string{}
	}

	c.JSON(http.StatusOK, mergeMemoriesResponse{
		Merged:     merged,
		RemovedIDs: removed,
	})
}

// corsMiddleware wraps an MCP Streamable HTTP handler with CORS headers so that
// browsers and tools that send an Origin header (e.g. MCP Inspector, Cursor)
// receive Access-Control-Allow-Origin and do not treat the response as invalid origin.
func (api *Api) corsMiddleware(c *gin.Context) {
	origin := c.Request.Header.Get("Origin")
	// If credentials are allowed, we must not use "*" per CORS spec.
	if origin != "" {
		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		// Ensure caches/proxies vary on Origin.
		c.Writer.Header().Add("Vary", "Origin")
	} else {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	}

	c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, HEAD")
	c.Writer.Header().Set("Access-Control-Allow-Headers", strings.Join([]string{
		"Content-Type", "Content-Length", "Accept-Encoding",
		"Authorization",
		"Accept", "Origin", "Cache-Control", "X-Requested-With",
		// MCP Streamable HTTP headers:
		"Mcp-Protocol-Version", "Mcp-Session-Id", "Last-Event-ID",
	}, ", "))

	if c.Request.Method == http.MethodOptions {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	c.Next()
}

func (api *Api) handleConsolidationAuto(c *gin.Context) {
	var req struct {
		MinSimilarity     float64 `json:"min_similarity,omitempty"`
		TypeFilter        string  `json:"type_filter,omitempty"`
		MaxConsolidations int     `json:"max_consolidations,omitempty"`
		DryRun            bool    `json:"dry_run,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			// If no JSON body, use query params
			var hasMinSim bool
			req.MinSimilarity, hasMinSim, err = parseFloatParam(c.Query("min_similarity"))
			if err != nil || (hasMinSim && (req.MinSimilarity < 0 || req.MinSimilarity > 1)) {
				c.JSON(http.StatusBadRequest, errorResponse{Error: "min_similarity must be between 0 and 1"})
				return
			}
			req.TypeFilter = strings.TrimSpace(c.Query("type_filter"))
			var hasMax bool
			req.MaxConsolidations, hasMax, err = parseIntParam(c.Query("max_consolidations"))
			if err != nil || (hasMax && req.MaxConsolidations < 0) {
				c.JSON(http.StatusBadRequest, errorResponse{Error: "max_consolidations must be a non-negative integer"})
				return
			}
			var hasDryRun bool
			req.DryRun, hasDryRun, err = parseBoolParam(c.Query("dry_run"))
			if err != nil && hasDryRun {
				c.JSON(http.StatusBadRequest, errorResponse{Error: "dry_run must be a boolean"})
				return
			}
		} else {
			c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid json body"})
			return
		}
	}

	result, err := api.store.AutoConsolidate(AutoConsolidateOptions{
		MinSimilarity:     req.MinSimilarity,
		TypeFilter:        req.TypeFilter,
		MaxConsolidations: req.MaxConsolidations,
		DryRun:            req.DryRun,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (api *Api) handleRebuildEdges(c *gin.Context) {
	var req struct {
		ForceRebuild  bool    `json:"force_rebuild,omitempty"`
		MinSimilarity float64 `json:"min_similarity,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			// If no JSON body, use query params
			var hasForce bool
			req.ForceRebuild, hasForce, err = parseBoolParam(c.Query("force_rebuild"))
			if err != nil && hasForce {
				c.JSON(http.StatusBadRequest, errorResponse{Error: "force_rebuild must be a boolean"})
				return
			}
			var hasMinSim bool
			req.MinSimilarity, hasMinSim, err = parseFloatParam(c.Query("min_similarity"))
			if err != nil || (hasMinSim && (req.MinSimilarity < 0 || req.MinSimilarity > 1)) {
				c.JSON(http.StatusBadRequest, errorResponse{Error: "min_similarity must be between 0 and 1"})
				return
			}
		} else {
			c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid json body"})
			return
		}
	}

	result, err := api.store.RebuildEdges(RebuildEdgesOptions{
		ForceRebuild:  req.ForceRebuild,
		MinSimilarity: req.MinSimilarity,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func parseIntParam(raw string) (int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, true, err
	}
	return v, true, nil
}

func parseFloatParam(raw string) (float64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, true, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, true, errors.New("invalid float")
	}
	return v, true, nil
}

func parseBoolParam(raw string) (bool, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, true, err
	}
	return v, true, nil
}
