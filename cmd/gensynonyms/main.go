// Command gensynonyms reads Princeton WordNet data files and extracts
// synonym groups (synsets with 2+ distinct stemmed single-word terms).
// Output is a compact text format: one group per line, tab-separated
// Porter-stemmed words. Intended to be embedded via //go:embed.
//
// Usage:
//
//	go run ./cmd/gensynonyms /path/to/wordnet/dict > wordnet_synonyms.txt
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gensynonyms <wordnet-dict-dir>")
		os.Exit(1)
	}
	dictDir := os.Args[1]

	dataFiles := []string{"data.noun", "data.verb", "data.adj", "data.adv"}

	// Collect all synonym groups: each is a set of stemmed single-word terms.
	var groups [][]string

	for _, fname := range dataFiles {
		path := filepath.Join(dictDir, fname)
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening %s: %v\n", path, err)
			os.Exit(1)
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if len(line) == 0 || line[0] == ' ' {
				continue
			}
			words := parseSynsetWords(line)
			if len(words) < 2 {
				continue
			}

			seen := make(map[string]struct{}, len(words))
			var stemmed []string
			for _, w := range words {
				s := stemToFixpoint(strings.ToLower(w))
				if len(s) < 2 {
					continue
				}
				if _, ok := seen[s]; ok {
					continue
				}
				seen[s] = struct{}{}
				stemmed = append(stemmed, s)
			}
			if len(stemmed) >= 2 {
				sort.Strings(stemmed)
				groups = append(groups, stemmed)
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", path, err)
			os.Exit(1)
		}
		f.Close()
	}

	// Deduplicate identical groups.
	sort.Slice(groups, func(i, j int) bool {
		return strings.Join(groups[i], "\t") < strings.Join(groups[j], "\t")
	})
	w := bufio.NewWriter(os.Stdout)
	prev := ""
	count := 0
	for _, g := range groups {
		key := strings.Join(g, "\t")
		if key == prev {
			continue
		}
		prev = key
		fmt.Fprintln(w, key)
		count++
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "wrote %d synonym groups from %d raw synsets\n", count, len(groups))
}

// stemToFixpoint applies the Porter stemmer repeatedly until the result stabilizes.
// The Porter stemmer is not idempotent (e.g., "abampere" → "abamper" → "abamp"),
// so we loop to ensure every emitted term satisfies stem(term) == term.
func stemToFixpoint(w string) string {
	for {
		s := porterStem(w)
		if s == w {
			return s
		}
		w = s
	}
}

// parseSynsetWords extracts single-word lemmas from a WordNet data line.
func parseSynsetWords(line string) []string {
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return nil
	}
	var wCnt int
	fmt.Sscanf(fields[3], "%x", &wCnt)
	if wCnt < 1 {
		return nil
	}

	var words []string
	for i := 0; i < wCnt; i++ {
		idx := 4 + i*2
		if idx >= len(fields) {
			break
		}
		w := fields[idx]
		if strings.Contains(w, "_") {
			continue
		}
		clean := true
		for _, c := range w {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				clean = false
				break
			}
		}
		if !clean {
			continue
		}
		words = append(words, w)
	}
	return words
}
