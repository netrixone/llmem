package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteStorage_PersistAndLoad(t *testing.T) {
	// Use temp file for test DB.
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create storage and store a chunk.
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage() error: %v", err)
	}
	if err := storage.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	chunk := storedChunkData{
		ID:        "m1",
		Text:      "test memory",
		Label:     "test label",
		CreatedAt: time.Now().Truncate(time.Microsecond),
		Vector:    map[string]float64{"test": 1.0, "memory": 1.0},
		Norm:      1.414,
		Tokens:    []string{"test", "memory", "label"},
		Edges:     map[string]float64{},
	}

	if err := storage.Save(chunk); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	storage.Close()

	// Reopen and verify data persisted.
	storage2, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage() reopen error: %v", err)
	}
	defer storage2.Close()

	chunks, err := storage2.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].ID != "m1" {
		t.Fatalf("unexpected ID: got %q", chunks[0].ID)
	}
	if chunks[0].Text != "test memory" {
		t.Fatalf("unexpected Text: got %q", chunks[0].Text)
	}
}

func TestSQLiteStorage_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage() error: %v", err)
	}
	if err := storage.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer storage.Close()

	chunk := storedChunkData{
		ID:        "m1",
		Text:      "to delete",
		Label:     "delete me",
		CreatedAt: time.Now(),
		Vector:    map[string]float64{},
		Tokens:    []string{},
		Edges:     map[string]float64{},
	}

	if err := storage.Save(chunk); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if err := storage.Delete("m1"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	chunks, err := storage.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks after delete, got %d", len(chunks))
	}

	// Deleting non-existent should error.
	if err := storage.Delete("m999"); err == nil {
		t.Fatalf("expected error deleting non-existent, got nil")
	}
}

func TestMemoryStore_PersistenceAcrossRestarts(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create store with persistence.
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage() error: %v", err)
	}
	if err := storage.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	store, err := NewMemoryStore(MemoryStoreOptions{Storage: storage})
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}

	// Add memories.
	c1, _, err := store.Add("alpha beta", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	c2, _, err := store.Add("gamma delta", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	store.Close()

	// Reopen and verify data persisted.
	storage2, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage() reopen error: %v", err)
	}

	store2, err := NewMemoryStore(MemoryStoreOptions{Storage: storage2})
	if err != nil {
		t.Fatalf("NewMemoryStore() reopen error: %v", err)
	}
	defer store2.Close()

	// Verify both chunks exist.
	got1, _, err := store2.Get(c1.ID)
	if err != nil {
		t.Fatalf("Get(%s) error: %v", c1.ID, err)
	}
	if got1.Text != c1.Text {
		t.Fatalf("unexpected text for %s: got %q want %q", c1.ID, got1.Text, c1.Text)
	}

	got2, _, err := store2.Get(c2.ID)
	if err != nil {
		t.Fatalf("Get(%s) error: %v", c2.ID, err)
	}
	if got2.Text != c2.Text {
		t.Fatalf("unexpected text for %s: got %q want %q", c2.ID, got2.Text, c2.Text)
	}

	// Verify next ID continues from where we left off.
	c3, _, err := store2.Add("epsilon", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if c3.ID != "m3" {
		t.Fatalf("expected m3 after reload, got %q", c3.ID)
	}
}

func TestMemoryStore_UpdatedAtPersistsAcrossRestart(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	storage.Init()
	store, _ := NewMemoryStore(MemoryStoreOptions{Storage: storage})

	c, _, _ := store.Add("original memory text", "", "fact", nil)
	store.Update(c.ID, "updated memory text", "", "", nil)

	// Verify UpdatedAt is set.
	got, _, _ := store.Get(c.ID)
	if got.UpdatedAt == nil {
		t.Fatal("UpdatedAt should be set after Update()")
	}
	savedUpdatedAt := *got.UpdatedAt

	store.Close()

	// Reopen.
	storage2, _ := NewSQLiteStorage(dbPath)
	store2, _ := NewMemoryStore(MemoryStoreOptions{Storage: storage2})
	defer store2.Close()

	got2, _, err := store2.Get(c.ID)
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if got2.UpdatedAt == nil {
		t.Fatal("UpdatedAt should survive restart")
	}
	// Allow sub-second precision loss in SQLite round-trip.
	diff := got2.UpdatedAt.Sub(savedUpdatedAt)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("UpdatedAt drift too large: got %v, want ~%v (diff: %v)", *got2.UpdatedAt, savedUpdatedAt, diff)
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, _ := NewSQLiteStorage(dbPath)
	storage.Init()
	store, _ := NewMemoryStore(MemoryStoreOptions{Storage: storage, SimilarityDelta: 0.1})
	defer store.Close()

	c1, _, _ := store.Add("alpha", "", "", nil)
	c2, _, _ := store.Add("alpha beta", "", "", nil)

	// c2 should have edge to c1.
	_, neighbors, _ := store.Get(c2.ID)
	if len(neighbors) != 1 || neighbors[0].ID != c1.ID {
		t.Fatalf("expected c2 to have neighbor c1")
	}

	// Delete c1.
	deleted, err := store.Delete(c1.ID)
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if deleted.ID != c1.ID {
		t.Fatalf("unexpected deleted ID: got %q want %q", deleted.ID, c1.ID)
	}

	// c1 should not exist.
	_, _, err = store.Get(c1.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}

	// c2 should have no neighbors (edge to c1 removed).
	_, neighbors, _ = store.Get(c2.ID)
	if len(neighbors) != 0 {
		t.Fatalf("expected c2 to have no neighbors after c1 deleted, got %d", len(neighbors))
	}
}

func TestMemoryStore_DeleteFileNotFound(t *testing.T) {
	// Create a store, verify deletion error when file is unlinked
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, _ := NewSQLiteStorage(dbPath)
	storage.Init()
	store, _ := NewMemoryStore(MemoryStoreOptions{Storage: storage})
	defer store.Close()

	// Deleting non-existent memory.
	_, err := store.Delete("m999")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestNullStorage(t *testing.T) {
	// NullStorage should work but not persist.
	store, err := NewMemoryStore(MemoryStoreOptions{})
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}

	c, _, err := store.Add("test", "", "", nil)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if c.ID != "m1" {
		t.Fatalf("unexpected ID: got %q", c.ID)
	}
}

func TestMemoryStore_Update(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, _ := NewSQLiteStorage(dbPath)
	storage.Init()
	store, _ := NewMemoryStore(MemoryStoreOptions{Storage: storage, SimilarityDelta: 0.1})
	defer store.Close()

	// Add initial memory.
	c1, _, _ := store.Add("alpha beta", "", "", nil)
	c2, _, _ := store.Add("gamma delta", "", "", nil)

	// Update c1.
	updated, related, err := store.Update(c1.ID, "gamma epsilon", "", "", nil)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated.ID != c1.ID {
		t.Fatalf("unexpected ID: got %q want %q", updated.ID, c1.ID)
	}
	if updated.Text != "gamma epsilon" {
		t.Fatalf("unexpected text: got %q", updated.Text)
	}

	// Should now be related to c2 (both have "gamma").
	found := false
	for _, r := range related {
		if r.ID == c2.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected c2 in related after update")
	}

	// Verify persistence.
	store.Close()
	storage2, _ := NewSQLiteStorage(dbPath)
	store2, _ := NewMemoryStore(MemoryStoreOptions{Storage: storage2})
	defer store2.Close()

	got, _, _ := store2.Get(c1.ID)
	if got.Text != "gamma epsilon" {
		t.Fatalf("update not persisted: got %q", got.Text)
	}
}

func TestMemoryStore_Update_KeepsExistingLabelAndType(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	// Add with explicit label and type.
	c, _, _ := store.Add("original text", "My Label", "fact", nil)
	if c.Label != "My Label" || c.Type != "fact" {
		t.Fatalf("Add: expected label=%q type=%q, got label=%q type=%q", "My Label", "fact", c.Label, c.Type)
	}

	// Update text only (empty label and type) -- both should be preserved.
	updated, _, err := store.Update(c.ID, "new text", "", "", nil)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated.Text != "new text" {
		t.Fatalf("expected text %q, got %q", "new text", updated.Text)
	}
	if updated.Label != "My Label" {
		t.Fatalf("expected label preserved as %q, got %q", "My Label", updated.Label)
	}
	if updated.Type != "fact" {
		t.Fatalf("expected type preserved as %q, got %q", "fact", updated.Type)
	}

	// Update with explicit new label -- should change.
	updated2, _, err := store.Update(c.ID, "new text", "New Label", "", nil)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated2.Label != "New Label" {
		t.Fatalf("expected label changed to %q, got %q", "New Label", updated2.Label)
	}
	if updated2.Type != "fact" {
		t.Fatalf("expected type still %q, got %q", "fact", updated2.Type)
	}

	// Update with explicit new type -- should change.
	updated3, _, err := store.Update(c.ID, "new text", "", "decision", nil)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated3.Label != "New Label" {
		t.Fatalf("expected label still %q, got %q", "New Label", updated3.Label)
	}
	if updated3.Type != "decision" {
		t.Fatalf("expected type changed to %q, got %q", "decision", updated3.Type)
	}
}

func TestMemoryStore_Stats(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{SimilarityDelta: 0.1})

	// Empty stats.
	stats := store.Stats()
	if stats.TotalMemories != 0 {
		t.Fatalf("expected 0 memories, got %d", stats.TotalMemories)
	}

	// Add some memories.
	store.Add("alpha beta", "", "", nil)
	store.Add("gamma delta", "", "", nil)
	store.Add("alpha gamma", "", "", nil) // should connect to both

	stats = store.Stats()
	if stats.TotalMemories != 3 {
		t.Fatalf("expected 3 memories, got %d", stats.TotalMemories)
	}
	if stats.TotalEdges == 0 {
		t.Fatalf("expected some edges")
	}
	if len(stats.MostConnected) == 0 {
		t.Fatalf("expected most connected list")
	}
}

func TestMemoryStore_Export(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	store.Add("alpha", "", "", nil)
	store.Add("beta", "", "", nil)

	exported := store.Export()
	if len(exported) != 2 {
		t.Fatalf("expected 2 exported, got %d", len(exported))
	}
	// Should be sorted by ID.
	if exported[0].ID != "m1" || exported[1].ID != "m2" {
		t.Fatalf("unexpected export order: %s, %s", exported[0].ID, exported[1].ID)
	}
}

func TestMemoryStore_Import(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	chunks := []ImportChunk{
		{Text: "alpha beta"},
		{Text: "gamma delta"},
		{Text: ""}, // Should fail - empty
	}

	results := store.Import(chunks, false)
	if results.Imported != 2 {
		t.Fatalf("expected 2 imported, got %d", results.Imported)
	}
	if results.Failed != 1 {
		t.Fatalf("expected 1 failed, got %d", results.Failed)
	}

	// Verify memories exist.
	stats := store.Stats()
	if stats.TotalMemories != 2 {
		t.Fatalf("expected 2 memories, got %d", stats.TotalMemories)
	}
}

func TestMemoryStore_List(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	store.Add("first", "", "", nil)
	store.Add("second", "", "", nil)
	store.Add("third", "", "", nil)

	list := store.List("", "")
	if len(list) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list))
	}
	// Should be sorted newest first.
	if list[0].ID != "m3" {
		t.Fatalf("expected m3 first (newest), got %s", list[0].ID)
	}
	if list[2].ID != "m1" {
		t.Fatalf("expected m1 last (oldest), got %s", list[2].ID)
	}
}

func TestMemoryStore_ImportSkipExisting(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	// Add initial memory.
	c1, _, _ := store.Add("original text", "", "", nil)

	// Try to import with same ID (should be skipped).
	chunks := []ImportChunk{
		{ID: c1.ID, Text: "new text"},
		{Text: "another memory"},
	}

	results := store.Import(chunks, true)
	if results.Skipped != 1 {
		t.Fatalf("expected 1 skipped, got %d", results.Skipped)
	}
	if results.Imported != 1 {
		t.Fatalf("expected 1 imported, got %d", results.Imported)
	}

	// Original should be unchanged.
	got, _, _ := store.Get(c1.ID)
	if got.Text != "original text" {
		t.Fatalf("original was modified: %q", got.Text)
	}
}

func TestMemoryStore_ImportPreservesIDWhenAvailable(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	// Import with explicit IDs.
	results := store.Import([]ImportChunk{
		{ID: "m42", Text: "memory with specific id"},
		{ID: "m99", Text: "another specific id"},
		{Text: "no id specified"},
	}, false)
	if results.Imported != 3 {
		t.Fatalf("expected 3 imported, got %d", results.Imported)
	}

	// Verify the explicit IDs were preserved.
	if results.Results[0].NewID != "m42" {
		t.Errorf("expected ID m42, got %s", results.Results[0].NewID)
	}
	if results.Results[1].NewID != "m99" {
		t.Errorf("expected ID m99, got %s", results.Results[1].NewID)
	}
	// Third chunk gets auto-generated ID; must not collide with m42 or m99.
	autoID := results.Results[2].NewID
	if autoID == "m42" || autoID == "m99" {
		t.Errorf("auto-generated ID %s collides with imported ID", autoID)
	}

	// Verify chunks are retrievable by their preserved IDs.
	c42, _, err := store.Get("m42")
	if err != nil {
		t.Fatalf("Get(m42): %v", err)
	}
	if c42.Text != "memory with specific id" {
		t.Errorf("m42 text: got %q", c42.Text)
	}
}

func TestMemoryStore_ImportIDConflictGeneratesNew(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	// Add a memory that will occupy m1.
	c1, _, _ := store.Add("existing memory", "", "", nil)

	// Import with conflicting ID and skipExisting=false.
	results := store.Import([]ImportChunk{
		{ID: c1.ID, Text: "conflicting import text"},
	}, false)
	if results.Imported != 1 {
		t.Fatalf("expected 1 imported, got %d (failed: %d)", results.Imported, results.Failed)
	}
	// New ID should differ from the conflicting one.
	if results.Results[0].NewID == c1.ID {
		t.Fatalf("expected new ID, but got conflicting ID %s", c1.ID)
	}

	// Original memory should still be intact.
	got, _, _ := store.Get(c1.ID)
	if got.Text != "existing memory" {
		t.Fatalf("original was modified: %q", got.Text)
	}
}

func TestMemoryStore_ImportPreservesCreatedAt(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	past := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	results := store.Import([]ImportChunk{
		{ID: "m5", Text: "memory from the past", CreatedAt: past},
		{Text: "memory without timestamp"},
	}, false)
	if results.Imported != 2 {
		t.Fatalf("expected 2 imported, got %d", results.Imported)
	}

	// Chunk with explicit CreatedAt should preserve it.
	c5, _, _ := store.Get("m5")
	if !c5.CreatedAt.Equal(past) {
		t.Errorf("CreatedAt not preserved: got %v, want %v", c5.CreatedAt, past)
	}

	// Chunk without CreatedAt should get a recent timestamp.
	autoID := results.Results[1].NewID
	cAuto, _, _ := store.Get(autoID)
	if cAuto.CreatedAt.Before(time.Now().Add(-5 * time.Second)) {
		t.Errorf("auto CreatedAt too old: %v", cAuto.CreatedAt)
	}
}

func TestMemoryStore_ExportImportRoundTrip(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	// Create memories with labels, types, and scopes.
	store.Add("global fact about databases", "DB fact", "fact", nil)
	store.Add("scoped decision for project alpha", "Alpha decision", "decision", []string{"alpha"})
	store.Add("multi-scope note shared between teams", "Shared note", "note", []string{"alpha", "beta"})

	exported := store.Export()
	if len(exported) != 3 {
		t.Fatalf("expected 3 exported, got %d", len(exported))
	}

	// Verify exported data preserves all fields.
	for _, ex := range exported {
		if ex.Label == "" {
			t.Errorf("exported chunk %s has empty label", ex.ID)
		}
		if ex.Type == "" {
			t.Errorf("exported chunk %s has empty type", ex.ID)
		}
	}

	// Find the scoped chunks in export.
	var foundAlpha, foundMulti bool
	for _, ex := range exported {
		if ex.Label == "Alpha decision" {
			foundAlpha = true
			if len(ex.Scopes) != 1 || ex.Scopes[0] != "alpha" {
				t.Errorf("Alpha decision scopes: got %v, want [alpha]", ex.Scopes)
			}
		}
		if ex.Label == "Shared note" {
			foundMulti = true
			if len(ex.Scopes) != 2 {
				t.Errorf("Shared note scopes: got %v, want [alpha, beta]", ex.Scopes)
			}
		}
	}
	if !foundAlpha || !foundMulti {
		t.Fatal("missing expected exported chunks")
	}

	// Import into a fresh store, passing ID and CreatedAt from export (backup/restore).
	store2, _ := NewMemoryStore(MemoryStoreOptions{})
	importChunks := make([]ImportChunk, len(exported))
	for i, ex := range exported {
		importChunks[i] = ImportChunk{
			ID:        ex.ID,
			Text:      ex.Text,
			Label:     ex.Label,
			Type:      ex.Type,
			Scopes:    ex.Scopes,
			CreatedAt: ex.CreatedAt,
		}
	}
	results := store2.Import(importChunks, false)
	if results.Imported != 3 {
		t.Fatalf("expected 3 imported, got %d (failed: %d)", results.Imported, results.Failed)
	}

	// Verify IDs are preserved through the round-trip.
	for i, r := range results.Results {
		if r.NewID != exported[i].ID {
			t.Errorf("import[%d]: ID not preserved: got %s, want %s", i, r.NewID, exported[i].ID)
		}
	}

	// Verify imported data preserved labels, types, scopes, and timestamps.
	list := store2.List("", "")
	if len(list) != 3 {
		t.Fatalf("expected 3 memories in new store, got %d", len(list))
	}

	for _, ex := range exported {
		chunk, _, err := store2.Get(ex.ID)
		if err != nil {
			t.Errorf("imported chunk %s not found by original ID", ex.ID)
			continue
		}
		if chunk.Label == "" {
			t.Errorf("imported chunk %s lost label", chunk.ID)
		}
		if chunk.Type == "" {
			t.Errorf("imported chunk %s lost type", chunk.ID)
		}
		if !chunk.CreatedAt.Equal(ex.CreatedAt) {
			t.Errorf("imported chunk %s: CreatedAt not preserved: got %v, want %v", chunk.ID, chunk.CreatedAt, ex.CreatedAt)
		}
	}

	// Verify scoped memory survives: search with scope filter.
	alphaList := store2.List("", "alpha")
	// Should find: the alpha-only decision, the multi-scope note, and the global fact.
	if len(alphaList) != 3 {
		t.Fatalf("alpha scope list: expected 3 (2 scoped + 1 global), got %d", len(alphaList))
	}

	// Verify that new IDs generated after import don't collide with imported IDs.
	newChunk, _, err := store2.Add("brand new memory after import", "", "", nil)
	if err != nil {
		t.Fatalf("Add after import failed: %v", err)
	}
	for _, ex := range exported {
		if newChunk.ID == ex.ID {
			t.Fatalf("new ID %s collides with imported ID", newChunk.ID)
		}
	}
}

func TestMemoryStore_FindRelevant(t *testing.T) {
	store, _ := NewMemoryStore(MemoryStoreOptions{})

	// Add memories about different topics.
	store.Add("#relationship Alice: works on backend, prefers Go", "", "", nil)
	store.Add("#decision Using PostgreSQL for the database layer", "", "", nil)
	store.Add("#status Project alpha is in beta testing phase", "", "", nil)

	// Conversational message about database should find the PostgreSQL memory.
	results := store.FindRelevant("What database are we using for the project?", 5, "", "")
	if len(results) == 0 {
		t.Fatal("expected at least one relevant memory")
	}

	found := false
	for _, r := range results {
		if r.Label != "" && (strings.Contains(r.Text, "PostgreSQL") || strings.Contains(r.Text, "database")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find PostgreSQL memory, got: %+v", results)
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		input    string
		contains []string
		excludes []string
	}{
		{
			input:    "What is the database configuration?",
			contains: []string{"databas", "configur"}, // stemmed forms
			excludes: []string{"what", "is", "the"},
		},
		{
			input:    "Can you help me with #authentication?",
			contains: []string{"#authentication"},
			excludes: []string{"can", "you", "help", "me", "with"},
		},
		{
			input:    "How does the backend work?",
			contains: []string{"backend", "work"},
			excludes: []string{"how", "does", "the"},
		},
		{
			input:    "Please update #ops-team metrics.",
			contains: []string{"#ops-team", "updat", "metric"}, // stemmed forms
			excludes: []string{"please"},
		},
	}

	for _, tc := range tests {
		keywords := extractKeywords(tc.input)
		kwSet := make(map[string]struct{})
		for _, k := range keywords {
			kwSet[k] = struct{}{}
		}

		for _, want := range tc.contains {
			if _, ok := kwSet[want]; !ok {
				t.Errorf("extractKeywords(%q): expected %q in keywords %v", tc.input, want, keywords)
			}
		}
		for _, notWant := range tc.excludes {
			if _, ok := kwSet[notWant]; ok {
				t.Errorf("extractKeywords(%q): did not expect %q in keywords %v", tc.input, notWant, keywords)
			}
		}
	}
}

// TestSQLiteStorage_Migration_AddTypeColumn verifies that an existing DB created
// without the type column can be opened after Init() (which runs ALTER TABLE ADD COLUMN type)
// and loads chunks with Type "".
func TestSQLiteStorage_Migration_AddTypeColumn(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "old.db")

	// Create DB with old schema (no type column).
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open error: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE chunks (
			id TEXT PRIMARY KEY,
			text TEXT NOT NULL,
			label TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			vector_json TEXT NOT NULL,
			norm REAL NOT NULL,
			label_vector_json TEXT NOT NULL,
			label_norm REAL NOT NULL,
			tokens_json TEXT NOT NULL,
			edges_json TEXT NOT NULL
		)
	`)
	if err != nil {
		db.Close()
		t.Fatalf("CREATE TABLE error: %v", err)
	}
	vecJSON, _ := json.Marshal(map[string]float64{"a": 1.0})
	labelVecJSON, _ := json.Marshal(map[string]float64{})
	tokensJSON, _ := json.Marshal([]string{"a"})
	edgesJSON, _ := json.Marshal(map[string]float64{})
	_, err = db.Exec(`
		INSERT INTO chunks (id, text, label, created_at, vector_json, norm, label_vector_json, label_norm, tokens_json, edges_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "m1", "old memory", "old label", time.Now().Format(time.RFC3339Nano), string(vecJSON), 1.0, string(labelVecJSON), 0.0, string(tokensJSON), string(edgesJSON))
	if err != nil {
		db.Close()
		t.Fatalf("INSERT error: %v", err)
	}
	db.Close()

	// Open with SQLiteStorage; Init() adds type column.
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage error: %v", err)
	}
	defer storage.Close()
	if err := storage.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	chunks, err := storage.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Type != "" {
		t.Fatalf("expected Type \"\" for migrated chunk, got %q", chunks[0].Type)
	}
	if chunks[0].Text != "old memory" {
		t.Fatalf("unexpected Text: got %q", chunks[0].Text)
	}

	// Save a new chunk with Type set; verify round-trip.
	chunks[0].Type = "fact"
	if err := storage.Save(chunks[0]); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	chunks2, err := storage.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() second time error: %v", err)
	}
	if len(chunks2) != 1 {
		t.Fatalf("expected 1 chunk after save, got %d", len(chunks2))
	}
	if chunks2[0].Type != "fact" {
		t.Fatalf("expected Type \"fact\" after save, got %q", chunks2[0].Type)
	}
}

func TestSQLiteStorage_LoadAll_SkipsCorruptedRows(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage() error: %v", err)
	}
	defer storage.Close()
	if err := storage.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Save a valid chunk.
	goodVec, _ := json.Marshal(map[string]float64{"hello": 1})
	goodToks, _ := json.Marshal([]string{"hello"})
	goodEdges, _ := json.Marshal(map[string]float64{})
	now := time.Now().Format(time.RFC3339Nano)

	_, err = storage.db.Exec(`
		INSERT INTO chunks (id, text, label, type, created_at, vector_json, norm,
			label_vector_json, label_norm, tokens_json, edges_json, scopes_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', 0, ?, ?, '[]')`,
		"m1", "good memory", "good", "fact", now,
		string(goodVec), 1.0, string(goodToks), string(goodEdges))
	if err != nil {
		t.Fatalf("insert good row: %v", err)
	}

	// Insert a row with corrupted vector_json.
	_, err = storage.db.Exec(`
		INSERT INTO chunks (id, text, label, type, created_at, vector_json, norm,
			label_vector_json, label_norm, tokens_json, edges_json, scopes_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', 0, ?, ?, '[]')`,
		"m2", "bad vector", "bad", "note", now,
		"NOT_JSON", 1.0, string(goodToks), string(goodEdges))
	if err != nil {
		t.Fatalf("insert bad vector row: %v", err)
	}

	// Insert a row with corrupted tokens_json.
	_, err = storage.db.Exec(`
		INSERT INTO chunks (id, text, label, type, created_at, vector_json, norm,
			label_vector_json, label_norm, tokens_json, edges_json, scopes_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', 0, ?, ?, '[]')`,
		"m3", "bad tokens", "bad", "note", now,
		string(goodVec), 1.0, "{INVALID}", string(goodEdges))
	if err != nil {
		t.Fatalf("insert bad tokens row: %v", err)
	}

	// Insert a row with corrupted edges_json.
	_, err = storage.db.Exec(`
		INSERT INTO chunks (id, text, label, type, created_at, vector_json, norm,
			label_vector_json, label_norm, tokens_json, edges_json, scopes_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', 0, ?, ?, '[]')`,
		"m4", "bad edges", "bad", "note", now,
		string(goodVec), 1.0, string(goodToks), "<<<>>>")
	if err != nil {
		t.Fatalf("insert bad edges row: %v", err)
	}

	// Insert another valid chunk after the bad ones.
	_, err = storage.db.Exec(`
		INSERT INTO chunks (id, text, label, type, created_at, vector_json, norm,
			label_vector_json, label_norm, tokens_json, edges_json, scopes_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', 0, ?, ?, '[]')`,
		"m5", "also good", "good2", "fact", now,
		string(goodVec), 1.0, string(goodToks), string(goodEdges))
	if err != nil {
		t.Fatalf("insert second good row: %v", err)
	}

	// LoadAll should succeed, returning only the 2 good rows.
	chunks, err := storage.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 good chunks, got %d", len(chunks))
	}
	if chunks[0].ID != "m1" {
		t.Fatalf("expected first chunk m1, got %s", chunks[0].ID)
	}
	if chunks[1].ID != "m5" {
		t.Fatalf("expected second chunk m5, got %s", chunks[1].ID)
	}
}
