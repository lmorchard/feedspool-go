package topics

import (
	"regexp"
	"strings"
)

// languageTokens are the language names the label guard treats as specific.
// It is deliberately a small list; add names here when a false positive shows.
var languageTokens = []string{
	"rust", "golang", "go", "python", "javascript", "typescript", "java",
	"c++", "c#", "ruby", "php", "swift", "kotlin", "scala", "elixir",
	"erlang", "haskell", "ocaml", "clojure", "zig", "nim", "mojo", "lua",
	"perl", "julia", "dart", "sql", "bash", "shell",
}

var wordBoundary = regexp.MustCompile(`[^a-z0-9+#]+`)

func containsWord(text, word string) bool {
	for _, token := range wordBoundary.Split(strings.ToLower(text), -1) {
		if token == word {
			return true
		}
	}
	return false
}

// GuardLabel generalizes a label that names a programming language when the
// sampled items do not all mention that language. The labeler sees only a few
// items, so one salient language can dominate a mixed software cluster.
func GuardLabel(label string, titles []string, generic string) (string, bool) {
	for _, language := range languageTokens {
		if !containsWord(label, language) {
			continue
		}
		all := len(titles) > 0
		for _, title := range titles {
			if !containsWord(title, language) {
				all = false
				break
			}
		}
		if all {
			return label, false
		}
		if strings.TrimSpace(generic) == "" {
			generic = "Software Development"
		}
		return generic, true
	}
	return label, false
}
