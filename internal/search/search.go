// Package search translates a user's search box into an FTS5 MATCH
// expression. Raw FTS5 syntax cannot be handed user input directly: bare
// punctuation such as C++, column filters like title:foo, and operators such
// as NEAR are syntax that FTS5 would try to parse, turning a perfectly
// reasonable query into a syntax error. Parse instead treats every term as
// literal text, quoting it so FTS5 matches it verbatim.
//
// The package is pure -- no I/O, no database -- and has no callers yet.
// Phase 5 wires it into the CLI, and phase 6 into the API.
package search

import (
	"errors"
	"strings"
)

// ErrOnlyExclusions is returned for a query with nothing to match, such as
// "-draft". FTS5 cannot answer "everything except X", and returning zero rows
// would read as a bug rather than as a rejected query.
var ErrOnlyExclusions = errors.New("search query needs at least one term to match")

// term is one token pulled from the raw query: quoted text ready to drop into
// an FTS5 expression, with prefix already appended if the user asked for one.
type term struct {
	quoted string
	negate bool
}

// Parse translates user input into an FTS5 MATCH expression. An empty result
// with a nil error means "no search filter", matching today's behavior for an
// empty q or --search.
//
// Every term is emitted double-quoted, so FTS5 operators a user did not
// intend -- NEAR, AND, column filters like title:foo, bare punctuation such
// as C++ -- are matched as literal text instead of being parsed as syntax or
// raising a syntax error the caller would have to surface as a 500.
//
// Positives and negatives combine as "(pos1 AND pos2) NOT (neg1 OR neg2)".
// The parentheses are load-bearing: FTS5 binds NOT tighter than AND, so the
// unparenthesized "a AND b NOT c" would mean "a AND (b NOT c)".
func Parse(raw string) (string, error) {
	terms := tokenize(raw)

	var positives, negatives []string

	for _, t := range terms {
		if t.negate {
			negatives = append(negatives, t.quoted)
		} else {
			positives = append(positives, t.quoted)
		}
	}

	switch {
	case len(positives) == 0 && len(negatives) == 0:
		return "", nil
	case len(positives) == 0:
		return "", ErrOnlyExclusions
	case len(negatives) == 0:
		return "(" + strings.Join(positives, " AND ") + ")", nil
	default:
		posGroup := "(" + strings.Join(positives, " AND ") + ")"
		negGroup := "(" + strings.Join(negatives, " OR ") + ")"

		return posGroup + " NOT " + negGroup, nil
	}
}

// tokenize walks raw left to right, splitting it into terms on whitespace
// except inside a double-quoted phrase, where whitespace is kept as part of
// the phrase. Applied per token, in order:
//
//  1. a leading "-" marks the term negated, and is stripped;
//  2. a double quote opens a phrase that runs to the next double quote, or to
//     end of input if none follows -- an unterminated quote closes implicitly
//     rather than erroring;
//  3. otherwise the token runs to the next whitespace, and a trailing "*"
//     marks it a prefix match and is stripped;
//  4. whatever text remains is quoted verbatim for FTS5, doubling any
//     embedded quote per FTS5 string literal rules.
//
// A token that reduces to empty text after stripping -- a bare "-" or a bare
// "*" -- contributes nothing; this is how the bare "*" case ends up as "no
// search filter" rather than an empty pair of quotes.
func tokenize(raw string) []term {
	runes := []rune(raw)
	n := len(runes)

	var terms []term

	i := 0
	for i < n {
		for i < n && isSpace(runes[i]) {
			i++
		}
		if i >= n {
			break
		}

		negate := false
		if runes[i] == '-' {
			negate = true
			i++
		}

		var text string

		prefix := false
		if i < n && runes[i] == '"' {
			i++
			start := i
			for i < n && runes[i] != '"' {
				i++
			}
			text = string(runes[start:i])
			if i < n {
				i++ // skip closing quote
			}
		} else {
			start := i
			for i < n && !isSpace(runes[i]) {
				i++
			}
			text = string(runes[start:i])
			prefix = strings.HasSuffix(text, "*")
			text = strings.TrimSuffix(text, "*")
		}

		if text == "" {
			continue // a bare "-", bare "*", or empty phrase contributes nothing
		}

		quoted := quoteFTS5(text)
		if prefix {
			quoted += "*"
		}

		terms = append(terms, term{quoted: quoted, negate: negate})
	}

	return terms
}

// quoteFTS5 wraps text in double quotes for use as an FTS5 string literal,
// doubling any embedded double quote as FTS5 requires.
func quoteFTS5(text string) string {
	return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}
