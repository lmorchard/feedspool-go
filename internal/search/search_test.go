package search

import (
	"errors"
	"testing"
)

// releaseNotesWant is the expected output for both a well-formed phrase and
// an unterminated one: an unterminated quote closes implicitly at end of
// input, so it produces the same expression as the closed phrase.
const releaseNotesWant = `("release notes")`

func TestParse(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"single term", "rust", `("rust")`},
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
	}

	for _, c := range cases {
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

func TestParseRejectsOnlyExclusions(t *testing.T) {
	if _, err := Parse("-draft -wip"); !errors.Is(err, ErrOnlyExclusions) {
		t.Fatalf("err = %v, want ErrOnlyExclusions", err)
	}
}
