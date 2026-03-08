package main

// Porter stemmer implementation.
// Based on: https://tartarus.org/martin/PorterStemmer/def.txt
//
// Pure Go, zero dependencies. Operates on lowercase ASCII English words.

// stem applies the Porter stemming algorithm to a single lowercase word.
// Returns the stemmed form. Words shorter than 3 characters are returned unchanged.
func stem(word string) string {
	if len(word) < 3 {
		return word
	}
	w := []byte(word)
	w = step1a(w)
	w = step1b(w)
	w = step1c(w)
	w = step2(w)
	w = step3(w)
	w = step4(w)
	w = step5a(w)
	w = step5b(w)
	return string(w)
}

// --- helper functions ---

func isConsonant(w []byte, i int) bool {
	switch w[i] {
	case 'a', 'e', 'i', 'o', 'u':
		return false
	case 'y':
		if i == 0 {
			return true
		}
		return !isConsonant(w, i-1)
	default:
		return true
	}
}

// measure returns the "measure" m of the word — the number of VC (vowel-consonant)
// sequences in the stem before a given position. For the full word: [C](VC){m}[V]
func measure(w []byte) int {
	n := len(w)
	i := 0
	// Skip leading consonants.
	for i < n && isConsonant(w, i) {
		i++
	}
	if i >= n {
		return 0
	}
	m := 0
	for i < n {
		// Skip vowels.
		for i < n && !isConsonant(w, i) {
			i++
		}
		if i >= n {
			break
		}
		// Skip consonants — each V→C transition is one measure.
		m++
		for i < n && isConsonant(w, i) {
			i++
		}
	}
	return m
}

// containsVowel returns true if the word contains at least one vowel.
func containsVowel(w []byte) bool {
	for i := range w {
		if !isConsonant(w, i) {
			return true
		}
	}
	return false
}

// endsWithDouble returns true if w ends with a double consonant (e.g. "ll", "ss").
func endsWithDouble(w []byte) bool {
	n := len(w)
	if n < 2 {
		return false
	}
	return w[n-1] == w[n-2] && isConsonant(w, n-1)
}

// endsWithCVC returns true if w ends with consonant-vowel-consonant,
// where the final consonant is not 'w', 'x', or 'y'. (The *o condition.)
func endsWithCVC(w []byte) bool {
	n := len(w)
	if n < 3 {
		return false
	}
	c2 := w[n-1]
	if c2 == 'w' || c2 == 'x' || c2 == 'y' {
		return false
	}
	return isConsonant(w, n-1) && !isConsonant(w, n-2) && isConsonant(w, n-3)
}

func hasSuffix(w []byte, suffix string) bool {
	if len(w) < len(suffix) {
		return false
	}
	for i := range suffix {
		if w[len(w)-len(suffix)+i] != suffix[i] {
			return false
		}
	}
	return true
}

func replaceSuffix(w []byte, suffix, replacement string) []byte {
	stem := w[:len(w)-len(suffix)]
	return append(stem, replacement...)
}

// --- Step 1a: plural reductions ---

func step1a(w []byte) []byte {
	if hasSuffix(w, "sses") {
		return w[:len(w)-2] // sses -> ss
	}
	if hasSuffix(w, "ies") {
		return w[:len(w)-2] // ies -> i
	}
	if hasSuffix(w, "ss") {
		return w // ss -> ss (no change)
	}
	if hasSuffix(w, "s") {
		return w[:len(w)-1] // s -> (delete)
	}
	return w
}

// --- Step 1b: past tense / gerund ---

func step1b(w []byte) []byte {
	if hasSuffix(w, "eed") {
		stem := w[:len(w)-3]
		if measure(stem) > 0 {
			return append(stem, "ee"...) // eed -> ee
		}
		return w
	}

	modified := false
	if hasSuffix(w, "ed") {
		stem := w[:len(w)-2]
		if containsVowel(stem) {
			w = stem
			modified = true
		}
	} else if hasSuffix(w, "ing") {
		stem := w[:len(w)-3]
		if containsVowel(stem) {
			w = stem
			modified = true
		}
	}

	if modified {
		// Additional transformations after removing -ed/-ing.
		if hasSuffix(w, "at") {
			return append(w, 'e') // at -> ate
		}
		if hasSuffix(w, "bl") {
			return append(w, 'e') // bl -> ble
		}
		if hasSuffix(w, "iz") {
			return append(w, 'e') // iz -> ize
		}
		if endsWithDouble(w) {
			c := w[len(w)-1]
			if c != 'l' && c != 's' && c != 'z' {
				return w[:len(w)-1] // double -> single (except l, s, z)
			}
		}
		if measure(w) == 1 && endsWithCVC(w) {
			return append(w, 'e') // *o -> add e
		}
	}

	return w
}

// --- Step 1c: y -> i ---

func step1c(w []byte) []byte {
	if hasSuffix(w, "y") {
		stem := w[:len(w)-1]
		if containsVowel(stem) {
			w[len(w)-1] = 'i'
		}
	}
	return w
}

// --- Step 2: context-sensitive suffix replacement (m > 0) ---

func step2(w []byte) []byte {
	// Dispatch on second-to-last character for efficiency.
	if len(w) < 3 {
		return w
	}
	switch w[len(w)-2] {
	case 'a':
		return step2replace(w, "ational", "ate", "tional", "tion")
	case 'c':
		return step2replace(w, "enci", "ence", "anci", "ance")
	case 'e':
		return step2replace(w, "izer", "ize")
	case 'l':
		return step2replace(w, "abli", "able", "alli", "al", "entli", "ent", "eli", "e", "ousli", "ous")
	case 'o':
		return step2replace(w, "ization", "ize", "ation", "ate", "ator", "ate")
	case 's':
		return step2replace(w, "alism", "al", "iveness", "ive", "fulness", "ful", "ousness", "ous")
	case 't':
		return step2replace(w, "aliti", "al", "iviti", "ive", "biliti", "ble")
	}
	return w
}

// step2replace tries suffix/replacement pairs in order, applying the first match where m > 0.
func step2replace(w []byte, pairs ...string) []byte {
	for i := 0; i < len(pairs); i += 2 {
		suffix := pairs[i]
		replacement := pairs[i+1]
		if hasSuffix(w, suffix) {
			stem := w[:len(w)-len(suffix)]
			if measure(stem) > 0 {
				return append(stem, replacement...)
			}
			return w
		}
	}
	return w
}

// --- Step 3: suffix removal (m > 0) ---

func step3(w []byte) []byte {
	if len(w) < 3 {
		return w
	}
	switch w[len(w)-1] {
	case 'e':
		return step3replace(w, "icate", "ic", "ative", "", "alize", "al")
	case 'i':
		return step3replace(w, "iciti", "ic")
	case 'l':
		return step3replace(w, "ical", "ic", "ful", "")
	case 's':
		return step3replace(w, "ness", "")
	}
	return w
}

func step3replace(w []byte, pairs ...string) []byte {
	for i := 0; i < len(pairs); i += 2 {
		suffix := pairs[i]
		replacement := pairs[i+1]
		if hasSuffix(w, suffix) {
			stem := w[:len(w)-len(suffix)]
			if measure(stem) > 0 {
				return append(stem, replacement...)
			}
			return w
		}
	}
	return w
}

// --- Step 4: suffix deletion (m > 1) ---

func step4(w []byte) []byte {
	if len(w) < 3 {
		return w
	}
	switch w[len(w)-2] {
	case 'a':
		return step4delete(w, "al")
	case 'c':
		return step4delete(w, "ance", "ence")
	case 'e':
		return step4delete(w, "er")
	case 'i':
		return step4delete(w, "ic")
	case 'l':
		return step4delete(w, "able", "ible")
	case 'n':
		return step4delete(w, "ant", "ement", "ment", "ent")
	case 'o':
		// Special case: -ion requires preceding s or t.
		if hasSuffix(w, "ion") {
			stem := w[:len(w)-3]
			if len(stem) > 0 && (stem[len(stem)-1] == 's' || stem[len(stem)-1] == 't') {
				if measure(stem) > 1 {
					return stem
				}
			}
			return w
		}
		return step4delete(w, "ou")
	case 's':
		return step4delete(w, "ism")
	case 't':
		return step4delete(w, "ate", "iti")
	case 'u':
		return step4delete(w, "ous")
	case 'v':
		return step4delete(w, "ive")
	case 'z':
		return step4delete(w, "ize")
	}
	return w
}

func step4delete(w []byte, suffixes ...string) []byte {
	for _, suffix := range suffixes {
		if hasSuffix(w, suffix) {
			stem := w[:len(w)-len(suffix)]
			if measure(stem) > 1 {
				return stem
			}
			return w
		}
	}
	return w
}

// --- Step 5a: remove trailing e ---

func step5a(w []byte) []byte {
	if hasSuffix(w, "e") {
		stem := w[:len(w)-1]
		m := measure(stem)
		if m > 1 {
			return stem
		}
		if m == 1 && !endsWithCVC(stem) {
			return stem
		}
	}
	return w
}

// --- Step 5b: ll -> l if m > 1 ---

func step5b(w []byte) []byte {
	if len(w) >= 2 && w[len(w)-1] == 'l' && w[len(w)-2] == 'l' {
		if measure(w) > 1 {
			return w[:len(w)-1]
		}
	}
	return w
}
