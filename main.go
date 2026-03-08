package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Init persistent storage.
	// Default path: ~/.llmem/data.db (or LLMEM_DB env var)
	dbPath := os.Getenv("LLMEM_DB")
	if dbPath == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			dbPath = home + "/.llmem/data.db"
		} else {
			dbPath = "llmem.db"
		}
	}

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		log.Fatalf("Failed to open storage: %v", err)
	}
	if err := storage.Init(); err != nil {
		log.Fatalf("Failed to init storage: %v", err)
	}
	log.Printf("Using storage: %s", dbPath)

	// Configure summarizer: use TgptSummarizer if TGPT_BIN is set, else Truncater.
	var summarizer Summarizer
	if tgptBin := os.Getenv("TGPT_BIN"); tgptBin != "" {
		summarizer = NewTgptSummarizer(TgptSummarizerOptions{
			Binary:   tgptBin,
			Provider: os.Getenv("TGPT_PROVIDER"),
			Model:    os.Getenv("TGPT_MODEL"),
			Key:      os.Getenv("TGPT_KEY"),
			URL:      os.Getenv("TGPT_URL"),
		})
		log.Printf("Using tgpt summarizer: %s", tgptBin)
	}

	// Memory store with persistence.
	store, err := NewMemoryStore(MemoryStoreOptions{
		Storage:    storage,
		Summarizer: summarizer,
	})
	if err != nil {
		log.Fatalf("Failed to init memory store: %v", err)
	}

	api := NewAPI(store)
	api.Start()

	// Start periodic auto-consolidation if enabled.
	// Use a cancelable context so we can stop the goroutine before closing the store.
	autoCtx, autoCancel := context.WithCancel(context.Background())
	autoConsolidateInterval := envOrDuration("LLMEM_AUTO_CONSOLIDATE_INTERVAL", 0)
	if autoConsolidateInterval > 0 {
		go runPeriodicAutoConsolidation(autoCtx, store, autoConsolidateInterval)
		log.Printf("Periodic auto-consolidation enabled (interval: %v)", autoConsolidateInterval)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done

	log.Println("Shutting down...")
	// Stop periodic consolidation first to ensure no storage operations
	// are in progress when we close the store.
	autoCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := api.Shutdown(ctx); err != nil {
		log.Printf("Error shutting down HTTP server: %v", err)
	}
	if err := store.Close(); err != nil {
		log.Printf("Error closing store: %v", err)
	}
}

// runPeriodicAutoConsolidation runs auto-consolidation periodically in the background.
// It stops cleanly when ctx is cancelled, ensuring no storage operations are in-flight
// when the caller proceeds to close the store.
func runPeriodicAutoConsolidation(ctx context.Context, store *MemoryStore, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := store.AutoConsolidate(AutoConsolidateOptions{
				MinSimilarity:     0.95, // High threshold for automatic consolidation
				MaxConsolidations: 10,   // Limit to avoid long-running operations
				DryRun:            false,
			})
			if err != nil {
				log.Printf("Auto-consolidation error: %v", err)
				continue
			}
			if result.Consolidated > 0 {
				log.Printf("Auto-consolidated %d memory pairs (removed %d, merged %d)",
					result.Consolidated, len(result.Removed), len(result.Merged))
			}
		}
	}
}

// envOrDuration returns the duration value of key from the environment, or def if unset or invalid.
func envOrDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("Invalid duration for %s: %v, using default", key, err)
		return def
	}
	return d
}
