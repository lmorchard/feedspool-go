package search

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // pure-Go sqlite driver, the same one internal/database uses
)

// The fixture mirrors the shipped index: same column order, same tokenizer, so
// an expression that executes here executes against a real spool.
const (
	fixtureDDL = `CREATE VIRTUAL TABLE items_fts USING fts5(
		title, summary, body,
		tokenize="porter unicode61 remove_diacritics 2"
	)`
	fixtureMatch = `SELECT count(*) FROM items_fts WHERE items_fts MATCH ?`
	// The ranked form the API and the CLI both issue. bm25 is exercised as well
	// as a bare MATCH because ordering by a rank function puts FTS5 into a
	// different code path than counting does.
	fixtureRanked = `SELECT rowid FROM items_fts
		WHERE items_fts MATCH ? ORDER BY bm25(items_fts, 10.0, 4.0, 1.0) LIMIT 5`
	fixtureRowIDs = `SELECT rowid FROM items_fts WHERE items_fts MATCH ? ORDER BY rowid`
)

// adversarialInputs are the shapes a caller can send that parseCases does not
// already cover: FTS5 operators, punctuation-only phrases, quote pile-ups,
// invalid UTF-8, and flat AND chains at the sizes the review probed. Every one
// of these was verified to execute; they are kept so a later change to the
// grammar cannot break them unnoticed.
func adversarialInputs() []string {
	return []string{
		nul + "*",
		"-" + nul,
		`"` + nul + `"`,
		soh + "\x02" + unitSep,
		`""`,
		`"*"`,
		`"+"*`,
		`""""`,
		"^rust",
		"...",
		`"..."`,
		"AND",
		"OR",
		"NOT",
		"(rust)",
		"{title body}:rust",
		"NEAR(rust release, 3)",
		"rust*release",
		"-*",
		`-"release notes"`,
		"\U0001F600",
		// Invalid UTF-8, which []rune folds to U+FFFD.
		"\xff\xfe rust",
		strings.TrimSpace(strings.Repeat("term ", 20)),
		strings.TrimSpace(strings.Repeat("term ", 200)),
		strings.TrimSpace(strings.Repeat("term ", 2000)),
		// A quote-heavy input, where the doubling rule does all the work.
		strings.Repeat(`"`, 64),
	}
}

// TestParseOutputExecutesAgainstFTS5 is the invariant this package exists for:
// whatever Parse returns without an error is a legal FTS5 expression. The
// string comparisons in TestParse pin what the expression looks like; only
// running it can show that FTS5 accepts it.
//
// The NUL cases are why this test exists. SQLite reads a bound MATCH operand as
// a NUL-terminated C string, so an embedded U+0000 used to truncate the
// expression mid-literal and raise "unterminated string", which the API
// surfaced as a 500 -- exactly the failure this package promises to prevent.
func TestParseOutputExecutesAgainstFTS5(t *testing.T) {
	db := newFTS5Fixture(t)

	cases := parseCases()
	inputs := make([]string, 0, len(cases)+len(controlCharacterVariants())+len(adversarialInputs()))
	for _, c := range cases {
		inputs = append(inputs, c.in)
	}
	inputs = append(inputs, controlCharacterVariants()...)
	inputs = append(inputs, adversarialInputs()...)

	for _, in := range inputs {
		t.Run(subtestName(in), func(t *testing.T) {
			expr, err := Parse(in)
			if err != nil {
				// Only the documented rejection is acceptable here.
				if !errors.Is(err, ErrOnlyExclusions) {
					t.Fatalf("Parse(%q) returned an unexpected error: %v", in, err)
				}
				return
			}
			if expr == "" {
				return // no search filter, so there is nothing to run
			}
			assertExecutes(t, db, in, expr)
		})
	}
}

// TestControlCharacterVariantsMatchTheBareTerm is the behavioral half of the
// fix: folding a control character to whitespace is only correct if the term
// still finds the same rows.
func TestControlCharacterVariantsMatchTheBareTerm(t *testing.T) {
	db := newFTS5Fixture(t)

	baseline := matchedRowIDs(t, db, termRust)
	if len(baseline) == 0 {
		t.Fatalf("fixture has no rows matching %q", termRust)
	}

	for _, in := range controlCharacterVariants() {
		t.Run(subtestName(in), func(t *testing.T) {
			if got := matchedRowIDs(t, db, in); !slices.Equal(got, baseline) {
				t.Errorf("Parse(%q) matched %v, want %v -- the same rows as %q",
					in, got, baseline, termRust)
			}
		})
	}
}

// assertExecutes runs the expression both as a bare MATCH and through bm25.
func assertExecutes(t *testing.T, db *sql.DB, in, expr string) {
	t.Helper()

	var count int
	if err := db.QueryRow(fixtureMatch, expr).Scan(&count); err != nil {
		t.Fatalf("MATCH %q (from %q) failed: %v", expr, in, err)
	}

	rows, err := db.Query(fixtureRanked, expr)
	if err != nil {
		t.Fatalf("ranked MATCH %q (from %q) failed: %v", expr, in, err)
	}
	defer rows.Close()
	for rows.Next() {
		var rowid int64
		if err := rows.Scan(&rowid); err != nil {
			t.Fatalf("scan for %q failed: %v", expr, err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("ranked MATCH %q (from %q) failed mid-iteration: %v", expr, in, err)
	}
}

// matchedRowIDs parses the input and returns the rowids it matches, in rowid
// order so two queries are comparable.
func matchedRowIDs(t *testing.T, db *sql.DB, in string) []int64 {
	t.Helper()

	expr, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse(%q) returned error: %v", in, err)
	}
	if expr == "" {
		t.Fatalf("Parse(%q) produced no expression", in)
	}

	rows, err := db.Query(fixtureRowIDs, expr)
	if err != nil {
		t.Fatalf("MATCH %q (from %q) failed: %v", expr, in, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var rowid int64
		if err := rows.Scan(&rowid); err != nil {
			t.Fatalf("scan for %q failed: %v", expr, err)
		}
		ids = append(ids, rowid)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("MATCH %q (from %q) failed mid-iteration: %v", expr, in, err)
	}
	return ids
}

// newFTS5Fixture builds a small in-memory index with the shipped column layout
// and tokenizer. The pool is capped at one connection because a modernc
// ":memory:" database belongs to its connection rather than to the pool.
func newFTS5Fixture(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open the fixture database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(fixtureDDL); err != nil {
		t.Fatalf("failed to create the fixture index: %v", err)
	}

	rows := [][3]string{
		{"Rust 1.90 release notes", "What changed", "The rust release notes cover security fixes."},
		{"C++ tips", "Assorted pointers", "Pointer arithmetic in C++ and other unsafe things."},
		{"title:foo is not a column filter", "NEAR and OR as words", "A draft about nothing at all."},
		{"Kubernetes digest", "Cluster notes", "term term term secure rollouts."},
	}
	for _, row := range rows {
		if _, err := db.Exec(
			`INSERT INTO items_fts(title, summary, body) VALUES (?, ?, ?)`,
			row[0], row[1], row[2],
		); err != nil {
			t.Fatalf("failed to seed the fixture index: %v", err)
		}
	}

	// This fixture is an ordinary FTS5 table rather than an external-content
	// one, so there is no content table for the strong check to read back and
	// the bare form is the only one that applies. internal/database uses the
	// strong form, ('integrity-check', 1), against the real index.
	if _, err := db.Exec(`INSERT INTO items_fts(items_fts) VALUES('integrity-check')`); err != nil {
		t.Fatalf("fixture integrity-check failed: %v", err)
	}
	return db
}

// subtestName keeps control characters and very long inputs out of test names.
func subtestName(in string) string {
	name := fmt.Sprintf("%q", in)
	const maxNameLength = 48
	if len(name) > maxNameLength {
		name = name[:maxNameLength] + fmt.Sprintf("...(%d bytes)", len(in))
	}
	return name
}
