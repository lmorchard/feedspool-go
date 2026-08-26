# Full-Text Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:subagent-driven-development`
> (recommended) or `superpowers:executing-plans` to implement this plan phase by
> phase. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace title-substring search with FTS5 over title + summary + body,
ranked by `bm25()`, on both `feedspool items --search` and
`GET /api/v1/items?q=`, leaving behind a canonical item-text substrate for #30.

**Approach:** A Go function derives HTML-stripped text into an `item_text`
table; an external-content `items_fts` virtual table indexes it, maintained by
triggers on `item_text`. Go owns inserts and updates (stripping is Go code);
triggers own deletes, which is what reaches the `ON DELETE CASCADE` path that
`DeleteFeed` uses. Search becomes one more `WHERE` condition in both query
builders.

**Tech stack:** Go 1.25, `modernc.org/sqlite` (FTS5, `CGO_ENABLED=0`),
`golang.org/x/net/html`, Cobra.

**Spec:** `docs/dev-sessions/2026-08-26-1247-fts5-search/spec.md`
**Research:** `docs/dev-sessions/2026-08-26-1247-fts5-search/research.md`

## Global constraints

- `make build`, never `go build`. `CGO_ENABLED=0` is load-bearing — no cgo, no
  C extensions, no new SQLite driver.
- `make format && make lint && make test` after every phase. `make lint` uses
  the golangci-lint version pinned in the Makefile; do not install another.
- Lint is strict about `goconst` (hoist repeated string literals, including in
  tests) and `gochecknoglobals` (package-level `var` slices/maps become
  functions). Budget a cleanup pass in each phase that adds a package.
- `internal/` gets tests; `cmd/` does not need them.
- One commit per phase, message `Phase N: <name>`.
- The CLI and the API must never disagree about what a query matches. Every
  phase that touches matching touches both, or explains why not.

---

## Phase 1: `internal/itemtext` — canonical text derivation

The one function that answers "what text represents this item?". Pure, no
database. #30's embedder is intended to call the same function with a smaller
truncation cap.

**Files:**
- Create: `internal/itemtext/itemtext.go`
- Test: `internal/itemtext/itemtext_test.go`

**Key changes:**

```go
package itemtext

// Generator and Version identify this derivation in item_text bookkeeping.
// Bump Version whenever the output of Derive changes for the same input --
// that is what forces a reindex.
const (
	Generator = "itemtext"
	Version   = 1
)

// DefaultMaxBodyBytes is a bloat guard, not a recall limit: long-form articles
// run well under it. #30 will pass a far smaller cap for embedding token
// limits, which is why this is an option rather than a constant in Derive.
const (
	DefaultMaxTitleBytes   = 4 * 1024
	DefaultMaxSummaryBytes = 32 * 1024
	DefaultMaxBodyBytes    = 256 * 1024
)

type Options struct {
	MaxTitleBytes   int
	MaxSummaryBytes int
	MaxBodyBytes    int
}

func DefaultOptions() Options

type Text struct {
	Title   string
	Summary string
	Body    string
}

// Derive strips HTML, decodes entities, collapses whitespace and truncates.
func Derive(title, summary, content string, opts Options) Text

// SourceHash fingerprints the raw inputs so an unchanged item on re-fetch is a
// string comparison rather than an HTML parse.
func SourceHash(title, summary, content string) string
```

Stripping uses `html.NewTokenizer` streaming, not goquery: this runs over the
whole corpus inside a migration, and goquery builds a document tree per item.

```go
func stripHTML(raw string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	var out strings.Builder
	skipDepth := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken: // includes io.EOF
			return strings.Join(strings.Fields(out.String()), " ")
		case html.StartTagToken:
			if name, _ := tokenizer.TagName(); isSkippedElement(name) {
				skipDepth++
			}
		case html.EndTagToken:
			if name, _ := tokenizer.TagName(); isSkippedElement(name) && skipDepth > 0 {
				skipDepth--
			}
		case html.TextToken:
			if skipDepth == 0 {
				out.Write(tokenizer.Text()) // already entity-decoded
				out.WriteByte(' ')
			}
		}
	}
}

// isSkippedElement drops elements whose text content is markup, not prose.
func isSkippedElement(name []byte) bool {
	switch string(name) {
	case "script", "style", "noscript", "template":
		return true
	}
	return false
}
```

`SourceHash` is `sha256` over `title + "\x00" + summary + "\x00" + content`,
hex-encoded and truncated to 32 characters — the same truncated-hash idiom as
`ItemHashID` in `internal/database/pagination.go:29`.

Truncation cuts at the byte cap then backs off to a UTF-8 rune boundary with
`utf8.DecodeLastRuneInString`, so a multi-byte character is never split.

- [ ] **Step 1: Write the failing tests**

```go
func TestDeriveStripsMarkup(t *testing.T) {
	got := Derive(
		"Rust 1.87 &amp; friends",
		"<p>A <b>short</b> summary</p>",
		`<div>Hello <script>var x = "networking";</script> world</div>`,
		DefaultOptions(),
	)
	if got.Title != "Rust 1.87 & friends" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Summary != "A short summary" {
		t.Errorf("summary = %q", got.Summary)
	}
	if got.Body != "Hello world" {
		t.Errorf("body = %q, script contents must not be indexed", got.Body)
	}
}

func TestDeriveTruncatesOnRuneBoundary(t *testing.T) {
	opts := DefaultOptions()
	opts.MaxBodyBytes = 5
	got := Derive("", "", "aé😀bcdef", opts)
	if !utf8.ValidString(got.Body) {
		t.Fatalf("body %q is not valid UTF-8", got.Body)
	}
	if len(got.Body) > opts.MaxBodyBytes {
		t.Fatalf("body is %d bytes, cap is %d", len(got.Body), opts.MaxBodyBytes)
	}
}

func TestSourceHashDistinguishesFieldBoundaries(t *testing.T) {
	// Without a separator these two would hash identically.
	if SourceHash("ab", "c", "") == SourceHash("a", "bc", "") {
		t.Fatal("hash ignores the boundary between title and summary")
	}
	if SourceHash("a", "b", "c") != SourceHash("a", "b", "c") {
		t.Fatal("hash is not stable")
	}
}
```

Plus table-driven cases for: plain text with no markup passes through; `a < b`
survives as text; entity-only input; empty input yields empty fields;
whitespace and newlines collapse to single spaces; unclosed tags do not consume
the rest of the document.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/itemtext/ -v`
Expected: build failure — package does not exist.

- [ ] **Step 3: Implement `internal/itemtext/itemtext.go`**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/itemtext/ -v`

**Verification — automated:**
- [ ] `go test ./internal/itemtext/ -v` passes
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes

**Verification — manual:**
- [ ] Read the derived output for one real article body (paste one into a
      scratch test and `t.Log` it). Confirm it reads as prose, with no leftover
      tag names, attribute values, or URL fragments.

---

## Phase 2: Migration 11 — schema, triggers, backfill runner, `feedspool reindex`

Delivers a database whose index exists and is correct: existing corpora get
backfilled, and every delete path is covered by triggers. Nothing searches it
yet.

**Files:**
- Create: `internal/database/item_text.go`
- Create: `internal/database/backfill.go`
- Create: `internal/database/item_text_test.go`
- Create: `internal/database/backfill_test.go`
- Create: `cmd/reindex.go`
- Modify: `internal/database/migrations.go` — add `migrationVersion11`, bump
  `maxMigrationVersion` to it, add DDL to `getMigrations()`, add
  `applyMigration11` and its `applySpecificMigration` case
- Modify: `internal/database/schema.sql` — same DDL, `IF NOT EXISTS`, matching
  the `item_annotations` precedent where schema.sql and migration 6 both
  declare the table
- Modify: `internal/database/migrations_test.go` — the three
  `maxMigrationVersion` assertions at lines 228, 297, 367 follow the constant,
  so they need no edit; confirm they still pass

**Key changes — DDL** (identical text in `getMigrations()` and `schema.sql`):

```sql
CREATE TABLE IF NOT EXISTS item_text (
    item_id           INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    title             TEXT NOT NULL DEFAULT '',
    summary           TEXT NOT NULL DEFAULT '',
    body              TEXT NOT NULL DEFAULT '',
    source_hash       TEXT NOT NULL,
    generator         TEXT NOT NULL,
    generator_version INTEGER NOT NULL,
    computed_at       DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_item_text_generator
    ON item_text(generator, generator_version);

CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
    title, summary, body,
    content='item_text', content_rowid='item_id',
    tokenize="porter unicode61 remove_diacritics 2"
);

CREATE TRIGGER IF NOT EXISTS item_text_ai AFTER INSERT ON item_text BEGIN
    INSERT INTO items_fts(rowid, title, summary, body)
    VALUES (new.item_id, new.title, new.summary, new.body);
END;
CREATE TRIGGER IF NOT EXISTS item_text_ad AFTER DELETE ON item_text BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, summary, body)
    VALUES ('delete', old.item_id, old.title, old.summary, old.body);
END;
CREATE TRIGGER IF NOT EXISTS item_text_au AFTER UPDATE ON item_text BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, summary, body)
    VALUES ('delete', old.item_id, old.title, old.summary, old.body);
    INSERT INTO items_fts(rowid, title, summary, body)
    VALUES (new.item_id, new.title, new.summary, new.body);
END;
```

The `'delete'` command must carry the **old** column values: an
external-content table cannot recover them from `item_text`, which by then
holds the new ones.

**Key changes — the reusable runner** (`internal/database/backfill.go`):

```go
// DerivedBackfill describes an artifact derived from items that must be
// recomputed when the item changes or the generator's version moves. #58
// implements it for FTS text; #30 implements it for embeddings.
type DerivedBackfill interface {
	Name() string
	Version() int
	// NextBatch returns up to limit item IDs still needing work, all with
	// id > afterID, in ascending id order.
	NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error)
	// Recompute derives and stores the artifact for those item IDs.
	Recompute(tx *sql.Tx, ids []int64) error
	// Remaining reports how many items still need work, for progress output.
	Remaining(tx *sql.Tx) (int64, error)
}

const defaultBackfillBatchSize = 500

// RunBackfill processes the generator in committed batches, so an interrupted
// run resumes where it stopped instead of restarting, and a large database is
// never held in one transaction.
func (db *DB) RunBackfill(gen DerivedBackfill, batchSize int, progress func(done, total int64)) error
```

`RunBackfill` walks by ascending item ID: begin tx, `NextBatch(tx, afterID,
batchSize)`, return if empty, `Recompute`, commit, set `afterID` to the largest
ID in the batch. The ID cursor is what makes the loop terminate even if a row
somehow stays stale — without it, a row the generator cannot fix would be
re-selected forever.

**Key changes — the FTS text generator** (`internal/database/item_text.go`):

```go
type itemTextBackfill struct{ opts itemtext.Options }

func (g *itemTextBackfill) Name() string { return itemtext.Generator }
func (g *itemTextBackfill) Version() int { return itemtext.Version }
```

Its `NextBatch` query — staleness is "no row, or a row from an older
generator version". A changed item is handled on the live write path in phase
3, which is the only thing that changes item text, so the backfill does not
need to re-hash every row on every run:

```sql
SELECT i.id
FROM items i LEFT JOIN item_text t ON t.item_id = i.id
WHERE i.id > ? AND (t.item_id IS NULL OR t.generator <> ? OR t.generator_version <> ?)
ORDER BY i.id
LIMIT ?
```

`Recompute` reads `title, summary, content` for those IDs, calls
`itemtext.Derive` and `itemtext.SourceHash`, and writes each through the one
shared helper. Phase 3's live write path calls the same helper — two spellings
of this statement is exactly the drift the spec exists to prevent:

```go
// upsertItemTextTx is the only place item_text rows are written. The backfill
// runner and UpsertItem both go through it.
func upsertItemTextTx(tx *sql.Tx, itemID int64, text itemtext.Text, sourceHash string) error {
	_, err := tx.Exec(`
		INSERT INTO item_text
			(item_id, title, summary, body, source_hash, generator, generator_version, computed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			title = excluded.title, summary = excluded.summary, body = excluded.body,
			source_hash = excluded.source_hash, generator = excluded.generator,
			generator_version = excluded.generator_version, computed_at = excluded.computed_at`,
		itemID, text.Title, text.Summary, text.Body, sourceHash,
		itemtext.Generator, itemtext.Version, formatDatabaseTime(time.Now().UTC()),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert item text for item %d: %w", itemID, err)
	}
	return nil
}
```

Exported entry point, shared by the migration and the command:

```go
// ReindexItemText brings the derived text and search index up to date. force
// discards every derived row first, which the triggers turn into a full index
// clear, so a tokenizer change can be applied without a schema migration.
func (db *DB) ReindexItemText(force bool, progress func(done, total int64)) error
```

**Key changes — `applyMigration11`:**

Three stages, deliberately not one transaction:

1. DDL in its own transaction. Idempotent (`IF NOT EXISTS` throughout).
2. `ReindexItemText(false, progressLogger)` — batched, committed per batch.
3. `INSERT INTO schema_migrations (version) VALUES (11)` in a final small
   transaction.

The version is recorded **last** on purpose: it then means "fully indexed". An
interrupted migration leaves the version unbumped, so the next run re-applies
the idempotent DDL and the backfill resumes from where it stopped. Bumping the
version first would mark a partially indexed database as done, and search would
silently miss items forever.

Follow the transaction shape of `applyMigration9` at
`internal/database/migrations.go:402` (`committed` flag plus deferred rollback).

**Key changes — `cmd/reindex.go`:**

Follow the structure of `cmd/status.go`: `database.New`, `IsInitialized`,
`defer db.Close()`. Flag `--force` (bool, "Discard and rebuild every derived
row, e.g. after a tokenizer change"). Progress to logrus at info level.

- [ ] **Step 1: Write the failing tests**

The maintenance matrix is the important one — an external-content index fails
*silently*, so every case asserts `('integrity-check')` afterward.

```go
// integrityCheck fails the test if the FTS index disagrees with item_text.
// External-content tables do not self-maintain, so this is the only assertion
// that catches a missing trigger.
func integrityCheck(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.conn.Exec(`INSERT INTO items_fts(items_fts) VALUES('integrity-check')`); err != nil {
		t.Fatalf("fts integrity-check failed: %v", err)
	}
}

func TestItemTextTriggersCoverEveryDeletePath(t *testing.T) {
	cases := []struct {
		name     string
		act      func(t *testing.T, db *DB)
		wantRows int // expected rows left in items_fts, from a seed of 2
	}{
		{"direct delete", func(t *testing.T, db *DB) {
			mustExec(t, db, `DELETE FROM item_text WHERE item_id = ?`, firstItemID(t, db))
		}, 1},
		{"one-level cascade from items", func(t *testing.T, db *DB) {
			mustExec(t, db, `DELETE FROM items WHERE id = ?`, firstItemID(t, db))
		}, 1},
		{"two-level cascade from feeds", func(t *testing.T, db *DB) {
			// feeds -> items -> item_text -> trigger. Verified to fire at this
			// depth; DeleteFeed reaches the index no other way.
			if err := db.DeleteFeed(testFeedURL); err != nil {
				t.Fatal(err)
			}
		}, 0},
		{"archived purge", func(t *testing.T, db *DB) {
			mustExec(t, db, `UPDATE items SET archived = 1`)
			if _, err := db.DeleteArchivedItems(time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}, 0},
		{"marking archived indexes nothing away", func(t *testing.T, db *DB) {
			// archived is a filter on items, not a text change, so the index
			// must be untouched.
			if err := db.MarkItemsArchived(testFeedURL, nil); err != nil {
				t.Fatal(err)
			}
		}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := seedTwoIndexedItems(t)
			tc.act(t, db)
			if got := countFTSRows(t, db); got != tc.wantRows {
				t.Errorf("items_fts has %d rows, want %d", got, tc.wantRows)
			}
			integrityCheck(t, db)
		})
	}
}

func TestItemTextUpdateRetiresStaleTerms(t *testing.T) {
	// Update a body; the old body term must stop matching, the untouched title
	// term must keep matching, and the new body term must match.
	// Regression guard: an update trigger that inserts without deleting leaves
	// both the old and new terms searchable, and integrity-check still passes.
}

func TestRunBackfillIsResumable(t *testing.T) {
	// Seed 25 items, run with batchSize 10 and a progress hook that returns
	// after the first batch; assert 10 indexed. Re-run to completion; assert
	// 25 indexed, no duplicates, integrityCheck.
}

func TestReindexItemTextRecomputesOnVersionBump(t *testing.T) {
	// Backfill, then rewrite every generator_version to itemtext.Version - 1,
	// then re-run: every row is recomputed and computed_at moves.
}

func TestReindexItemTextForceRebuilds(t *testing.T) {
	// Corrupt item_text by hand (UPDATE body to junk), run with force=true,
	// assert the derived text matches itemtext.Derive output again.
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/database/ -run 'ItemText|Backfill|Reindex' -v`
Expected: FAIL — `item_text` does not exist.

- [ ] **Step 3: Implement the DDL, `backfill.go`, `item_text.go`, migration 11, `cmd/reindex.go`**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/database/ -run 'ItemText|Backfill|Reindex|Migration' -v`

- [ ] **Step 5: Commit** — `Phase 2: FTS5 schema, triggers, and backfill`

**Verification — automated:**
- [ ] `go test ./internal/database/ -v` passes
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `make build` succeeds

**Verification — manual:**
- [ ] Copy a real feed database, run the built binary against it, and watch
      migration 11 apply. Note the wall-clock time and the item count.
- [ ] `sqlite3 <copy> "INSERT INTO items_fts(items_fts) VALUES('integrity-check');"`
      returns without error.
- [ ] Compare `SELECT count(*) FROM items` and `SELECT count(*) FROM item_text`
      — they must be equal.
- [ ] Check the on-disk size delta against the pre-migration copy, and record
      it in `notes.md` alongside the 81%-of-source figure from `research.md`.

---

## Phase 3: `UpsertItem` keeps the index current

Delivers end-to-end freshness for new data: a fetched item is searchable
immediately, and an unchanged item on re-fetch costs a hash comparison rather
than an HTML parse.

**Files:**
- Modify: `internal/database/item_repository.go:29-59` — `UpsertItem`
- Modify: `internal/database/item_repository_test.go`

**Key changes:**

`UpsertItem` becomes transactional and returns the item ID from the write so
the derived row can be attached to it:

```go
// RETURNING is required here: LastInsertId is not meaningful for the
// ON CONFLICT DO UPDATE branch, which is the common case on re-fetch.
query := `
	INSERT INTO items (feed_url, guid, title, link, published_date, first_seen,
		content, summary, archived, item_json)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(feed_url, guid) DO UPDATE SET
		title = excluded.title,
		link = excluded.link,
		content = excluded.content,
		summary = excluded.summary,
		archived = excluded.archived,
		item_json = excluded.item_json
	RETURNING id
`
```

Then, in the same transaction:

```go
// Skip the derivation entirely when nothing that feeds the index changed.
// Re-fetching an unchanged feed is the common case, and this keeps it to one
// indexed lookup instead of an HTML parse per item. It also avoids firing the
// update trigger, which would delete and reinsert identical index rows.
hash := itemtext.SourceHash(item.Title, item.Summary, item.Content)
var storedHash string
var storedVersion int
err := tx.QueryRow(
	`SELECT source_hash, generator_version FROM item_text WHERE item_id = ?`, id,
).Scan(&storedHash, &storedVersion)
switch {
case errors.Is(err, sql.ErrNoRows): // fall through to derive
case err != nil:
	return err
case storedHash == hash && storedVersion == itemtext.Version:
	return tx.Commit() // nothing to do
}
```

Reuse the same upsert statement as `Recompute` in phase 2 — extract it to a
shared helper `upsertItemTextTx(tx *sql.Tx, id int64, text itemtext.Text, hash string) error`
in `internal/database/item_text.go` and call it from both. Two spellings of
this statement is exactly the drift the spec is trying to prevent.

- [ ] **Step 1: Write the failing tests**

```go
func TestUpsertItemIndexesNewItem(t *testing.T) {
	// Upsert an item with markup in its content; assert a matching item_text
	// row exists with stripped text, and that an FTS MATCH on a body-only term
	// finds it. integrityCheck.
}

func TestUpsertItemSkipsDerivationWhenUnchanged(t *testing.T) {
	// Upsert twice with identical content; assert computed_at is unchanged
	// after the second call -- proving the short-circuit fired rather than
	// silently re-deriving.
}

func TestUpsertItemReindexesChangedContent(t *testing.T) {
	// Upsert, then upsert the same feed_url/guid with different content.
	// Assert the old body term no longer matches and the new one does.
	// integrityCheck.
}

func TestUpsertItemReindexesOnVersionBump(t *testing.T) {
	// Upsert, hand-set generator_version to itemtext.Version - 1, upsert the
	// same unchanged item; assert it was recomputed despite the hash matching.
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/database/ -run TestUpsertItem -v`

- [ ] **Step 3: Implement the `UpsertItem` change**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/database/ ./internal/fetcher/ -v`

- [ ] **Step 5: Commit** — `Phase 3: Maintain the search index on item writes`

**Verification — automated:**
- [ ] `go test ./internal/database/ ./internal/fetcher/ -v` passes
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes

**Verification — manual:**
- [ ] Against the real-database copy: `feedspool fetch` one feed, then confirm
      `SELECT count(*) FROM items` still equals `SELECT count(*) FROM item_text`
      and integrity-check passes.
- [ ] Run the same fetch twice and confirm the second is not noticeably slower
      than it was before this phase.

---

## Phase 4: `internal/search` — the query parser

Delivers a safe translation from a user's search box to an FTS5 `MATCH`
expression. Standalone and pure; nothing calls it yet.

**Files:**
- Create: `internal/search/search.go`
- Test: `internal/search/search_test.go`

**Key changes:**

```go
package search

// ErrOnlyExclusions is returned for a query with nothing to match, such as
// "-draft". FTS5 cannot answer "everything except X", and returning zero rows
// would read as a bug rather than as a rejected query.
var ErrOnlyExclusions = errors.New("search query needs at least one term to match")

// Parse translates user input into an FTS5 MATCH expression. An empty result
// with a nil error means "no search filter", matching today's behavior for an
// empty q or --search.
//
// Every term is emitted double-quoted, so FTS5 operators a user did not intend
// -- NEAR, AND, column filters like title:foo, bare punctuation such as C++ --
// are matched as literal text instead of being parsed as syntax or raising a
// syntax error the caller would have to surface as a 500.
func Parse(raw string) (expression string, err error)
```

Grammar, in the order the tokenizer applies it:

| Input | Emitted |
| --- | --- |
| `rust release` | `("rust" AND "release")` |
| `"release notes"` | `("release notes")` |
| `-draft` | `NOT ("draft")` |
| `secur*` | `("secur"*)` |
| `C++` | `("C++")` |

Positives and negatives combine as `(pos1 AND pos2) NOT (neg1 OR neg2)`. The
parentheses are load-bearing: FTS5 binds `NOT` tighter than `AND`, so the
unparenthesized `a AND b NOT c` would mean `a AND (b NOT c)`.

Quoting doubles any embedded `"`. An unterminated quote closes implicitly at
end of input rather than erroring — forgiving is right for a search box, and
the result is still a well-formed expression.

- [ ] **Step 1: Write the failing tests**

```go
func TestParse(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"single term", "rust", `("rust")`},
		{"implicit and", "rust release", `("rust" AND "release")`},
		{"phrase", `"release notes"`, `("release notes")`},
		{"exclusion", "rust -draft", `("rust") NOT ("draft")`},
		{"prefix", "secur*", `("secur"*)`},
		{"operator as literal", "foo NEAR bar", `("foo" AND "NEAR" AND "bar")`},
		{"column filter as literal", "title:foo", `("title:foo")`},
		{"punctuation", "C++", `("C++")`},
		{"embedded quote", `say "hi" there`, `("say" AND "hi" AND "there")`},
		{"unterminated quote", `"release notes`, `("release notes")`},
		{"bare star", "*", ""},
	}
	// ... assert Parse(in) == want, err == nil
}

func TestParseRejectsOnlyExclusions(t *testing.T) {
	if _, err := Parse("-draft -wip"); !errors.Is(err, ErrOnlyExclusions) {
		t.Fatalf("err = %v, want ErrOnlyExclusions", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/search/ -v`
Expected: build failure — package does not exist.

- [ ] **Step 3: Implement `internal/search/search.go`**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/search/ -v`

- [ ] **Step 5: Commit** — `Phase 4: Add the FTS5 query parser`

**Verification — automated:**
- [ ] `go test ./internal/search/ -v` passes
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes

**Verification — manual:**
- [ ] None. Pure function, fully covered by the table above.

---

## Phase 5: CLI search end-to-end

Delivers a working feature: `feedspool items --search "container networking"
--sort relevance` searches bodies and ranks results. If phase 6 never lands,
this still ships.

**Files:**
- Create: `internal/database/search_sql.go` — the SQL fragments both query
  builders share
- Modify: `internal/database/item_repository.go` — `ItemFilter` gains `Sort`;
  `buildItemsQuery` (line 411) gains an error return and the FTS join; the
  `instr(lower(i.title), ...)` condition at line 442 goes away
- Modify: `internal/database/item_repository_test.go`
- Modify: `cmd/items.go` — `--sort relevance`, `--search` help text,
  `validateItemsOptions`, skip `reverseItemsList` for relevance

**Key changes — the shared fragments** (`internal/database/search_sql.go`):

```go
// These three constants are the single source of truth for what a search
// matches and how it ranks. buildItemsQuery (CLI) and itemPageConditions (API)
// both build from them; TestSearchSurfacesAgree asserts they cannot drift.
// The predecessor to this file was a duplicated instr() expression in two
// places held in step only by a comment.
const (
	itemsFTSJoin  = " JOIN items_fts f ON f.rowid = i.id"
	itemsFTSMatch = "f MATCH ?"
	// Title outranks summary outranks body. Without column weights, a body
	// mention buries an exact title match.
	itemsFTSRank = "bm25(f, 10.0, 4.0, 1.0)"
)

// SortRelevance and friends name the orderings both surfaces accept.
const (
	SortNewest    = "newest"
	SortOldest    = "oldest"
	SortRelevance = "relevance"
)
```

**Key changes — `buildItemsQuery`:**

```go
func buildItemsQuery(filter *ItemFilter) (query string, args []interface{}, err error)
```

When `filter.Search != ""`, call `search.Parse`; an empty expression means no
filter, and an error propagates out of `GetItems` (so `-search "-draft"` exits
non-zero with the parser's message rather than returning everything). When the
expression is non-empty, append `itemsFTSJoin` to the FROM clause and
`itemsFTSMatch` to the conditions.

Ordering: `SortRelevance` emits
`ORDER BY ` + itemsFTSRank + ` ASC, ` + aliasedEffectiveDateExpression + ` DESC, i.id DESC`.
bm25 is negative-better, so the sort ascends; the date and ID tiebreaks make
the ordering total for equally-scored rows. Anything else keeps the existing
`ORDER BY aliasedEffectiveDateExpression DESC`.

The structural test that both surfaces agree lands in **phase 6**, once
`ListItems` also searches — there is nothing to compare against until then.

**Key changes — `cmd/items.go`:**

- `--search` help becomes `"Full-text search over title, summary and body"`.
- `--sort` help becomes `"Sort order (newest|oldest|relevance)"`.
- `validateItemsOptions` (line 129) accepts `relevance` and rejects it without
  `--search`: `"--sort relevance requires --search"`.
- `runItems` (line 101) must not call `reverseItemsList` when the sort is
  relevance — the ranking is already in the right order and reversing it would
  put the worst match first.
- `--sort relevance` with `--limit 0` is allowed; ranking without a limit is
  slow but not wrong, and the CLI has no pagination.

- [ ] **Step 1: Write the failing tests**

```go
func TestGetItemsSearchMatchesBodyAndSummary(t *testing.T) {
	// A term present only in content, and one only in summary, both match --
	// the behavior change from title-only substring search.
}

func TestGetItemsSearchComposesWithFilters(t *testing.T) {
	// search + FeedURL, search + Since/Until, search + Seen/Unseen.
	// Each must intersect, not replace.
}

func TestGetItemsRelevanceRanksTitleAboveBody(t *testing.T) {
	// Two items: one with the term in the title, one with it repeated in the
	// body. With Sort=relevance the title match comes first -- this is what
	// the bm25 column weights buy.
}

func TestGetItemsSearchRejectsOnlyExclusions(t *testing.T) {
	// GetItems returns the parser error rather than every row.
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/database/ -run 'GetItemsSearch|Relevance' -v`

- [ ] **Step 3: Implement `search_sql.go`, the `buildItemsQuery` change, and the `cmd/items.go` change**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/database/ -v`

- [ ] **Step 5: Commit** — `Phase 5: Full-text search and relevance on the CLI`

**Verification — automated:**
- [ ] `go test ./internal/database/ -v` passes
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `make build` succeeds

**Verification — manual:**
- [ ] Against the real-database copy: `feedspool items --search "<a topic you
      know is in there>" --limit 20` returns items whose *bodies* are about
      that topic, not only ones with it in the title.
- [ ] The same query with `--sort relevance` puts visibly better matches first.
      This is a judgement call and the point of the phase — a correct search
      that ranks badly is still a bad search.
- [ ] `feedspool items --search "-draft"` exits non-zero with a readable
      message rather than dumping the whole database.
- [ ] `feedspool items --sort relevance` without `--search` exits non-zero.

---

## Phase 6: API search end-to-end

Delivers the HTTP surface: `?q=` matches bodies, `sort=relevance` ranks, and
relevance paginates by an offset hidden inside the opaque cursor.

**Files:**
- Modify: `internal/database/pagination.go` — `ItemPage` gains `Sort`;
  `ItemCursor` gains `Relevance` and `Offset`; `itemPageConditions` (line 155)
  swaps the `instr()` condition at line 175 for the FTS join; `ListItems`
  gains the relevance ordering and offset path
- Modify: `internal/database/pagination_test.go`
- Modify: `internal/api/cursor.go` — payload gains the two fields
- Modify: `internal/api/items.go` — `sortRelevance`, validation, `Sort` wiring;
  `itemFilters` (line ~136) gains a `sort string` field alongside `ascending`,
  because two later checks need to know which ordering was requested, not just
  its direction
- Modify: `internal/api/openapi.yaml` — `q` description, `sort` enum
- Modify: `internal/api/handlers_test.go` — including
  `TestListItemsSearchMatchesTitleOnly` at line 314, which asserts the old
  behavior and must be rewritten rather than deleted
- Create: `internal/database/search_agreement_test.go`

**Key changes — the cursor:**

```go
type ItemCursor struct {
	DateRank      int
	EffectiveDate float64
	ID            int64

	// Relevance and Offset are used only by Sort == SortRelevance. A bm25
	// ordering has no stable natural key -- the score depends on corpus
	// statistics, so any write shifts every score -- and a keyset built on it
	// would silently skip rows. Offset can only repeat or drop a row, which is
	// the ordinary offset caveat and a strictly better failure.
	Relevance bool
	Offset    int
}
```

`itemCursorPayload` gains `Relevance bool \`json:"m,omitempty"\`` and
`Offset int \`json:"o,omitempty"\``. `cursorVersion` stays 1: the decoder uses
`DisallowUnknownFields`, and older cursors simply decode with both fields
zero, which is exactly the date-mode cursor they were.

`decodeItemCursor` gains no new validation; the *handler* rejects the mismatch,
because that is where the requested sort is known.

**Key changes — `ListItems`:**

When `page.Sort == SortRelevance`, the query orders by `itemsFTSRank ASC,
effective_date DESC, i.id DESC` and paginates with `LIMIT ? OFFSET ?` from
`page.After.Offset`, ignoring the keyset predicate entirely. The next cursor is
`&ItemCursor{Relevance: true, Offset: previousOffset + limit}`. Everything else
keeps the existing keyset path unchanged.

Relevance requires a search expression; `ListItems` returns an error if
`Sort == SortRelevance` with an empty parsed expression, mirroring the CLI.

**Key changes — `internal/api/items.go`:**

The sort names must exist once, not twice. `internal/api` already declares
`sortNewest` and `sortOldest` at `internal/api/items.go:14-15`; rebind all three
to the `internal/database` constants added in phase 5 so the two packages cannot
drift on a spelling:

```go
// Bound to the database constants rather than re-spelled, so a rename cannot
// leave the two packages disagreeing about what "relevance" is called.
const (
	sortNewest    = database.SortNewest
	sortOldest    = database.SortOldest
	sortRelevance = database.SortRelevance
)
```

In `parseItemFilters` (line 142), extend the validation at lines 167-174 and
record the chosen sort:

```go
if sort != sortNewest && sort != sortOldest && sort != sortRelevance {
	return filters, fmt.Errorf("%s must be %s, %s or %s",
		paramSort, sortNewest, sortOldest, sortRelevance)
}
if sort == sortRelevance && strings.TrimSpace(query.Get(paramQuery)) == "" {
	return filters, fmt.Errorf("%s=%s requires %s", paramSort, sortRelevance, paramQuery)
}
filters.sort = sort
filters.ascending = sort == sortOldest
```

`strings` is not currently imported in `internal/api/items.go`; add it.

In `buildItemPage`, after decoding the cursor, reject a mode mismatch as a
cursor error so the handler reports `invalid_cursor`:

```go
// A cursor from one ordering means nothing in another: a relevance cursor
// carries an offset, a date cursor carries a keyset position. Replaying the
// wrong one would silently return a wrong page rather than an error.
if after != nil && after.Relevance != (filters.sort == sortRelevance) {
	return nil, nil, cursorError{fmt.Errorf("cursor does not match sort=%s", filters.sort)}
}
```

A `search.Parse` failure must surface as `400 invalid_parameter`, not a 500.
`ListItems` returns the parser's error; `handleListItems` needs to distinguish
it from a database failure — check `errors.Is(err, search.ErrOnlyExclusions)`
before falling through to `writeInternalError`.

**Key changes — `openapi.yaml`:**

Replace the `q` description at lines 174-178:

```yaml
        - name: q
          in: query
          description: |
            Full-text search over the item title, summary and body. Terms are
            ANDed; `"quoted phrases"`, `-exclusions` and `prefix*` are
            supported; everything else is matched literally. Identical to
            `feedspool items --search`.
          schema: { type: string }
```

and the `sort` enum at line 200-202:

```yaml
        - name: sort
          in: query
          description: |
            `relevance` requires `q` and is the only ordering that does not use
            a keyset cursor; its cursors are not interchangeable with the
            others.
          schema: { type: string, enum: [newest, oldest, relevance], default: newest }
```

- [ ] **Step 1: Write the failing tests**

```go
func TestListItemsSearchMatchesBodyAndSummary(t *testing.T) {
	// Replaces TestListItemsSearchMatchesTitleOnly at handlers_test.go:314.
	// Rewrite rather than delete: the old test pins the old contract, and the
	// new one is the record that the contract changed deliberately.
}

func TestListItemsRelevancePaginatesWithoutGapsOrRepeats(t *testing.T) {
	// Seed 25 matching items, page through with limit 10, collect IDs.
	// Assert 25 distinct IDs and that the relevance order is preserved across
	// page boundaries. This is the totality property the offset cursor has to
	// keep in the absence of writes.
}

func TestListItemsRejectsCursorFromAnotherSort(t *testing.T) {
	// A relevance cursor replayed with sort=newest is 400 invalid_cursor,
	// and vice versa.
}

func TestListItemsRelevanceRequiresQuery(t *testing.T) {
	// sort=relevance with no q is 400 invalid_parameter.
}

func TestListItemsOnlyExclusionsIsBadRequest(t *testing.T) {
	// q=-draft is 400 invalid_parameter, not 500.
}

func TestListItemsDefaultsToNewestWithAQuery(t *testing.T) {
	// The spec's explicit call: a query does NOT imply relevance. ?q=<term>
	// with no sort returns newest-first and a keyset cursor, not an offset
	// cursor. Asserting it means a later "helpful" change to the default has
	// to be deliberate.
}

// TestSearchSurfacesAgree is the guard against the CLI and the API drifting
// apart on what a query matches -- the failure mode #62 hit with annotation
// kinds. It is a structural invariant, not a regression test: any query, any
// corpus, the two paths must return the same item IDs. Lives in
// internal/database/search_agreement_test.go.
func TestSearchSurfacesAgree(t *testing.T) {
	// Seed a corpus with terms appearing in title only, summary only, body
	// only, several at once, and none.
	for _, query := range []string{
		"networking", "release notes", `"release notes"`, "rust -draft",
		"secur*", "C++", "absent",
	} {
		cliItems, cliErr := db.GetItems(&ItemFilter{Search: query})
		apiItems, _, apiErr := db.ListItems(&ItemPage{Search: query, Limit: 1000})
		// Errors must agree too: "-draft" has to fail on both surfaces or
		// neither, otherwise one of them is silently returning everything.
		if (cliErr == nil) != (apiErr == nil) {
			t.Errorf("query %q: CLI err = %v, API err = %v", query, cliErr, apiErr)
			continue
		}
		if cliErr != nil {
			continue
		}
		if cli, api := sortedIDs(cliItems), sortedIDs(apiItems); !slices.Equal(cli, api) {
			t.Errorf("query %q: CLI returned %v, API returned %v", query, cli, api)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/api/ ./internal/database/ -run 'ListItems|Relevance|Cursor' -v`

- [ ] **Step 3: Implement the pagination, cursor, handler and OpenAPI changes**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/api/ ./internal/database/ -v`

- [ ] **Step 5: Confirm the two surfaces agree**

Run: `go test ./internal/database/ -run TestSearchSurfacesAgree -v`

- [ ] **Step 6: Commit** — `Phase 6: Full-text search and relevance on the API`

**Verification — automated:**
- [ ] `go test ./internal/api/ ./internal/database/ -v` passes
- [ ] `TestOpenAPICoversEveryRoute` passes (it is what keeps the hand-written
      spec from drifting — see `internal/api/openapi.go:14`)
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `go test -race ./internal/api/ ./internal/database/` passes (needs cgo,
      runs outside the Makefile, as CI does it)

**Verification — manual:**
- [ ] Against the real-database copy, with `feedspool serve` running:
      `curl -s 'localhost:8889/api/v1/items?q=<topic>&sort=relevance&limit=5' | jq '.data[].title'`
      returns sensibly ranked results.
- [ ] Page through with the returned `next_cursor` and confirm no repeats.
- [ ] `curl -s 'localhost:8889/api/v1/items?sort=relevance'` returns 400.
- [ ] Compare the same query through the CLI and the API and confirm the same
      items come back.

---

## Phase 7: Documentation and the real-database smoke test

**Files:**
- Modify: `MANUAL.md` — the `items` subcommand section (line ~357), the HTTP
  API "Reading items" parameter table (line ~733) and its `q` bullet, the
  Data Model section (line ~1076) for `item_text` and `items_fts`, a `reindex`
  subcommand entry, and the SQL Recipes section (line ~1147) with one worked
  FTS query
- Modify: `docs/dev-sessions/2026-08-26-1247-fts5-search/notes.md`

**Key changes:**

MANUAL.md's `q` bullet currently reads "**`q` matches titles only**, not body or
summary — the same limitation as `feedspool items --search` ... Tracked in
#58." That text and the matching row in the parameter table are the two places
the old contract is written down; both change, and the note should say plainly
that the semantics changed before v1 shipped in a tagged release.

Add the query-language table from the spec to both the CLI and API sections,
spelled the same way in each.

- [ ] **Step 1: Update MANUAL.md**

- [ ] **Step 2: Run the full smoke test against a copy of a real feed database**

```bash
cp ~/path/to/real/feeds.db /tmp/fts-smoke.db
make build
./feedspool --database /tmp/fts-smoke.db status
./feedspool --database /tmp/fts-smoke.db items --search "<known topic>" --limit 20
./feedspool --database /tmp/fts-smoke.db items --search "<known topic>" --sort relevance --limit 20
./feedspool --database /tmp/fts-smoke.db reindex --force
sqlite3 /tmp/fts-smoke.db "INSERT INTO items_fts(items_fts) VALUES('integrity-check');"
```

- [ ] **Step 3: Record findings in `notes.md`** — migration wall-clock, index
      size delta, query latency at the real corpus size, and a judgement on
      result quality. If items are hitting the 256 KiB body cap, or `porter`
      stemming is visibly hurting known-item lookup, this is where those two
      open questions from the spec get closed.

- [ ] **Step 4: Commit** — `Phase 7: Document full-text search`

**Verification — automated:**
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `make build` succeeds

**Verification — manual:**
- [ ] Read the MANUAL.md diff end to end. The CLI section and the API section
      must describe the same query language in the same words.
- [ ] No remaining claim anywhere in the repo that search matches titles only:
      `grep -rn "title.*only\|titles only" MANUAL.md internal/ cmd/`
- [ ] The smoke-test results are recorded in `notes.md`, including the quality
      judgement rather than only the timings.

---

## Deferred, recorded so it is not silently dropped

- `include=snippet` and a relevance score on the item DTO. Both work for free
  with an external-content table; neither has a consumer yet, and both are
  purely additive whenever one appears.
- `OR`, `NEAR`, and column filters in the query language. Literal text today;
  adding operators later is additive.
- Re-deriving text when `items` is written by anything other than `UpsertItem`.
  Nothing does today; `reindex --force` is the repair path if that changes.
