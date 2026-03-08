package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Storage defines the persistence interface for memory chunks.
type Storage interface {
	// Init creates tables if needed.
	Init() error
	// LoadAll returns all stored chunks.
	LoadAll() ([]storedChunkData, error)
	// Save persists a single chunk.
	Save(chunk storedChunkData) error
	// Delete removes a chunk by ID.
	Delete(id string) error
	// Close releases resources.
	Close() error
}

// storedChunkData is the serializable form of a memory chunk.
type storedChunkData struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Label     string    `json:"label"`
	Type      string    `json:"type"`
	Scopes    []string  `json:"scopes,omitempty"` // Empty/nil = global
	CreatedAt time.Time `json:"createdAt"`

	// Vector and related data are stored as JSON for simplicity.
	Vector map[string]float64 `json:"vector"`
	Norm   float64            `json:"norm"`
	Tokens []string           `json:"tokens"`
	Edges  map[string]float64 `json:"edges"`

	// Access tracking
	LastAccessed *time.Time `json:"lastAccessed,omitempty"`
	AccessCount  uint64     `json:"accessCount,omitempty"`

	// Modification tracking
	UpdatedAt *time.Time `json:"updatedAt,omitempty"` // Nil until first Update()
}

// SQLiteStorage implements Storage using SQLite.
type SQLiteStorage struct {
	db   *sql.DB
	path string
}

// NewSQLiteStorage creates a new SQLite-backed storage.
// If path is empty, defaults to "llmem.db" in current directory.
func NewSQLiteStorage(path string) (*SQLiteStorage, error) {
	if path == "" {
		path = "llmem.db"
	}

	// Ensure directory exists.
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// SQLite doesn't handle concurrent writers well; keep a single connection
	// to avoid SQLITE_BUSY errors with parallel Save/Delete calls.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Enable WAL mode for better concurrent access.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	// Set busy timeout so concurrent operations retry instead of failing
	// immediately with SQLITE_BUSY.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteStorage{db: db, path: path}, nil
}

// Init creates the chunks table if it doesn't exist and runs migrations (e.g. add type column).
func (s *SQLiteStorage) Init() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS chunks (
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
		return err
	}
	// Migration: add type column for existing DBs (ignore duplicate column error).
	if _, err := s.db.Exec(`ALTER TABLE chunks ADD COLUMN type TEXT DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	// Migration: add access tracking columns
	if _, err := s.db.Exec(`ALTER TABLE chunks ADD COLUMN last_accessed DATETIME`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE chunks ADD COLUMN access_count INTEGER DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	// Migration: add scopes column
	if _, err := s.db.Exec(`ALTER TABLE chunks ADD COLUMN scopes_json TEXT DEFAULT '[]'`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	// Migration: add updated_at column
	if _, err := s.db.Exec(`ALTER TABLE chunks ADD COLUMN updated_at DATETIME`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	return nil
}

// LoadAll retrieves all chunks from the database.
func (s *SQLiteStorage) LoadAll() ([]storedChunkData, error) {
	rows, err := s.db.Query(`
		SELECT id, text, label, type, created_at, vector_json, norm, 
		       label_vector_json, label_norm, tokens_json, edges_json,
		       last_accessed, access_count, scopes_json, updated_at
		FROM chunks
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []storedChunkData
	for rows.Next() {
		var c storedChunkData
		var vectorJSON, tokensJSON, edgesJSON string
		var createdAt string
		var typeVal sql.NullString
		var lastAccessed sql.NullTime
		var accessCount sql.NullInt64
		var scopesJSONVal sql.NullString
		var updatedAt sql.NullTime
		// label_vector_json and label_norm are vestigial DB columns; scan into
		// throwaway variables so the SELECT still works on existing databases.
		var unusedLabelVecJSON string
		var unusedLabelNorm float64

		err := rows.Scan(
			&c.ID, &c.Text, &c.Label, &typeVal, &createdAt,
			&vectorJSON, &c.Norm,
			&unusedLabelVecJSON, &unusedLabelNorm,
			&tokensJSON, &edgesJSON,
			&lastAccessed, &accessCount,
			&scopesJSONVal,
			&updatedAt,
		)
		if err != nil {
			log.Printf("LoadAll: skipping row (scan error): %v", err)
			continue
		}

		if typeVal.Valid && typeVal.String != "" {
			c.Type = typeVal.String
		}

		// Parse timestamp.
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)

		// Parse access tracking
		if lastAccessed.Valid {
			c.LastAccessed = &lastAccessed.Time
		}
		if accessCount.Valid {
			c.AccessCount = uint64(accessCount.Int64)
		}
		if updatedAt.Valid {
			c.UpdatedAt = &updatedAt.Time
		}

		// Parse scopes
		if scopesJSONVal.Valid && scopesJSONVal.String != "" {
			if err := json.Unmarshal([]byte(scopesJSONVal.String), &c.Scopes); err != nil {
				c.Scopes = nil
			}
		} else {
			c.Scopes = nil
		}

		// Parse JSON fields — skip row on corruption instead of aborting.
		if err := json.Unmarshal([]byte(vectorJSON), &c.Vector); err != nil {
			log.Printf("LoadAll: skipping %s (bad vector_json): %v", c.ID, err)
			continue
		}
		var tokens []string
		if err := json.Unmarshal([]byte(tokensJSON), &tokens); err != nil {
			log.Printf("LoadAll: skipping %s (bad tokens_json): %v", c.ID, err)
			continue
		}
		c.Tokens = tokens
		if err := json.Unmarshal([]byte(edgesJSON), &c.Edges); err != nil {
			log.Printf("LoadAll: skipping %s (bad edges_json): %v", c.ID, err)
			continue
		}

		chunks = append(chunks, c)
	}

	return chunks, rows.Err()
}

// Save persists a chunk to the database (insert or replace).
func (s *SQLiteStorage) Save(chunk storedChunkData) error {
	vectorJSON, err := json.Marshal(chunk.Vector)
	if err != nil {
		return err
	}
	tokensJSON, err := json.Marshal(chunk.Tokens)
	if err != nil {
		return err
	}
	edgesJSON, err := json.Marshal(chunk.Edges)
	if err != nil {
		return err
	}
	scopesJSON, err := json.Marshal(chunk.Scopes)
	if err != nil {
		return err
	}

	var lastAccessed interface{}
	if chunk.LastAccessed != nil {
		lastAccessed = chunk.LastAccessed.Format(time.RFC3339Nano)
	}
	var updatedAtVal interface{}
	if chunk.UpdatedAt != nil {
		updatedAtVal = chunk.UpdatedAt.Format(time.RFC3339Nano)
	}
	// label_vector_json and label_norm are vestigial columns kept for DB compatibility;
	// always write empty defaults.
	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO chunks 
		(id, text, label, type, created_at, vector_json, norm, label_vector_json, label_norm, tokens_json, edges_json, last_accessed, access_count, scopes_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', 0, ?, ?, ?, ?, ?, ?)
	`,
		chunk.ID, chunk.Text, chunk.Label, chunk.Type, chunk.CreatedAt.Format(time.RFC3339Nano),
		string(vectorJSON), chunk.Norm,
		string(tokensJSON), string(edgesJSON),
		lastAccessed, chunk.AccessCount,
		string(scopesJSON),
		updatedAtVal,
	)
	return err
}

// Delete removes a chunk by ID.
func (s *SQLiteStorage) Delete(id string) error {
	result, err := s.db.Exec("DELETE FROM chunks WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// Close closes the database connection.
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// NullStorage is a no-op storage for testing or when persistence is disabled.
type NullStorage struct{}

func (NullStorage) Init() error                         { return nil }
func (NullStorage) LoadAll() ([]storedChunkData, error) { return nil, nil }
func (NullStorage) Save(storedChunkData) error          { return nil }
func (NullStorage) Delete(string) error                 { return ErrNotFound }
func (NullStorage) Close() error                        { return nil }
