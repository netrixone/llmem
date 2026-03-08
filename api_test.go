package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupTestRouter creates a gin router with API routes backed by a test MemoryStore.
// Returns the router and the underlying store so tests can seed data.
func setupTestRouter(t *testing.T, opts ...MemoryStoreOptions) (*gin.Engine, *MemoryStore) {
	t.Helper()

	var storeOpts MemoryStoreOptions
	if len(opts) > 0 {
		storeOpts = opts[0]
	}
	store := mustNewMemoryStore(t, storeOpts)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := &Api{store: store, engine: engine}

	v1 := engine.Group("/v1")
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

	return engine, store
}

func doJSON(t *testing.T, router *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func doGet(t *testing.T, router *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	return doJSON(t, router, http.MethodGet, path, nil)
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decode body: %v (body=%q)", err, w.Body.String())
	}
}

// --- Health & Stats ---

func TestAPI_Health(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doGet(t, router, "/v1/health")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]bool
	decodeBody(t, w, &resp)
	if !resp["ok"] {
		t.Fatal("expected ok=true")
	}
}

func TestAPI_Stats_Empty(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doGet(t, router, "/v1/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp MemoryStats
	decodeBody(t, w, &resp)
	if resp.TotalMemories != 0 {
		t.Fatalf("expected 0 memories, got %d", resp.TotalMemories)
	}
}

// --- Create / Get / Update / Delete ---

func TestAPI_CRUD(t *testing.T) {
	router, _ := setupTestRouter(t)

	// Create
	w := doJSON(t, router, http.MethodPost, "/v1/memories", createMemoryRequest{
		Text:  "test memory about Go programming",
		Label: "Go note",
		Type:  "fact",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createResp createMemoryResponse
	decodeBody(t, w, &createResp)
	if createResp.Chunk.ID == "" {
		t.Fatal("create: empty ID")
	}
	if createResp.Chunk.Text != "test memory about Go programming" {
		t.Fatalf("create: unexpected text %q", createResp.Chunk.Text)
	}
	id := createResp.Chunk.ID

	// Get
	w = doGet(t, router, "/v1/memories/"+id)
	if w.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", w.Code)
	}
	var getResp getByIDResponse
	decodeBody(t, w, &getResp)
	if getResp.Chunk.ID != id {
		t.Fatalf("get: expected ID %s, got %s", id, getResp.Chunk.ID)
	}
	if getResp.Chunk.Label != "Go note" {
		t.Fatalf("get: expected label %q, got %q", "Go note", getResp.Chunk.Label)
	}

	// Update
	w = doJSON(t, router, http.MethodPut, "/v1/memories/"+id, updateMemoryRequest{
		Text:  "updated memory about Rust programming",
		Label: "Rust note",
		Type:  "decision",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updateResp updateMemoryResponse
	decodeBody(t, w, &updateResp)
	if updateResp.Chunk.Text != "updated memory about Rust programming" {
		t.Fatalf("update: unexpected text %q", updateResp.Chunk.Text)
	}
	if updateResp.Chunk.Type != "decision" {
		t.Fatalf("update: expected type %q, got %q", "decision", updateResp.Chunk.Type)
	}

	// Delete
	w = doJSON(t, router, http.MethodDelete, "/v1/memories/"+id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var deleteResp deleteResponse
	decodeBody(t, w, &deleteResp)
	if deleteResp.Deleted.ID != id {
		t.Fatalf("delete: expected ID %s, got %s", id, deleteResp.Deleted.ID)
	}

	// Get after delete → 404
	w = doGet(t, router, "/v1/memories/"+id)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get-after-delete: expected 404, got %d", w.Code)
	}
}

func TestAPI_Create_EmptyText(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doJSON(t, router, http.MethodPost, "/v1/memories", createMemoryRequest{Text: "  "})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPI_Create_InvalidJSON(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPI_Get_NotFound(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doGet(t, router, "/v1/memories/m999")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAPI_Update_HappyPath(t *testing.T) {
	router, store := setupTestRouter(t)
	c, _, _ := store.Add("original text", "orig label", "fact", nil)

	w := doJSON(t, router, http.MethodPut, "/v1/memories/"+c.ID, updateMemoryRequest{
		Text:  "updated text",
		Label: "new label",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp updateMemoryResponse
	decodeBody(t, w, &resp)
	if resp.Chunk.Text != "updated text" {
		t.Errorf("text not updated: got %q", resp.Chunk.Text)
	}
	if resp.Chunk.Label != "new label" {
		t.Errorf("label not updated: got %q", resp.Chunk.Label)
	}
	if resp.Chunk.UpdatedAt == nil {
		t.Error("UpdatedAt should be set after update")
	}
}

func TestAPI_Update_NotFound(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doJSON(t, router, http.MethodPut, "/v1/memories/m999", updateMemoryRequest{Text: "hello"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAPI_Update_InvalidJSON(t *testing.T) {
	router, store := setupTestRouter(t)
	c, _, _ := store.Add("text", "", "", nil)

	req := httptest.NewRequest(http.MethodPut, "/v1/memories/"+c.ID,
		strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPI_Update_EmptyText(t *testing.T) {
	router, store := setupTestRouter(t)
	c, _, _ := store.Add("text", "", "", nil)

	w := doJSON(t, router, http.MethodPut, "/v1/memories/"+c.ID, updateMemoryRequest{Text: ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty text, got %d", w.Code)
	}
}

func TestAPI_Delete_NotFound(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doJSON(t, router, http.MethodDelete, "/v1/memories/m999", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- List / Export / Import ---

func TestAPI_ListAndExport(t *testing.T) {
	router, store := setupTestRouter(t)
	store.Add("alpha memory", "alpha", "fact", nil)
	store.Add("beta memory", "beta", "note", nil)

	// List
	w := doGet(t, router, "/v1/memories/list")
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var listResp struct {
		Memories []MemoryListItem `json:"memories"`
	}
	decodeBody(t, w, &listResp)
	if len(listResp.Memories) != 2 {
		t.Fatalf("list: expected 2 items, got %d", len(listResp.Memories))
	}

	// List with type filter
	w = doGet(t, router, "/v1/memories/list?type=fact")
	decodeBody(t, w, &listResp)
	if len(listResp.Memories) != 1 {
		t.Fatalf("list with type: expected 1 item, got %d", len(listResp.Memories))
	}

	// Export
	w = doGet(t, router, "/v1/export")
	if w.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d", w.Code)
	}
	var expResp exportResponse
	decodeBody(t, w, &expResp)
	if len(expResp.Memories) != 2 {
		t.Fatalf("export: expected 2, got %d", len(expResp.Memories))
	}
}

func TestAPI_Import(t *testing.T) {
	router, _ := setupTestRouter(t)

	w := doJSON(t, router, http.MethodPost, "/v1/import", importRequest{
		Memories: []ImportChunk{
			{Text: "imported alpha"},
			{Text: "imported beta"},
			{Text: ""},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("import: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp ImportResults
	decodeBody(t, w, &resp)
	if resp.Imported != 2 {
		t.Fatalf("import: expected 2 imported, got %d", resp.Imported)
	}
	if resp.Failed != 1 {
		t.Fatalf("import: expected 1 failed, got %d", resp.Failed)
	}
}

func TestAPI_Import_EmptyArray(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doJSON(t, router, http.MethodPost, "/v1/import", importRequest{Memories: []ImportChunk{}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- Search / Relevant / Context ---

func TestAPI_Search(t *testing.T) {
	router, store := setupTestRouter(t)
	store.Add("#decision Use PostgreSQL for persistence", "", "", nil)
	store.Add("#decision Use Redis for caching", "", "", nil)

	w := doGet(t, router, "/v1/memories/search?q=PostgreSQL")
	if w.Code != http.StatusOK {
		t.Fatalf("search: expected 200, got %d", w.Code)
	}
	var resp searchByLabelResponse
	decodeBody(t, w, &resp)
	if len(resp.Matches) == 0 {
		t.Fatal("search: expected at least one match")
	}
}

func TestAPI_Search_EmptyQuery(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doGet(t, router, "/v1/memories/search")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPI_Relevant(t *testing.T) {
	router, store := setupTestRouter(t)
	store.Add("#status Project in beta testing phase", "", "", nil)

	w := doGet(t, router, "/v1/memories/relevant?message=What+is+the+project+status")
	if w.Code != http.StatusOK {
		t.Fatalf("relevant: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Keywords []string         `json:"keywords"`
		Memories []RelevantMemory `json:"memories"`
	}
	decodeBody(t, w, &resp)
	if len(resp.Keywords) == 0 {
		t.Fatal("relevant: expected keywords extracted")
	}
}

func TestAPI_Relevant_MissingMessage(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doGet(t, router, "/v1/memories/relevant")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPI_Context(t *testing.T) {
	router, store := setupTestRouter(t)
	store.Add("#self I am a test assistant", "", "fact", nil)
	store.Add("#goal Improve the codebase", "", "note", nil)

	w := doGet(t, router, "/v1/memories/context")
	if w.Code != http.StatusOK {
		t.Fatalf("context: expected 200, got %d", w.Code)
	}
	var resp ContextResult
	decodeBody(t, w, &resp)
	if len(resp.Memories) == 0 {
		t.Fatal("context: expected at least one memory")
	}
}

func TestAPI_Context_FormatPrompt(t *testing.T) {
	router, store := setupTestRouter(t)
	store.Add("#self I am a test assistant", "", "fact", nil)
	store.Add("#goal Improve the codebase", "", "note", nil)

	w := doGet(t, router, "/v1/memories/context?format=prompt")
	if w.Code != http.StatusOK {
		t.Fatalf("context format=prompt: expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if body == "" {
		t.Fatal("context format=prompt: expected non-empty body")
	}
	if !strings.Contains(body, "#self") || !strings.Contains(body, "#goal") {
		t.Fatalf("context format=prompt: expected #self and #goal in body, got %q", body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("context format=prompt: expected text/plain, got %q", ct)
	}
}

func TestAPI_Context_PerCategory(t *testing.T) {
	router, store := setupTestRouter(t)
	store.Add("#goal first goal", "", "", nil)
	store.Add("#goal second goal", "", "", nil)
	store.Add("#goal third goal", "", "", nil)

	// Default: 2 per category
	w := doGet(t, router, "/v1/memories/context")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp ContextResult
	decodeBody(t, w, &resp)
	if len(resp.Memories) != 2 {
		t.Fatalf("default: expected 2 memories, got %d", len(resp.Memories))
	}

	// per_category=1
	w = doGet(t, router, "/v1/memories/context?per_category=1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	decodeBody(t, w, &resp)
	if len(resp.Memories) != 1 {
		t.Fatalf("per_category=1: expected 1 memory, got %d", len(resp.Memories))
	}

	// per_category=3
	w = doGet(t, router, "/v1/memories/context?per_category=3")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	decodeBody(t, w, &resp)
	if len(resp.Memories) != 3 {
		t.Fatalf("per_category=3: expected 3 memories, got %d", len(resp.Memories))
	}
}

// --- Consolidation ---

func TestAPI_ConsolidationMerge(t *testing.T) {
	router, store := setupTestRouter(t)
	c1, _, _ := store.Add("memory about databases", "", "fact", nil)
	c2, _, _ := store.Add("memory about schemas", "", "fact", nil)

	w := doJSON(t, router, http.MethodPost, "/v1/consolidation/merge", mergeMemoriesRequest{
		IDs: []string{c1.ID, c2.ID},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("merge: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp mergeMemoriesResponse
	decodeBody(t, w, &resp)
	if resp.Merged.ID == "" {
		t.Fatal("merge: empty merged ID")
	}
	if len(resp.RemovedIDs) != 2 {
		t.Fatalf("merge: expected 2 removed, got %d", len(resp.RemovedIDs))
	}
}

func TestAPI_ConsolidationMerge_TooFewIDs(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doJSON(t, router, http.MethodPost, "/v1/consolidation/merge", mergeMemoriesRequest{
		IDs: []string{"m1"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPI_ConsolidationMerge_InvalidJSON(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/consolidation/merge",
		strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPI_ConsolidationMerge_NotFound(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doJSON(t, router, http.MethodPost, "/v1/consolidation/merge", mergeMemoriesRequest{
		IDs: []string{"m999", "m998"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for not-found IDs, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_ConsolidationMerge_IncompatibleTypes(t *testing.T) {
	router, store := setupTestRouter(t)
	c1, _, _ := store.Add("memory one", "", "fact", nil)
	c2, _, _ := store.Add("memory two", "", "decision", nil)

	w := doJSON(t, router, http.MethodPost, "/v1/consolidation/merge", mergeMemoriesRequest{
		IDs: []string{c1.ID, c2.ID},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for incompatible types, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_ConsolidationCandidates(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doGet(t, router, "/v1/consolidation/candidates")
	if w.Code != http.StatusOK {
		t.Fatalf("candidates: expected 200, got %d", w.Code)
	}
	var resp consolidationCandidatesResponse
	decodeBody(t, w, &resp)
	// No pairs in an empty or no-edge store — just verify the response structure.
	if resp.Pairs == nil {
		t.Fatal("candidates: expected non-nil Pairs slice")
	}
}

// --- Rebuild Edges ---

func TestAPI_RebuildEdges(t *testing.T) {
	router, store := setupTestRouter(t, MemoryStoreOptions{SimilarityDelta: 0.99})
	store.Add("alpha bravo charlie delta", "", "", nil)
	store.Add("alpha bravo charlie echo", "", "", nil)

	w := doJSON(t, router, http.MethodPost, "/v1/rebuild-edges", map[string]any{
		"force_rebuild":  true,
		"min_similarity": 0.1,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("rebuild: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp RebuildEdgesResult
	decodeBody(t, w, &resp)
	if resp.ChunksProcessed != 2 {
		t.Fatalf("rebuild: expected 2 processed, got %d", resp.ChunksProcessed)
	}
	if resp.EdgesCreated == 0 {
		t.Fatal("rebuild: expected at least 1 edge created")
	}
}

// --- Auto Consolidation ---

func TestAPI_AutoConsolidate(t *testing.T) {
	router, _ := setupTestRouter(t)

	w := doJSON(t, router, http.MethodPost, "/v1/consolidation/auto", map[string]any{
		"dry_run": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("auto: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Param parsing ---

func TestAPI_Relevant_BadLimit(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doGet(t, router, "/v1/memories/relevant?message=hello&limit=abc")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad limit, got %d", w.Code)
	}
}

func TestAPI_Candidates_BadMinSimilarity(t *testing.T) {
	router, _ := setupTestRouter(t)
	w := doGet(t, router, "/v1/consolidation/candidates?min_similarity=not_a_float")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad min_similarity, got %d", w.Code)
	}
}

// --- CORS ---

func TestAPI_CORS_WithOrigin(t *testing.T) {
	router, _ := setupTestRouter(t)

	// Register CORS middleware on a test route.
	api := &Api{store: nil}
	router.Use(api.corsMiddleware)
	router.GET("/test-cors", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/test-cors", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	origin := w.Header().Get("Access-Control-Allow-Origin")
	if origin != "https://example.com" {
		t.Fatalf("expected origin echo, got %q", origin)
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("expected credentials=true with origin")
	}
}

func TestAPI_CORS_WithoutOrigin(t *testing.T) {
	router, _ := setupTestRouter(t)

	api := &Api{store: nil}
	router.Use(api.corsMiddleware)
	router.GET("/test-cors", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/test-cors", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	origin := w.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Fatalf("expected wildcard origin, got %q", origin)
	}
}

func TestAPI_CORS_Preflight(t *testing.T) {
	router, _ := setupTestRouter(t)

	api := &Api{store: nil}
	router.Use(api.corsMiddleware)
	router.GET("/test-cors", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest(http.MethodOptions, "/test-cors", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight: expected 204, got %d", w.Code)
	}
}

// --- Query param fallback for POST endpoints ---

func TestAPI_RebuildEdges_QueryParams(t *testing.T) {
	router, store := setupTestRouter(t, MemoryStoreOptions{SimilarityDelta: 0.99})
	store.Add("alpha bravo charlie delta", "", "", nil)
	store.Add("alpha bravo charlie echo", "", "", nil)

	// POST with empty body and query params instead.
	req := httptest.NewRequest(http.MethodPost, "/v1/rebuild-edges?force_rebuild=true&min_similarity=0.1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rebuild query params: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_RebuildEdges_BadForceParam(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/rebuild-edges?force_rebuild=notbool", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPI_AutoConsolidate_QueryParams(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/consolidation/auto?dry_run=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("auto query params: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_AutoConsolidate_BadParams(t *testing.T) {
	router, _ := setupTestRouter(t)

	// Bad min_similarity
	req := httptest.NewRequest(http.MethodPost, "/v1/consolidation/auto?min_similarity=abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad min_similarity, got %d", w.Code)
	}

	// Bad max_consolidations
	req = httptest.NewRequest(http.MethodPost, "/v1/consolidation/auto?max_consolidations=xyz", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad max_consolidations, got %d", w.Code)
	}

	// Bad dry_run
	req = httptest.NewRequest(http.MethodPost, "/v1/consolidation/auto?dry_run=notbool", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad dry_run, got %d", w.Code)
	}
}

// --- Search with alternate param ---

func TestAPI_Search_TextParam(t *testing.T) {
	router, store := setupTestRouter(t)
	store.Add("PostgreSQL database layer", "", "", nil)

	// Use "text" instead of "q"
	w := doGet(t, router, "/v1/memories/search?text=PostgreSQL")
	if w.Code != http.StatusOK {
		t.Fatalf("search text param: expected 200, got %d", w.Code)
	}
}
