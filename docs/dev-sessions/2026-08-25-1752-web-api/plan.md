# HTTP API v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read/write JSON API for the feed database, mounted at `/api/v1/` on the existing `serve` command behind a `--api` flag.

**Architecture:** A new `internal/api` package holds handlers, DTOs, cursor logic, and an embedded OpenAPI document, wired onto an `http.ServeMux` that `internal/server` mounts alongside the existing static file handler. Two new keyset-paginated repository methods back the list endpoints; the existing `GetItems` is left untouched because the CLI depends on its Go-side filtering. A new `internal/ids` package holds the derived feed and item identifiers so the API and the static-site renderer cannot drift apart.

**Tech Stack:** Go 1.25 stdlib only — `http.ServeMux` method+wildcard patterns (1.22+), `go:embed`, `crypto/subtle`. No router dependency, no OpenAPI codegen. `modernc.org/sqlite` (pure Go, CGO_ENABLED=0).

**Spec:** `docs/dev-sessions/2026-08-25-1752-web-api/spec.md`

## Global Constraints

- Build with `make build`, never bare `go build`. Binary is named `feedspool`.
- CGO-free. Do not add `mattn/go-sqlite3` or any dependency requiring a C toolchain.
- Add no new Go module dependencies. The stdlib covers everything here.
- Run `make format`, `make lint`, then `make test` after each task. `make lint` uses the pinned `GOLANGCI_LINT_VERSION`; do not install a different version to dodge a finding.
- `internal/` code gets tests; `cmd/` does not need them.
- Existing behavior is frozen: `feedspool serve` without `--api` must behave exactly as it does today, and `database.GetItems` must keep its current semantics.
- Migration number is **10**. `maxMigrationVersion` on `main` is 9.
- All API timestamps are RFC3339 strings or JSON `null`. Never leak a `sql.NullTime` shape.

---

### Task 1: `internal/ids` package

**Files:**
- Create: `internal/ids/ids.go`
- Create: `internal/ids/ids_test.go`
- Modify: `internal/renderer/workflow.go` (remove `generateFeedID`, call `ids.FeedID`)

**Interfaces:**
- Produces: `ids.FeedID(feedURL string) string` → 8 lowercase hex chars. `ids.ItemID(feedURL, guid string) string` → 16 lowercase hex chars.

- [ ] **Step 1: Write the failing test**

`internal/ids/ids_test.go`. The golden values pin the current renderer output — those strings are live static-site URLs (`feeds/<id>.html`) and must not change.

```go
package ids

import "testing"

func TestFeedIDMatchesRendererGoldenValues(t *testing.T) {
	// Computed from the pre-existing generateFeedID: first 8 hex of sha256(url).
	tests := []struct{ url, want string }{
		{"https://example.com/feed.xml", ""},
		{"https://blog.example.org/atom", ""},
	}
	for _, tt := range tests {
		if got := FeedID(tt.url); got != tt.want {
			t.Errorf("FeedID(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestFeedIDIsEightHexChars(t *testing.T) {
	got := FeedID("https://example.com/feed.xml")
	if len(got) != 8 {
		t.Errorf("FeedID() length = %d, want 8", len(got))
	}
	for _, r := range got {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("FeedID() = %q, want lowercase hex", got)
		}
	}
}

func TestItemIDIsSixteenHexChars(t *testing.T) {
	got := ItemID("https://example.com/feed.xml", "guid-1")
	if len(got) != 16 {
		t.Errorf("ItemID() length = %d, want 16", len(got))
	}
}

func TestItemIDSeparatorPreventsCollision(t *testing.T) {
	// Without a separator, ("ab","c") and ("a","bc") would hash identically.
	if ItemID("ab", "c") == ItemID("a", "bc") {
		t.Error("ItemID() collided across a shifted boundary; separator is missing")
	}
}

func TestItemIDVariesByFeed(t *testing.T) {
	if ItemID("https://a.example/f", "g") == ItemID("https://b.example/f", "g") {
		t.Error("ItemID() must differ when the feed URL differs")
	}
}
```

Fill the two empty `want` values by running the existing renderer helper first:

```bash
cat > /tmp/golden.go <<'EOF'
package main

import ("crypto/sha256"; "fmt")

func generateFeedID(feedURL string) string {
	hash := sha256.Sum256([]byte(feedURL))
	return fmt.Sprintf("%x", hash)[:8]
}

func main() {
	for _, u := range []string{"https://example.com/feed.xml", "https://blog.example.org/atom"} {
		fmt.Printf("%s -> %s\n", u, generateFeedID(u))
	}
}
EOF
go run /tmp/golden.go
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ids/ -v`
Expected: FAIL — package has no `FeedID` or `ItemID`.

- [ ] **Step 3: Write the implementation**

`internal/ids/ids.go`:

```go
// Package ids derives the stable public identifiers feedspool uses for feeds
// and items. They are computed from natural keys rather than stored, so they
// survive a purge-and-refetch cycle that would reassign items.id.
package ids

import (
	"crypto/sha256"
	"encoding/hex"
)

const (
	feedIDLength = 8
	itemIDLength = 16
)

// itemIDSeparator keeps ("ab","c") from hashing the same as ("a","bc"). A
// newline is safe because it cannot appear in a URL.
const itemIDSeparator = "\n"

// FeedID returns the public identifier for a feed URL.
//
// The length is 8 rather than something roomier because these values are
// already published as static-site URLs (feeds/<id>.html). Widening it would
// break existing bookmarks.
func FeedID(feedURL string) string {
	sum := sha256.Sum256([]byte(feedURL))
	return hex.EncodeToString(sum[:])[:feedIDLength]
}

// ItemID returns the public identifier for an item, keyed on the same
// (feed_url, guid) pair that uniquely identifies its row.
func ItemID(feedURL, guid string) string {
	sum := sha256.Sum256([]byte(feedURL + itemIDSeparator + guid))
	return hex.EncodeToString(sum[:])[:itemIDLength]
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ids/ -v`
Expected: PASS.

- [ ] **Step 5: Point the renderer at the shared helper**

In `internal/renderer/workflow.go`: delete `generateFeedID`, add the `ids` import, and replace both call sites (around lines 334 and 465) with `ids.FeedID(...)`. Drop the now-unused `crypto/sha256` import if nothing else in the file uses it.

- [ ] **Step 6: Verify the renderer is unchanged**

Run: `make format && make lint && make test`
Expected: PASS, with renderer tests still green — proving the ID values did not move.

- [ ] **Step 7: Commit**

```bash
git add internal/ids internal/renderer/workflow.go
git commit -m "refactor: Extract feed and item ID derivation into internal/ids"
```

---

### Task 2: Migration 10 — annotation uniqueness

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/annotations.go`
- Modify: `internal/database/schema.sql`
- Test: `internal/database/migrations_test.go`, `internal/database/annotations_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `AddAnnotation` becomes idempotent. New `(db *DB) AnnotationExists(feedURL, itemGUID, kind string, value sql.NullString) (bool, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/database/migrations_test.go`:

```go
func TestMigration10DedupesAnnotations(t *testing.T) {
	db := setupTestDB(t)

	seedFeedAndItem(t, db, "https://example.com/feed.xml", "guid-1")

	// Two identical NULL-valued annotations, distinguishable only by time.
	for _, ts := range []string{"2026-01-01 00:00:00", "2026-02-02 00:00:00"} {
		if _, err := db.conn.Exec(
			`INSERT INTO item_annotations (feed_url, item_guid, kind, value, actor, created_at)
			 VALUES (?, ?, 'seen', NULL, NULL, ?)`,
			"https://example.com/feed.xml", "guid-1", ts,
		); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.applyMigration10(); err != nil {
		t.Fatalf("applyMigration10() error = %v", err)
	}

	var count int
	var createdAt string
	if err := db.conn.QueryRow(
		`SELECT COUNT(*), MIN(created_at) FROM item_annotations WHERE kind = 'seen'`,
	).Scan(&count, &createdAt); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("annotation count after migration = %d, want 1", count)
	}
	if createdAt != "2026-01-01 00:00:00" {
		t.Errorf("surviving created_at = %q, want the earliest", createdAt)
	}
}

func TestMigration10KeepsDistinctValues(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, "https://example.com/feed.xml", "guid-1")

	for _, v := range []string{"later", "urgent"} {
		if _, err := db.conn.Exec(
			`INSERT INTO item_annotations (feed_url, item_guid, kind, value)
			 VALUES (?, ?, 'tag', ?)`,
			"https://example.com/feed.xml", "guid-1", v,
		); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.applyMigration10(); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM item_annotations WHERE kind = 'tag'`,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("distinct-valued annotations = %d, want 2", count)
	}
}
```

Create `internal/database/annotations_test.go`:

```go
package database

import (
	"database/sql"
	"testing"
)

func seedFeedAndItem(t *testing.T, db *DB, feedURL, guid string) {
	t.Helper()
	if err := db.UpsertFeed(&Feed{URL: feedURL, Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertItem(&Item{FeedURL: feedURL, GUID: guid, Title: "Item"}); err != nil {
		t.Fatal(err)
	}
}

func TestAddAnnotationIsIdempotent(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, "https://example.com/feed.xml", "guid-1")

	for range 3 {
		if err := db.AddAnnotation(
			"https://example.com/feed.xml", "guid-1", "seen", sql.NullString{}, sql.NullString{},
		); err != nil {
			t.Fatalf("AddAnnotation() error = %v", err)
		}
	}

	annotations, err := db.GetAnnotations("https://example.com/feed.xml", "guid-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 {
		t.Errorf("annotations after 3 identical adds = %d, want 1", len(annotations))
	}
}

func TestAddAnnotationDistinctValuesCoexist(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, "https://example.com/feed.xml", "guid-1")

	for _, v := range []string{"later", "urgent"} {
		if err := db.AddAnnotation("https://example.com/feed.xml", "guid-1", "tag",
			sql.NullString{String: v, Valid: true}, sql.NullString{}); err != nil {
			t.Fatal(err)
		}
	}

	annotations, err := db.GetAnnotations("https://example.com/feed.xml", "guid-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 2 {
		t.Errorf("annotations = %d, want 2", len(annotations))
	}
}

func TestAnnotationExists(t *testing.T) {
	db := setupTestDB(t)
	seedFeedAndItem(t, db, "https://example.com/feed.xml", "guid-1")

	exists, err := db.AnnotationExists("https://example.com/feed.xml", "guid-1", "seen", sql.NullString{})
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("AnnotationExists() = true before any add, want false")
	}

	if err := db.AddAnnotation("https://example.com/feed.xml", "guid-1", "seen",
		sql.NullString{}, sql.NullString{}); err != nil {
		t.Fatal(err)
	}

	exists, err = db.AnnotationExists("https://example.com/feed.xml", "guid-1", "seen", sql.NullString{})
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("AnnotationExists() = false after add, want true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/database/ -run 'Annotation|Migration10' -v`
Expected: FAIL — `applyMigration10` and `AnnotationExists` are undefined.

- [ ] **Step 3: Add the migration**

In `internal/database/migrations.go`, add the constant and bump the max:

```go
	migrationVersion10  = 10 // Deduplicate annotations and add a uniqueness index
	maxMigrationVersion = migrationVersion10
```

Add to `getMigrations()`:

```go
		migrationVersion10: `DELETE FROM item_annotations
		WHERE rowid NOT IN (
			SELECT MIN(rowid) FROM item_annotations
			GROUP BY feed_url, item_guid, kind, COALESCE(value, ''), COALESCE(created_at, '')
		)
		AND rowid NOT IN (
			SELECT keep.rowid FROM (
				SELECT rowid,
					ROW_NUMBER() OVER (
						PARTITION BY feed_url, item_guid, kind, COALESCE(value, '')
						ORDER BY created_at ASC, rowid ASC
					) AS rn
				FROM item_annotations
			) AS keep
			WHERE keep.rn = 1
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_item_annotations_unique
			ON item_annotations(feed_url, item_guid, kind, COALESCE(value, ''));`,
```

Simplify that to just the `ROW_NUMBER()` half — the first `DELETE` subquery is redundant. The final statement pair should read:

```go
		migrationVersion10: `DELETE FROM item_annotations
		WHERE rowid NOT IN (
			SELECT rowid FROM (
				SELECT rowid,
					ROW_NUMBER() OVER (
						PARTITION BY feed_url, item_guid, kind, COALESCE(value, '')
						ORDER BY created_at ASC, rowid ASC
					) AS rn
				FROM item_annotations
			)
			WHERE rn = 1
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_item_annotations_unique
			ON item_annotations(feed_url, item_guid, kind, COALESCE(value, ''));`,
```

`COALESCE(value, '')` is required: SQLite treats NULLs as distinct in a unique
index, so `(feed, guid, 'seen', NULL)` would never conflict with itself.
`ORDER BY created_at ASC, rowid ASC` keeps the earliest sighting, with a
deterministic tiebreak because `CURRENT_TIMESTAMP` has one-second resolution.

Add the `applyMigration10` method next to the others:

```go
// applyMigration10 removes duplicate annotations and adds the uniqueness index
// that keeps them from coming back.
func (db *DB) applyMigration10() error {
	migrations := getMigrations()
	return db.ApplyMigration(migrationVersion10, migrations[migrationVersion10])
}
```

Wire it into the same `switch`/dispatch the other `applyMigrationN` functions use — match the surrounding style exactly.

- [ ] **Step 4: Make `AddAnnotation` idempotent and add `AnnotationExists`**

In `internal/database/annotations.go`:

```go
func (db *DB) AddAnnotation(feedURL, itemGUID, kind string, value, actor sql.NullString) error {
	query := `
		INSERT INTO item_annotations (feed_url, item_guid, kind, value, actor)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING
	`
	_, err := db.conn.Exec(query, feedURL, itemGUID, kind, value, actor)
	if err != nil {
		return fmt.Errorf("failed to add annotation: %w", err)
	}
	return nil
}

// AnnotationExists reports whether an identical annotation is already stored.
// The API uses it to answer 201-on-create versus 200-on-already-present.
func (db *DB) AnnotationExists(feedURL, itemGUID, kind string, value sql.NullString) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM item_annotations
			WHERE feed_url = ? AND item_guid = ? AND kind = ?
				AND COALESCE(value, '') = COALESCE(?, '')
		)
	`
	var exists bool
	if err := db.conn.QueryRow(query, feedURL, itemGUID, kind, value).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check annotation: %w", err)
	}
	return exists, nil
}
```

`ON CONFLICT DO NOTHING` without a conflict target works against any unique
index, which is what the expression index requires — a target clause would have
to restate the `COALESCE` expression.

- [ ] **Step 5: Update `schema.sql`**

Add the unique index next to the existing `item_annotations` indexes so fresh
databases match migrated ones:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_item_annotations_unique
    ON item_annotations(feed_url, item_guid, kind, COALESCE(value, ''));
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/database/ -v`
Expected: PASS, including the pre-existing migration tests.

- [ ] **Step 7: Commit**

```bash
make format && make lint && make test
git add internal/database
git commit -m "fix: Make item annotations unique and AddAnnotation idempotent"
```

---

### Task 3: Keyset-paginated repository methods

**Files:**
- Create: `internal/database/pagination.go`
- Create: `internal/database/pagination_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:

```go
type ItemCursor struct {
	DateRank      int     // 0 = orderable date, 1 = NULL (unparseable/absent)
	EffectiveDate float64 // julianday value; meaningless when DateRank == 1
	ID            int64
}

type ItemPage struct {
	FeedURL   string
	FeedQuery string
	Link      string
	Search    string
	Since     time.Time
	Until     time.Time
	Seen      *bool   // nil = no filter
	Archived  *bool   // nil = any
	Ascending bool
	Limit     int
	After     *ItemCursor
}

func (db *DB) ListItems(page *ItemPage) ([]*Item, *ItemCursor, error)

type FeedPage struct {
	URL   string
	Limit int
	After string // last url returned
}

func (db *DB) ListFeeds(page *FeedPage) ([]*Feed, string, error)
func (db *DB) GetItemByHashID(id string) (*Item, error)
func (db *DB) GetFeedByHashID(id string) (*Feed, error)
func (db *DB) CountItemsForFeed(feedURL string) (total, unseen int, err error)
```

`ListItems` returns `(rows, nextCursor, error)`; `nextCursor` is nil when the
result set is exhausted. `ListFeeds` returns an empty string for the same.

- [ ] **Step 1: Write the failing tests**

`internal/database/pagination_test.go`. The continuity test is the one that
justifies keyset over offset — it must exist.

```go
package database

import (
	"fmt"
	"testing"
	"time"
)

func seedItems(t *testing.T, db *DB, feedURL string, n int) {
	t.Helper()
	if err := db.UpsertFeed(&Feed{URL: feedURL, Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		if err := db.UpsertItem(&Item{
			FeedURL:       feedURL,
			GUID:          fmt.Sprintf("guid-%03d", i),
			Title:         fmt.Sprintf("Item %03d", i),
			Link:          fmt.Sprintf("https://example.com/%03d", i),
			PublishedDate: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListItemsPagesWithoutGapsOrDuplicates(t *testing.T) {
	db := setupTestDB(t)
	const feedURL = "https://example.com/feed.xml"
	seedItems(t, db, feedURL, 25)

	seen := map[string]bool{}
	var cursor *ItemCursor
	pages := 0
	for {
		items, next, err := db.ListItems(&ItemPage{Limit: 10, After: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if seen[item.GUID] {
				t.Fatalf("item %q returned twice", item.GUID)
			}
			seen[item.GUID] = true
		}
		pages++
		if next == nil {
			break
		}
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
		cursor = next
	}
	if len(seen) != 25 {
		t.Errorf("total distinct items paged = %d, want 25", len(seen))
	}
}

func TestListItemsInsertionMidScanDoesNotDuplicate(t *testing.T) {
	// The offset-pagination failure mode: a row arriving at the head of the
	// ordering shifts every later page. Keyset must be immune.
	db := setupTestDB(t)
	const feedURL = "https://example.com/feed.xml"
	seedItems(t, db, feedURL, 20)

	first, cursor, err := db.ListItems(&ItemPage{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if cursor == nil {
		t.Fatal("expected a continuation cursor")
	}

	// Newest-yet item arrives between page 1 and page 2.
	if err := db.UpsertItem(&Item{
		FeedURL:       feedURL,
		GUID:          "guid-999",
		Title:         "Breaking",
		PublishedDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	second, _, err := db.ListItems(&ItemPage{Limit: 10, After: cursor})
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, item := range append(first, second...) {
		if seen[item.GUID] {
			t.Errorf("item %q appeared on both pages after a mid-scan insert", item.GUID)
		}
		seen[item.GUID] = true
	}
}

func TestListItemsOrdersNullDatesLast(t *testing.T) {
	db := setupTestDB(t)
	const feedURL = "https://example.com/feed.xml"
	if err := db.UpsertFeed(&Feed{URL: feedURL, Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	// A scraped item: no published_date, no first_seen.
	if err := db.UpsertItem(&Item{FeedURL: feedURL, GUID: "no-date", Title: "Undated"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertItem(&Item{
		FeedURL: feedURL, GUID: "dated", Title: "Dated",
		PublishedDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	items, _, err := db.ListItems(&ItemPage{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].GUID != "dated" {
		t.Errorf("first item = %q, want the dated one", items[0].GUID)
	}
}

func TestListItemsFiltersBySearch(t *testing.T) {
	db := setupTestDB(t)
	const feedURL = "https://example.com/feed.xml"
	seedItems(t, db, feedURL, 5)

	items, _, err := db.ListItems(&ItemPage{Limit: 10, Search: "ITEM 003"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].GUID != "guid-003" {
		t.Errorf("search returned %d items, want exactly guid-003", len(items))
	}
}

func TestListFeedsPages(t *testing.T) {
	db := setupTestDB(t)
	for i := range 7 {
		if err := db.UpsertFeed(&Feed{
			URL:   fmt.Sprintf("https://example.com/%02d.xml", i),
			Title: fmt.Sprintf("Feed %02d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	after := ""
	for {
		feeds, next, err := db.ListFeeds(&FeedPage{Limit: 3, After: after})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range feeds {
			if seen[f.URL] {
				t.Fatalf("feed %q returned twice", f.URL)
			}
			seen[f.URL] = true
		}
		if next == "" {
			break
		}
		after = next
	}
	if len(seen) != 7 {
		t.Errorf("feeds paged = %d, want 7", len(seen))
	}
}

func TestGetItemByHashID(t *testing.T) {
	db := setupTestDB(t)
	const feedURL = "https://example.com/feed.xml"
	seedItems(t, db, feedURL, 3)

	item, err := db.GetItemByHashID(ItemHashID(feedURL, "guid-001"))
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || item.GUID != "guid-001" {
		t.Fatalf("GetItemByHashID() = %v, want guid-001", item)
	}

	missing, err := db.GetItemByHashID("ffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Errorf("GetItemByHashID(unknown) = %v, want nil", missing)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/database/ -run 'List|HashID' -v`
Expected: FAIL — undefined types and methods.

- [ ] **Step 3: Implement `internal/database/pagination.go`**

Key points for the implementer:

- The sort expression is `julianday(COALESCE(i.published_date, i.first_seen))`.
  Reuse `aliasedEffectiveDateExpression`, which already holds exactly that.
- `date_rank` is `CASE WHEN <expr> IS NULL THEN 1 ELSE 0 END`. Select it, order
  by it first, and carry it in the cursor.
- Descending (newest-first) keyset predicate:
  `(rank, effective, id) > (?, ?, ?)` is wrong for mixed directions — rank
  ascends while date descends. Write it explicitly rather than as a row-value
  comparison:

```sql
(
  date_rank > ?
  OR (date_rank = ? AND effective_date < ?)
  OR (date_rank = ? AND effective_date = ? AND i.id < ?)
)
```

  For `Ascending`, flip `<` to `>` in the last two clauses. Rank always
  ascends so undated rows stay at the tail in both directions.
  When `DateRank == 1`, `effective_date` is NULL and the middle clause can
  never be true; guard the rank-1 case with `date_rank = 1 AND i.id < ?` only.
  Build the predicate in Go with a small helper rather than one unreadable
  string.
- Fetch `Limit + 1` rows. If the extra row comes back, drop it and build the
  cursor from the last *kept* row. That is how `nextCursor == nil` stays
  accurate without a second count query.
- `Search` uses `instr(lower(i.title), lower(?)) > 0`, identical to
  `buildItemsQuery`. `FeedQuery` uses `instr(lower(i.feed_url), lower(?)) > 0`.
- `Since`/`Until` reuse `discoveryTimeExpression` with the same
  `(expr IS NULL OR expr >= julianday(?))` shape the existing code uses.
- `Seen` is `*bool`: nil adds nothing, true adds the `EXISTS` subquery, false
  adds `NOT EXISTS` — copy both from `buildItemsQuery`.
- `Archived` is `*bool`: nil adds nothing, otherwise `i.archived = ?`.
- Scan with `scanNullableTime(&item.PublishedDate)`, matching `GetItems`.
- Add a package-level `ItemHashID(feedURL, guid string) string` in this file
  that mirrors `ids.ItemID`. `internal/database` must not import
  `internal/ids` — keep the dependency pointing one way — so the SQL side
  computes the hash itself. Add a test in `internal/ids` asserting the two
  agree, so the duplication cannot silently diverge.
- `GetItemByHashID` and `GetFeedByHashID` cannot index on a hash, so they scan.
  For `GetFeedByHashID` that is a few hundred rows — fine. For
  `GetItemByHashID`, restrict the scan in SQL where possible; the honest
  implementation computes the hash in Go over `(feed_url, guid)` pairs:

```go
func (db *DB) GetItemByHashID(id string) (*Item, error) {
	rows, err := db.conn.Query(`SELECT feed_url, guid FROM items`)
	// ... find the matching pair, then delegate to a single-row fetch
}
```

  Note the cost in a comment and reference #60's follow-up territory. At
  personal scale (tens of thousands of rows) a hash scan is milliseconds; a
  stored generated column would be the fix if it ever matters.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/database/ -v`
Expected: PASS.

- [ ] **Step 5: Add the cross-package agreement test**

In `internal/ids/ids_test.go`, add a test asserting `ids.ItemID` and
`database.ItemHashID` produce identical output for the same input. Import
`internal/database` from the `ids` test file only (test-only dependency, so the
production import graph stays acyclic).

- [ ] **Step 6: Commit**

```bash
make format && make lint && make test
git add internal/database internal/ids
git commit -m "feat: Add keyset-paginated ListItems and ListFeeds"
```

---

### Task 4: API foundation — errors, params, cursors, DTOs

**Files:**
- Create: `internal/api/errors.go`, `internal/api/params.go`, `internal/api/cursor.go`, `internal/api/dto.go`
- Create: `internal/api/errors_test.go`, `internal/api/params_test.go`, `internal/api/cursor_test.go`, `internal/api/dto_test.go`

**Interfaces:**
- Consumes: `database.ItemCursor`, `database.Item`, `database.Feed`, `database.ItemAnnotation`, `database.SpoolStatus`, `ids.FeedID`, `ids.ItemID`.
- Produces:

```go
func writeError(w http.ResponseWriter, status int, code, message string)
func writeJSON(w http.ResponseWriter, status int, payload any)

type collection struct {
	Data       any     `json:"data"`
	NextCursor *string `json:"next_cursor"`
	Limit      int     `json:"limit"`
}

func encodeItemCursor(c *database.ItemCursor) string
func decodeItemCursor(s string) (*database.ItemCursor, error)

type includeSet map[string]bool
func parseInclude(raw string, allowed []string) (includeSet, error)
func parseLimit(raw string, def, max int) (int, error)
func parseTriState(raw string) (*bool, error)   // "", "true", "false", "any"
func parseBoolFilter(raw string) (*bool, error) // "", "true", "false"
func parseRFC3339(raw string) (time.Time, error)
func rejectUnknownParams(q url.Values, allowed []string) error

func itemDTO(item *database.Item, inc includeSet, ann []database.ItemAnnotation, meta *database.URLMetadata) map[string]any
func feedDTO(feed *database.Feed, inc includeSet, total, unseen int) map[string]any
func annotationDTO(a database.ItemAnnotation) map[string]any
```

- [ ] **Step 1: Write the failing tests**

Cover, at minimum: error envelope shape and status; cursor round-trip; cursor
rejection for malformed base64, valid base64 of garbage, and truncated input;
`parseLimit` clamping above max and rejecting non-numeric and zero/negative;
`parseTriState` accepting `any`; `rejectUnknownParams` catching `limitt`;
`parseInclude` rejecting an unknown value and rejecting `counts` when it is not
in `allowed`; `itemDTO` emitting `null` (not `{"Time":...}`) for an invalid
`FirstSeen`; `itemDTO` omitting `content` unless included; `itemDTO` emitting
`[]` not `null` for an empty annotation list; `feedDTO` including `type` and
`scrape_selector`.

Write these as ordinary table-driven Go tests in the four `_test.go` files.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -v`
Expected: FAIL to build — nothing is defined.

- [ ] **Step 3: Implement**

Notes that matter:

- The cursor payload is a compact struct marshaled to JSON then
  `base64.RawURLEncoding`. Include a small version byte or field so a future
  format change is detectable rather than silently misparsed.
- `decodeItemCursor` must reject anything it cannot fully parse. Never return a
  zero-valued cursor on error — that would silently restart pagination.
- DTOs return `map[string]any` rather than structs because the `include` set
  makes the field list dynamic. Key order does not matter in JSON.
- Timestamps go through one helper: `func rfc3339OrNull(t time.Time) any` and
  `func nullTimeOrNull(nt sql.NullTime) any`, both returning `nil` for the
  zero/invalid case so `encoding/json` emits `null`.
- `annotationDTO` parses `CreatedAt` (a string) with `time.Parse`, trying
  RFC3339 and then `2006-01-02 15:04:05`; on failure emit the raw string, per
  the spec.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make format && make lint && make test
git add internal/api
git commit -m "feat: Add API error, param, cursor, and DTO primitives"
```

---

### Task 5: Read handlers and the route table

**Files:**
- Create: `internal/api/api.go`, `internal/api/feeds.go`, `internal/api/items.go`, `internal/api/status.go`
- Create: `internal/api/api_test.go`, `internal/api/feeds_test.go`, `internal/api/items_test.go`

**Interfaces:**
- Consumes: everything from Task 4, plus `database.ListItems` / `ListFeeds` from Task 3.
- Produces:

```go
type Config struct {
	DB      *database.DB
	Token   string
	Version string
}

type route struct {
	method  string
	pattern string
	handler func(*Server, http.ResponseWriter, *http.Request)
}

var routes []route

func NewServer(cfg Config) *Server
func (s *Server) Handler() http.Handler
```

- [ ] **Step 1: Write the failing tests**

Build a `newTestServer(t)` helper that creates a temp DB with
`database.New(filepath.Join(t.TempDir(), "test.db"))`, calls `InitSchema` and
`RunMigrations`, seeds a couple of feeds and items, and returns an
`httptest.Server` over `Handler()`.

Cover: service root returns `api_version: "v1"`; `/status` returns the spool
counts; `/feeds` returns an envelope with `data`, `next_cursor`, `limit`;
`/feeds/{id}` returns a bare object; unknown feed id is a 404 with the error
envelope; `/items?limit=1` paginates and the returned `next_cursor` fetches
different rows; `?limitt=1` is a 400; `?seen=maybe` is a 400; `?q=` filters;
`?feed_id=` and `?feed_url=` together is a 400; `include=content` adds the
field and its absence omits it; `POST /api/v1/feeds` is a 405.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

- Declare every route in the package-level `routes` slice. `Handler()` walks it
  and calls `mux.HandleFunc(r.method + " " + r.pattern, ...)`. Go 1.22+ pattern
  syntax gives method matching and `{id}` wildcards, and returns 405
  automatically when a path matches but the method does not.
- Register `GET /api/v1/{$}` for the service root so it does not swallow
  everything under the prefix.
- Each handler: parse and validate params first (returning 400 on the first
  problem), then query, then serialize. No partial writes after a header.
- Wrap handlers in a recovery middleware that logs the panic and returns a
  500 `internal_error` — a panic in a long-running server must not take down
  the whole process while it is also serving the static site.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make format && make lint && make test
git add internal/api
git commit -m "feat: Add API read handlers for feeds, items, and status"
```

---

### Task 6: Annotation handlers

**Files:**
- Create: `internal/api/annotations.go`, `internal/api/annotations_test.go`
- Modify: `internal/api/api.go` (add the four routes)

- [ ] **Step 1: Write the failing tests**

Cover: `GET .../annotations` returns `[]` for an item with none; `POST` returns
201 with the annotation; the same `POST` again returns 200; `DELETE` returns
204 and the annotation is gone; `DELETE` again still returns 204; `DELETE` with
`?value=` only removes the matching value; `POST` with `Content-Type: text/plain`
is a 415; `POST` with `kind: ""` is a 400; `POST` with a 65-character kind is a
400; `POST` with `kind: "a/b"` is a 400; bulk `POST /api/v1/annotations` returns
the `added`/`already_present`/`not_found` tallies; bulk with 501 ids is a 400.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run Annotation -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

- `kind` validation: `^[A-Za-z0-9_.:-]{1,64}$`, compiled once as a package
  `var kindPattern = regexp.MustCompile(...)`.
- 201-vs-200 uses `AnnotationExists` *before* `AddAnnotation`.
- Bulk resolves each id, tallies `not_found` for misses, and calls
  `AnnotationExists` + `AddAnnotation` per hit. Cap at 500 ids.
- `DELETE` maps `?value=` to `sql.NullString{String: v, Valid: true}` and its
  absence to the zero value, matching `RemoveAnnotation` exactly.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make format && make lint && make test
git add internal/api
git commit -m "feat: Add API annotation read and write handlers"
```

---

### Task 7: Auth middleware, server wiring, and CLI flags

**Files:**
- Create: `internal/api/middleware.go`, `internal/api/middleware_test.go`
- Modify: `internal/server/server.go`, `internal/server/server_test.go`
- Modify: `cmd/serve.go`, `internal/config/config.go`, `feedspool.yaml.example`

- [ ] **Step 1: Write the failing tests**

`internal/api/middleware_test.go`: empty token passes every request through;
non-empty token returns 401 without a header, 401 with a wrong token, 401 with
a malformed `Authorization` value, and 200 with the right bearer token; the 401
carries `WWW-Authenticate: Bearer`.

`internal/server/server_test.go`: `validateConfig` accepts an empty `Dir` when
`APIEnabled` is true and still rejects it when false; a server with `APIEnabled`
and no `Dir` returns a JSON 404 at `/`; a server with a `Dir` and no `APIEnabled`
returns 404 at `/api/v1/` (the API is genuinely absent, not just unauthorized).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ ./internal/server/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement the middleware**

```go
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" {
			next(w, r)
			return
		}
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) ||
			subtle.ConstantTimeCompare([]byte(header[len(prefix):]), []byte(s.cfg.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")
			return
		}
		next(w, r)
	}
}
```

- [ ] **Step 4: Wire the server**

- `NewServer(config *Config, db *database.DB) *Server`.
- `createHandler` builds an `http.ServeMux`, registers the API handler at
  `/api/v1/` when `config.APIEnabled`, and the static handler at `/`. Keep the
  existing security-header and verbose-logging wrapper on the outside so its
  behavior is unchanged.
- `validateConfig`: only require `Dir` when static serving is on.
- Listen address becomes `net.JoinHostPort(config.Bind, strconv.Itoa(config.Port))`.
  An empty `Bind` must still produce `:8889` — verify with a test, because
  `JoinHostPort("", "8889")` returns `:8889`, which is what we want.
- Update the two startup `fmt.Printf` lines to mention the API when enabled.

- [ ] **Step 5: Wire the CLI**

`cmd/serve.go`: add `--api` and `--bind`, bind both to viper
(`serve.api.enabled`, `serve.bind`), and `viper.BindEnv("serve.api.token",
"FEEDSPOOL_API_TOKEN")`. Deliberately no `--api-token` flag — a token on the
command line lands in `ps` output.

Open the DB only when the API is enabled, check `IsInitialized()`, and `defer
db.Close()`.

Emit the warning when it is actually warranted:

```go
if serveConfig.APIEnabled && serveConfig.APIToken == "" && !isLoopback(serveConfig.Bind) {
	logrus.Warn("API is enabled without a token and bound to a non-loopback address; " +
		"anyone who can reach this port can read and annotate your feeds. " +
		"Set serve.api.token or FEEDSPOOL_API_TOKEN, or bind to 127.0.0.1.")
}
```

`isLoopback("")` is false — an empty bind means all interfaces.

`internal/config/config.go`: add `Bind` to `ServeConfig` and a nested
`APIConfig{Enabled bool; Token string}`; load both in `LoadConfig`.

- [ ] **Step 6: Run tests**

Run: `make format && make lint && make test`
Expected: PASS. Existing `serve` tests must be untouched.

- [ ] **Step 7: Manual smoke test**

```bash
make build
./feedspool --database ./feeds.db serve --api --bind 127.0.0.1 --port 8899 &
curl -s localhost:8899/api/v1/ | jq
curl -s 'localhost:8899/api/v1/items?limit=2' | jq '.data[].title, .next_cursor'
curl -s 'localhost:8899/api/v1/items?limitt=2' | jq
kill %1
```

- [ ] **Step 8: Commit**

```bash
git add internal/api internal/server internal/config cmd/serve.go feedspool.yaml.example
git commit -m "feat: Mount the API on serve behind --api with optional bearer auth"
```

---

### Task 8: OpenAPI document, drift test, and documentation

**Files:**
- Create: `internal/api/openapi.yaml`, `internal/api/openapi.go`, `internal/api/openapi_test.go`
- Modify: `MANUAL.md`, `README.md`

- [ ] **Step 1: Write the failing test**

```go
func TestOpenAPICoversEveryRoute(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openAPIDocument, &doc); err != nil {
		t.Fatal(err)
	}

	documented := map[string]bool{}
	for path, methods := range doc.Paths {
		for method := range methods {
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}

	registered := map[string]bool{}
	for _, r := range routes {
		registered[r.method+" "+r.pattern] = true
	}

	for key := range registered {
		if !documented[key] {
			t.Errorf("route %s is not documented in openapi.yaml", key)
		}
	}
	for key := range documented {
		if !registered[key] {
			t.Errorf("openapi.yaml documents %s, which is not a registered route", key)
		}
	}
}
```

`gopkg.in/yaml.v3` is already an indirect dependency via viper; promoting it to
direct adds no new module to the build. Confirm with `go mod why gopkg.in/yaml.v3`
before relying on that. If it would add a direct dependency the project does not
want, parse the `paths:` keys with a small hand-rolled scanner instead — the
document's shape is under our control.

Normalize the route pattern (`/api/v1/{$}` → `/api/v1/`) on one side before
comparing, and document which side does it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run OpenAPI -v`
Expected: FAIL — no document.

- [ ] **Step 3: Write `openapi.yaml` and the embed**

```go
//go:embed openapi.yaml
var openAPIDocument []byte
```

Serve it at `GET /api/v1/openapi.yaml` with `Content-Type: application/yaml`.
Document every route, every parameter from the spec's tables, the error
envelope, and the collection envelope. Use `$ref` components for `Item`,
`Feed`, `Annotation`, `Error`, and `Collection` so the field lists appear once.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -v`
Expected: PASS in both directions.

- [ ] **Step 5: Write the documentation**

`MANUAL.md` gains an "HTTP API" section covering: enabling it, the config
block, the token and the `FEEDSPOOL_API_TOKEN` env var, the endpoint table,
`curl` examples for the common cases, cursor pagination with a paging loop,
the `include` parameter, **the `archived` default diverging from `feedspool
items`**, the `q` title-only limitation, the annotation `kind` charset
restriction and its CLI asymmetry, and the v1 compatibility rule.

`README.md` gets three or four lines pointing at the manual section.

- [ ] **Step 6: Final verification**

```bash
make format && make lint && make test
go test -race ./...
make build && ./feedspool serve --help
```

- [ ] **Step 7: Commit**

```bash
git add internal/api MANUAL.md README.md
git commit -m "docs: Add OpenAPI document, drift test, and API manual section"
```

---

## Self-Review

**Spec coverage.** Identity → Task 1. Annotation uniqueness migration → Task 2.
Keyset pagination and the timestamp caveat → Task 3. DTOs, `include`, errors,
param strictness → Task 4. Read endpoints including `/status` → Task 5. Write
endpoints → Task 6. Auth, binding, warning, server wiring, config → Task 7.
OpenAPI, drift test, docs → Task 8. `busy_timeout` is deliberately absent
(PR #59). Search beyond title substring is deliberately absent (#58). Timestamp
normalization is deliberately absent (#60).

**Type consistency.** `ItemPage`/`ItemCursor`/`FeedPage` are defined in Task 3
and consumed in Task 5. `includeSet`, `writeError`, `writeJSON`, and the DTO
functions are defined in Task 4 and consumed in Tasks 5 and 6. `routes` is
defined in Task 5 and read by the drift test in Task 8. `Config.Token` is set
in Task 7 and read by `requireAuth` in the same task.

**Known duplication.** `ids.ItemID` and `database.ItemHashID` compute the same
value in two packages, because `internal/database` must not depend on
`internal/ids`. Task 3 Step 5 adds a test asserting they agree.
