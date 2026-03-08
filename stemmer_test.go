package main

import "testing"

// Reference test cases from the original Porter stemmer page:
// https://tartarus.org/martin/PorterStemmer/
func TestStem_PorterReference(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		// Short words — unchanged.
		{"a", "a"},
		{"be", "be"},
		{"go", "go"},

		// Step 1a: plurals.
		{"caresses", "caress"},
		{"ponies", "poni"},
		{"ties", "ti"},
		{"caress", "caress"},
		{"cats", "cat"},

		// Step 1b: past tense / gerund.
		{"feed", "feed"},
		{"agreed", "agre"},
		{"plastered", "plaster"},
		{"bled", "bled"},
		{"motoring", "motor"},
		{"sing", "sing"},
		{"conflated", "conflat"},
		{"troubled", "troubl"},
		{"sized", "size"},
		{"hopping", "hop"},
		{"tanned", "tan"},
		{"falling", "fall"},
		{"hissing", "hiss"},
		{"fizzing", "fizz"},
		{"failing", "fail"},
		{"filing", "file"},

		// Step 1c: y -> i.
		{"happy", "happi"},
		{"sky", "sky"},

		// Step 2: context-sensitive suffix replacement.
		{"relational", "relat"},
		{"conditional", "condit"},
		{"rational", "ration"},
		{"valenci", "valenc"},
		{"hesitanci", "hesit"},
		{"digitizer", "digit"},
		{"conformabli", "conform"},
		{"radicalli", "radic"},
		{"differentli", "differ"},
		{"vileli", "vile"},
		{"analogousli", "analog"},
		{"vietnamization", "vietnam"},
		{"predication", "predic"},
		{"operator", "oper"},
		{"feudalism", "feudal"},
		{"decisiveness", "decis"},
		{"hopefulness", "hope"},
		{"callousness", "callous"},
		{"formaliti", "formal"},
		{"sensitiviti", "sensit"},
		{"sensibiliti", "sensibl"},

		// Step 3: suffix removal.
		{"triplicate", "triplic"},
		{"formative", "form"},
		{"formalize", "formal"},
		{"electriciti", "electr"},
		{"electrical", "electr"},
		{"hopeful", "hope"},
		{"goodness", "good"},

		// Step 4: suffix deletion.
		{"revival", "reviv"},
		{"allowance", "allow"},
		{"inference", "infer"},
		{"airliner", "airlin"},
		{"adjustable", "adjust"},
		{"defensible", "defens"},
		{"irritant", "irrit"},
		{"replacement", "replac"},
		{"adjustment", "adjust"},
		{"dependent", "depend"},
		{"adoption", "adopt"},
		{"homologou", "homolog"},
		{"communism", "commun"},
		{"activate", "activ"},
		{"angulariti", "angular"},
		{"homologous", "homolog"},
		{"effective", "effect"},
		{"bowdlerize", "bowdler"},

		// Step 5: cleanup.
		{"probate", "probat"},
		{"rate", "rate"},
		{"cease", "ceas"},
		{"roll", "roll"},
		{"controll", "control"},

		// Real-world words commonly encountered in llmem usage.
		{"databases", "databas"},
		{"database", "databas"},
		{"testing", "test"},
		{"tests", "test"},
		{"running", "run"},
		{"improvements", "improv"},
		{"improvement", "improv"},
		{"recommended", "recommend"},
		{"recommendations", "recommend"},
		{"programming", "program"},
		{"configured", "configur"},
		{"configuration", "configur"},
		{"architecture", "architectur"},
		{"architectural", "architectur"},
		{"deployment", "deploy"},
		{"deploying", "deploi"},
		{"preferences", "prefer"},
		{"preferred", "prefer"},
	}
	for _, tc := range cases {
		got := stem(tc.input)
		if got != tc.want {
			t.Errorf("stem(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestStem_EmptyAndShort(t *testing.T) {
	if got := stem(""); got != "" {
		t.Errorf("stem(\"\") = %q, want \"\"", got)
	}
	if got := stem("x"); got != "x" {
		t.Errorf("stem(\"x\") = %q, want \"x\"", got)
	}
	if got := stem("ab"); got != "ab" {
		t.Errorf("stem(\"ab\") = %q, want \"ab\"", got)
	}
}

// TestStem_MatchingForms verifies that different inflections of the same
// word produce the same stem (the core property we need for search).
func TestStem_MatchingForms(t *testing.T) {
	groups := [][]string{
		{"database", "databases"},
		{"test", "testing", "tests", "tested"},
		{"run", "running", "runs"},
		{"improve", "improvement", "improvements", "improving"},
		{"recommend", "recommended", "recommendations", "recommending"},
		{"program", "programming", "programmed", "programs"},
		{"configure", "configured", "configuration", "configuring"},
		{"prefer", "preferred", "preferences", "preferring"},
	}
	for _, group := range groups {
		stems := make(map[string]bool)
		for _, w := range group {
			stems[stem(w)] = true
		}
		if len(stems) != 1 {
			strs := make([]string, 0, len(group))
			for _, w := range group {
				strs = append(strs, w+"->"+stem(w))
			}
			t.Errorf("inflections do not converge: %v", strs)
		}
	}
}

func BenchmarkStem(b *testing.B) {
	words := []string{
		"databases", "testing", "running", "improvements", "recommended",
		"programming", "configured", "architecture", "deployment", "preferences",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, w := range words {
			stem(w)
		}
	}
}
