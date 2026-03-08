package main

import (
	"fmt"
	"testing"
)

// mustBenchStore creates a MemoryStore for benchmarks (no storage, no summarizer).
func mustBenchStore(b *testing.B) *MemoryStore {
	b.Helper()
	s, err := NewMemoryStore(MemoryStoreOptions{})
	if err != nil {
		b.Fatalf("NewMemoryStore() error: %v", err)
	}
	return s
}

// seedStore adds n memories with realistic text content.
func seedStore(b *testing.B, s *MemoryStore, n int) {
	b.Helper()
	topics := []string{
		"Go concurrency patterns with goroutines and channels for parallel processing",
		"SQLite WAL mode enables concurrent readers with single writer for better performance",
		"TF-IDF cosine similarity scoring for text search and document matching",
		"REST API design with Gin framework using JSON middleware and error handling",
		"Memory management in Go with sync.RWMutex for concurrent read-write access",
		"Edge-based similarity graph for finding related memories and neighbors",
		"Export import round-trip preserving IDs timestamps and metadata fidelity",
		"Auto-consolidation of duplicate memories using high similarity threshold",
		"MCP protocol tools for persistent AI memory across conversation sessions",
		"Token indexing with inverted index for fast lexical candidate lookup",
	}
	for i := range n {
		text := fmt.Sprintf("#fact Memory %d: %s", i, topics[i%len(topics)])
		if _, _, err := s.Add(text, "", "fact", nil); err != nil {
			b.Fatalf("seed Add(%d) error: %v", i, err)
		}
	}
}

// BenchmarkAdd measures the cost of adding a memory to a store of size N.
// Adds then deletes to keep the store at a stable size.
func BenchmarkAdd(b *testing.B) {
	for _, size := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			s := mustBenchStore(b)
			seedStore(b, s, size)
			b.ResetTimer()
			for i := range b.N {
				text := fmt.Sprintf("benchmark memory %d about testing performance", i)
				chunk, _, err := s.Add(text, "", "fact", nil)
				if err != nil {
					b.Fatalf("Add() error: %v", err)
				}
				s.Delete(chunk.ID)
			}
		})
	}
}

// BenchmarkGet measures retrieval by ID (map lookup + access tracking).
func BenchmarkGet(b *testing.B) {
	s := mustBenchStore(b)
	seedStore(b, s, 100)
	ids := make([]string, 0, 100)
	for _, item := range s.List("", "") {
		ids = append(ids, item.ID)
	}
	b.ResetTimer()
	for i := range b.N {
		id := ids[i%len(ids)]
		if _, _, err := s.Get(id); err != nil {
			b.Fatalf("Get(%s) error: %v", id, err)
		}
	}
}

// BenchmarkSearch measures content search across a store of size N.
func BenchmarkSearch(b *testing.B) {
	for _, size := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			s := mustBenchStore(b)
			seedStore(b, s, size)
			queries := []string{
				"concurrent goroutines channels",
				"SQLite database performance",
				"similarity scoring search",
				"REST API JSON endpoints",
				"memory management mutex",
			}
			b.ResetTimer()
			for i := range b.N {
				q := queries[i%len(queries)]
				if _, err := s.Search(q, "", ""); err != nil {
					b.Fatalf("Search() error: %v", err)
				}
			}
		})
	}
}

// BenchmarkFindRelevant measures mid-conversation relevance search.
func BenchmarkFindRelevant(b *testing.B) {
	for _, size := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			s := mustBenchStore(b)
			seedStore(b, s, size)
			messages := []string{
				"How does the SQLite storage work?",
				"Tell me about memory consolidation",
				"What are the API endpoints?",
				"How do you handle concurrent access?",
				"What about the edge similarity graph?",
			}
			b.ResetTimer()
			for i := range b.N {
				msg := messages[i%len(messages)]
				s.FindRelevant(msg, 5, "", "")
			}
		})
	}
}

// BenchmarkUpdate measures updating a memory in a store of size N.
func BenchmarkUpdate(b *testing.B) {
	for _, size := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			s := mustBenchStore(b)
			seedStore(b, s, size)
			ids := make([]string, 0, size)
			for _, item := range s.List("", "") {
				ids = append(ids, item.ID)
			}
			b.ResetTimer()
			for i := range b.N {
				id := ids[i%len(ids)]
				text := fmt.Sprintf("updated memory %d with new content about testing", i)
				if _, _, err := s.Update(id, text, "", "", nil); err != nil {
					b.Fatalf("Update() error: %v", err)
				}
			}
		})
	}
}

// BenchmarkGetContext measures context restoration across categories.
func BenchmarkGetContext(b *testing.B) {
	s := mustBenchStore(b)
	categories := []string{"#self", "#goal", "#relationship", "#status", "#principle", "#thought"}
	for i := range 60 {
		cat := categories[i%len(categories)]
		text := fmt.Sprintf("%s memory number %d about the project", cat, i)
		if _, _, err := s.Add(text, "", "fact", nil); err != nil {
			b.Fatalf("seed Add() error: %v", err)
		}
	}
	b.ResetTimer()
	for range b.N {
		s.GetContext("", "", 0)
	}
}

// BenchmarkList measures listing all memories.
func BenchmarkList(b *testing.B) {
	s := mustBenchStore(b)
	seedStore(b, s, 100)
	b.ResetTimer()
	for range b.N {
		s.List("", "")
	}
}

// BenchmarkTokenize measures the tokenization function.
func BenchmarkTokenize(b *testing.B) {
	texts := []string{
		"Go concurrency patterns with goroutines and channels for parallel processing",
		"#principle Build for your actual use case not imagined ones",
		"SQLite WAL mode enables concurrent readers with single writer",
	}
	for range b.N {
		for _, t := range texts {
			tokenize(t)
		}
	}
}
