package search

import (
	"errors"
	"testing"
)

// releaseNotesWant is the expected output for both a well-formed phrase and
// an unterminated one: an unterminated quote closes implicitly at end of
// input, so it produces the same expression as the closed phrase.
const releaseNotesWant = `("release notes")`

// termRust is the sample term shared by the cases below, the
// control-character variants, and the FTS5 fixture in execution_test.go.
const termRust = "rust"

// The control characters the variants are built from. Only nul actually breaks
// FTS5 -- see tokenize -- but the rule covers the whole C0 block, so the other
// two ride along to show it.
const (
	nul     = "\x00"
	soh     = "\x01"
	unitSep = "\x1f"
)

// parseCase is one input and the expression Parse has to produce for it.
type parseCase struct{ name, in, want string }

// parseCases is the shared corpus. TestParse asserts the expression each input
// produces; TestParseOutputExecutesAgainstFTS5 runs those same expressions
// against a real FTS5 index, so an input cannot be pinned in one test and
// forgotten in the other.
func parseCases() []parseCase {
	return []parseCase{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"single term", termRust, `("rust")`},
		{"implicit and", "rust release", `("rust" AND "release")`},
		{"phrase", `"release notes"`, releaseNotesWant},
		{"exclusion", "rust -draft", `("rust") NOT ("draft")`},
		{"prefix", "secur*", `("secur"*)`},
		{"operator as literal", "foo NEAR bar", `("foo" AND "NEAR" AND "bar")`},
		{"column filter as literal", "title:foo", `("title:foo")`},
		{"punctuation", "C++", `("C++")`},
		{"embedded quote", `say "hi" there`, `("say" AND "hi" AND "there")`},
		{"unterminated quote", `"release notes`, releaseNotesWant},
		{"bare star", "*", ""},
		{"embedded double quote in word", `say"quote"word`, `("say""quote""word")`},
		// Control characters separate terms rather than reaching FTS5, where a
		// NUL would truncate the bound expression mid-literal.
		{"nul only", nul, ""},
		{"nul inside a term", "ru" + nul + "st", `("ru" AND "st")`},
		{"nul inside a phrase", `"release` + nul + `notes"`, releaseNotesWant},
	}
}

// controlCharacterVariants are termRust with control characters glued on. Each
// has to behave exactly as the bare term does.
func controlCharacterVariants() []string {
	return []string{
		nul + termRust,
		termRust + nul,
		nul + termRust + nul,
		soh + termRust + unitSep,
	}
}

func TestParse(t *testing.T) {
	for _, c := range parseCases() {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.in)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("Parse(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// A control character glued to a term is a separator, not text, so the term is
// found as though it were not there.
func TestParseFoldsControlCharactersToWhitespace(t *testing.T) {
	want := `("` + termRust + `")`
	for _, in := range controlCharacterVariants() {
		t.Run(subtestName(in), func(t *testing.T) {
			got, err := Parse(in)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", in, err)
			}
			if got != want {
				t.Errorf("Parse(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestParseRejectsOnlyExclusions(t *testing.T) {
	if _, err := Parse("-draft -wip"); !errors.Is(err, ErrOnlyExclusions) {
		t.Fatalf("err = %v, want ErrOnlyExclusions", err)
	}
}
