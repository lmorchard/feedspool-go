# Embedding Substrate Implementation Plan (issue #30, slice A)

**Goal:** Persist per-item vector embeddings from a configurable provider, with
`feedspool embed` to fill a date window and `feedspool related` to prove the
vectors mean something.

**Approach:** Embeddings derive from `item_text` (so FTS5 and the embedder
cannot drift, and staleness is a SQL hash comparison). Storage is keyed
`(item_id, model_id)` so two models coexist for a bake-off. Network I/O runs
outside any transaction via a new `RunStagedBackfill` driver, because
`SetMaxOpenConns(1)` makes an open transaction hold the whole database.

**Tech stack:** Go 1.26, `modernc.org/sqlite` (cgo-free), Cobra + Viper,
`internal/httpclient`, Ollama `/api/embed`.

**Phases 1–3 are foundations with their own tests; phase 4 is the first
end-to-end user-visible slice.** If phase 5 fails, phases 1–4 still deliver a
working `embed`. If phase 4 fails, phases 1–3 are independently tested units.

---

## Phase 1: `internal/embed` — provider, templates, batching

Delivers a testable client that turns text into vectors: batching, per-model
input templates, per-model `num_ctx`, and a unit-norm assertion. No database.

**Files:**
- Create: `internal/embed/embed.go` — `Provider`, `Version`, model defaults, `Config`
- Create: `internal/embed/ollama.go` — the HTTP provider
- Create: `internal/embed/embed_test.go` — model-default resolution, norm check
- Create: `internal/embed/ollama_test.go` — batching/templating against `httptest`

**Key changes:**

```go
// Version identifies this derivation in item_embeddings bookkeeping. Bump it
// whenever the embedded input changes for the same item text -- which includes
// changing a model's input template below.
const Version = 1

type Provider interface {
	ModelID() string
	// Embed accepts any number of texts and handles batching internally.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// ModelDefaults are the per-model knobs measured in research.md §2 and §5.
// Template is a fmt pattern with exactly one %s.
type ModelDefaults struct {
	Template  string
	NumCtx    int
	BatchSize int
}

// Keyed by model name with any ":tag" stripped, so "qwen3-embedding:0.6b" and
// "qwen3-embedding" resolve the same.
var modelDefaults = map[string]ModelDefaults{
	"nomic-embed-text": {Template: "clustering: %s", NumCtx: 8192, BatchSize: 8},
	"qwen3-embedding":  {Template: "%s", NumCtx: 32768, BatchSize: 64},
	"embeddinggemma":   {Template: "title: none | text: %s", NumCtx: 2048, BatchSize: 32},
}

// DefaultsFor falls back to a conservative unknown-model profile rather than
// erroring, so a model we have not characterised still works.
func DefaultsFor(model string) ModelDefaults
```

`num_ctx` is not cosmetic: Ollama caps `nomic-embed-text` at 2K unless it is
set, and 2.3% of a real 2-day window exceeds 2,048 tokens (`research.md` §5).

```go
type Config struct {
	BaseURL   string
	Model     string
	APIKey    string
	BatchSize int // 0 = per-model default
	NumCtx    int // 0 = per-model default
	Timeout   time.Duration
}

func NewOllamaProvider(cfg Config, client *httpclient.Client) *OllamaProvider
```

Request and response shapes for `POST {BaseURL}/api/embed`:

```go
type embedRequest struct {
	Model   string         `json:"model"`
	Input   []string       `json:"input"`
	Options map[string]any `json:"options,omitempty"` // {"num_ctx": N}
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}
```

`Embed` splits `texts` into `BatchSize` chunks, applies `Template` to each
text, POSTs each chunk, and concatenates. It errors if a response returns a
different number of embeddings than it sent, or if vector lengths are not
uniform across the whole call.

```go
// CheckUnitNorm verifies the provider returns L2-normalised vectors, which is
// what lets similarity be a plain dot product. Both measured models return
// norm 1.0000 (research.md §2); this is what stops that assumption rotting
// silently if a provider changes. Checked once per Provider instance, on the
// first vector of the first batch.
func CheckUnitNorm(v []float32) error // tolerance 1e-3
```

**Verification — automated:**
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `go test ./internal/embed -v` — 20 texts at batch 8 issues exactly 3 requests sized 8/8/4
- [ ] `go test ./internal/embed -run TestTemplate -v` — captured request body shows `clustering: ` prefix for nomic, bare text for qwen3
- [ ] `go test ./internal/embed -run TestNumCtx -v` — captured body carries `options.num_ctx` = 8192 for nomic
- [ ] `go test ./internal/embed -run TestEmbedErrors -v` — count mismatch, ragged dims, non-200, and malformed JSON each return an error
- [ ] `go test ./internal/embed -run TestUnitNorm -v` — a non-normalised vector is rejected
- [ ] Empty input issues zero HTTP requests and returns an empty slice

**Verification — manual:**
- [ ] Against real Ollama, an env-gated test (`FEEDSPOOL_EMBED_LIVE=1`, skipped otherwise so CI stays hermetic) embeds one string with each of `nomic-embed-text` and `qwen3-embedding:0.6b` and reports 768 and 1024 dims respectively

---

## Phase 2: Migration 12, vector codec, and storage

Delivers the `item_embeddings` table and the encode/decode/upsert path, with
two models proven to coexist for the same item.

**Files:**
- Create: `internal/database/vector.go` — `EncodeVector` / `DecodeVector`
- Create: `internal/database/item_embedding.go` — `upsertItemEmbeddingTx`, `readItemEmbeddings`
- Modify: `internal/database/schema.sql` — append `item_embeddings` (DDL identical to the migration, per the `item_text` / migration 11 arrangement)
- Modify: `internal/database/migrations.go` — `migrationVersion12`, bump `maxMigrationVersion`, description, DDL entry
- Create: `internal/database/vector_test.go`
- Create: `internal/database/item_embedding_test.go`
- Modify: `internal/database/migrations_test.go` — migration 12 applies to a v11 database and is idempotent

**Key changes:**

```sql
CREATE TABLE IF NOT EXISTS item_embeddings (
    item_id           INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    model_id          TEXT    NOT NULL,
    dims              INTEGER NOT NULL,
    vector            BLOB    NOT NULL,
    source_hash       TEXT    NOT NULL,
    generator_version INTEGER NOT NULL,
    computed_at       DATETIME NOT NULL,
    PRIMARY KEY (item_id, model_id)
);
CREATE INDEX IF NOT EXISTS idx_item_embeddings_model
    ON item_embeddings(model_id, item_id);
```

No index for the date window: migration 9 already indexes
`julianday(COALESCE(published_date, first_seen))` (`migrations.go:90-92`).

**Migration 12 creates the table and stops.** Unlike `applyMigration11`
(`migrations.go:542`), it must NOT backfill: embedding requires network access
and a configured provider, and a migration that makes HTTP calls would turn
any `IsInitialized` call — including a `serve` startup — into a network
operation. Backfill is explicit, via `feedspool embed`.

```go
// EncodeVector stores float32 little-endian, dims*4 bytes.
func EncodeVector(v []float32) []byte

// DecodeVector errors unless len(blob) == dims*4, so a truncated or
// wrong-model blob fails loudly instead of yielding garbage similarity.
func DecodeVector(blob []byte, dims int) ([]float32, error)
```

```go
// upsertItemEmbeddingTx is the only place item_embeddings rows are written.
func upsertItemEmbeddingTx(tx *sql.Tx, itemID int64, modelID string,
	vector []float32, sourceHash string) error
```

Uses `ON CONFLICT(item_id, model_id) DO UPDATE SET ...`, mirroring
`upsertItemTextTx` (`item_text.go:177`), and writes `computed_at` via
`formatDatabaseTime(time.Now().UTC())`.

**Verification — automated:**
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `go test ./internal/database -run TestVector -v` — round-trip preserves values bit-for-bit; `len(blob) == dims*4`
- [ ] `go test ./internal/database -run TestDecodeVectorRejects -v` — blob one byte short, one byte long, and empty all error
- [ ] `go test ./internal/database -run TestItemEmbeddingTwoModels -v` — one item holds a 768-dim nomic row and a 1024-dim qwen3 row simultaneously, each readable independently
- [ ] `go test ./internal/database -run TestItemEmbeddingUpsert -v` — re-upserting the same `(item_id, model_id)` replaces rather than duplicating
- [ ] `go test ./internal/database -run TestMigration12 -v` — applies to a v11 database, re-applying is a no-op, `maxMigrationVersion` is 12
- [ ] `go test ./internal/database -run TestItemEmbeddingCascade -v` — deleting the parent item removes its embedding rows

**Verification — manual:**
- [ ] `./feedspool --database /tmp/feedspool-issue30/spool.db status` on a fresh copy of `data/feeds-backup.db` migrates 11→12 and reports 34,613 items; note the elapsed time (expect well under the 17.6s that included the `item_text` backfill, since migration 12 does DDL only)

---

## Phase 3: `RunStagedBackfill`

Delivers a backfill driver whose slow phase provably cannot hold a
transaction. `RunBackfill` and everything #58 built are untouched.

**Files:**
- Create: `internal/database/staged_backfill.go`
- Create: `internal/database/staged_backfill_test.go`

**Key changes:**

```go
// StagedInput is one item's embed input, read inside a short transaction.
type StagedInput struct {
	ItemID     int64
	Text       string
	SourceHash string
}

// StagedResult is what Compute produces for one item.
type StagedResult struct {
	ItemID     int64
	SourceHash string
	Vector     []float32
}

// StagedBackfill is DerivedBackfill for generators whose work is slow and
// external. The phases exist so Compute can run with no transaction open:
// db.go:48 sets SetMaxOpenConns(1), so an open *sql.Tx holds the only
// connection and blocks every other database user in the process for its
// duration. Compute deliberately takes no *sql.Tx -- the type signature is
// what enforces the rule.
type StagedBackfill interface {
	Name() string
	Version() int
	NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error)
	ReadInputs(tx *sql.Tx, ids []int64) ([]StagedInput, error)
	Compute(ctx context.Context, inputs []StagedInput) ([]StagedResult, error)
	Write(tx *sql.Tx, results []StagedResult) error
	Remaining(tx *sql.Tx) (int64, error)
}

func (db *DB) RunStagedBackfill(ctx context.Context, gen StagedBackfill,
	batchSize int, progress func(done, total int64)) error
```

Per batch, in this order: transaction 1 runs `NextBatch` + `ReadInputs` and is
rolled back (read-only, mirroring `backfillRemaining` at `backfill.go:69`);
`Compute` runs with **no transaction open**; transaction 2 runs `Write` and
commits. The cursor advances past the largest ID in the batch so a row the
generator cannot complete is skipped next pass rather than selected forever —
the same termination argument as `backfill.go:57-62`.

`progress` keeps `RunBackfill`'s exact `func(done, total int64)` shape and
"resumed runs count from zero against the smaller remainder" semantics, so
`cmd/embed.go` can report the way `cmd/reindex.go` does.

The invariant test, which is the reason this driver exists:

```go
// If RunStagedBackfill ever regresses to holding a transaction across
// Compute, this read cannot obtain the single pooled connection and fails on
// the deadline instead of returning.
func TestRunStagedBackfillHoldsNoTransactionDuringCompute(t *testing.T) {
	// ... gen.Compute does:
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	var one int
	if err := db.conn.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return nil, fmt.Errorf("a transaction was open during Compute: %w", err)
	}
}
```

**Verification — automated:**
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `go test ./internal/database -run TestRunStagedBackfillHoldsNoTransactionDuringCompute -v` — passes
- [ ] Same test fails when the driver is temporarily changed to pass its read transaction into `Compute` (prove the test has teeth before trusting it; revert immediately)
- [ ] `go test ./internal/database -run TestRunStagedBackfillResumes -v` — a generator that errors mid-run leaves earlier batches committed, and a second run processes only the remainder
- [ ] `go test ./internal/database -run TestRunStagedBackfillTerminates -v` — a generator whose `Write` is a no-op (so rows stay selectable) still terminates rather than looping
- [ ] `go test ./internal/database -run TestRunStagedBackfillProgress -v` — `progress` is called once per committed batch with a monotonic `done`
- [ ] `go test ./internal/database -run TestRunStagedBackfillCancel -v` — a cancelled context stops the run and returns the context error

**Verification — manual:**
- [ ] None — this phase has no user-visible surface

---

## Phase 4: The embed generator and `feedspool embed`

First end-to-end slice: `feedspool embed --last 2d` fills a window.

**Files:**
- Modify: `internal/database/item_embedding.go` — `itemEmbeddingBackfill`, `EmbedItems`, `CountItemsMissingText`
- Modify: `internal/config/config.go` — `EmbedConfig`, defaults as named constants
- Modify: `cmd/root.go` — `viper.SetDefault` entries
- Create: `cmd/embed.go`
- Modify: `internal/database/item_embedding_test.go` — staleness and window cases
- Modify: `internal/config/config_test.go` — `EmbedConfig` loading

**Key changes:**

```go
type itemEmbeddingBackfill struct {
	provider embed.Provider
	modelID  string
	since    time.Time
	until    time.Time
	force    bool
}

func (g *itemEmbeddingBackfill) Name() string { return "itemembedding" }
func (g *itemEmbeddingBackfill) Version() int { return embed.Version }
```

The work-selection predicate. **Reuse `aliasedEffectiveDateExpression`
verbatim** (`item_repository.go:20-28`) — migration 9's index only applies on a
textual match, so a hand-written equivalent `COALESCE` silently full-scans:

```go
func (g *itemEmbeddingBackfill) workCondition() (condition string, args []any) {
	window := aliasedEffectiveDateExpression + " >= julianday(?) AND " +
		aliasedEffectiveDateExpression + " <= julianday(?)"
	args = []any{formatDatabaseTime(g.since), formatDatabaseTime(g.until)}
	if g.force {
		return window, args
	}
	return window + ` AND (e.item_id IS NULL
			OR e.source_hash <> t.source_hash
			OR e.generator_version <> ?)`,
		append(args, g.Version())
}
```

`NextBatch` binds in this order — **`model_id` first, because it sits in the
JOIN, not the WHERE**:

```go
// args: modelID, afterID, since, until, [version if !force], limit
//nolint:gosec // Safe: condition is built from package constants, not user input
query := `
	SELECT i.id
	FROM items i
	JOIN item_text t            ON t.item_id = i.id
	LEFT JOIN item_embeddings e ON e.item_id = i.id AND e.model_id = ?
	WHERE i.id > ? AND (` + condition + `)
	ORDER BY i.id
	LIMIT ?`
```

`Remaining` uses the same FROM/JOIN/WHERE with `COUNT(*)` and no cursor or
limit, mirroring `itemTextBackfill.Remaining` (`item_text.go:89`).

`ReadInputs` selects `t.title, t.summary, t.body, t.source_hash` for the batch
and joins them into one string. It reads **every row before any writing
begins**, for the reason `readItemTextSources` documents at
`item_text.go:130`: the pool is capped at one connection.

`Compute` calls `g.provider.Embed(ctx, texts)` and pairs each vector back to
its `StagedInput` by index. `Write` calls `upsertItemEmbeddingTx` per result.

```go
// EmbedItems fills embeddings for the window. Mirrors ReindexItemText
// (item_text.go:206) in shape.
func (db *DB) EmbedItems(ctx context.Context, provider embed.Provider,
	since, until time.Time, force bool, batchSize int,
	progress func(done, total int64)) error

// CountItemsMissingText reports items in the window that have no item_text
// row, which the generator's inner JOIN excludes. Reported so a user is told
// to run reindex rather than silently getting fewer embeddings than items.
func (db *DB) CountItemsMissingText(since, until time.Time) (int64, error)
```

Config, following `internal/config/config.go`'s typed-sub-struct pattern:

```go
type EmbedConfig struct {
	BaseURL   string `mapstructure:"base_url"`
	Model     string `mapstructure:"model"`
	BatchSize int    `mapstructure:"batch_size"` // 0 = per-model default
	NumCtx    int    `mapstructure:"num_ctx"`    // 0 = per-model default
	// APIKey is deliberately not exposed as a command-line flag: a token
	// passed on the command line ends up in ps output. Config file or
	// FEEDSPOOL_EMBED_API_KEY only. Follows APIConfig.Token (config.go:77).
	APIKey string `mapstructure:"api_key"`
}
```

Defaults in `cmd/root.go`: `embed.base_url` = `http://localhost:11434`,
`embed.model` = `nomic-embed-text`, `embed.batch_size` = 0, `embed.num_ctx` = 0.

`cmd/embed.go` maps `config.EmbedConfig` onto `embed.Config` — they are
deliberately separate types, one for Viper binding and one for the provider —
and fills `embed.Config.Timeout` from the **global** `cfg.Timeout`, which
`EmbedConfig` therefore does not duplicate.

`cmd/embed.go` registers `--last` (duration), `--since` / `--until` (RFC3339),
`--model`, `--force`, `--dry-run`, `--batch-size`, and resolves the window with
`database.ParseTimeWindow(embedLast, embedSince, embedUntil)`. It rejects
`--last` together with `--since`/`--until`, matching how `render` rejects
`--max-age` with `--start`/`--end` (`cmd/render.go:259`). Progress goes to
stdout, not logrus, for the reason `cmd/reindex.go:44` documents. `--dry-run`
reports `Remaining` and `CountItemsMissingText` and makes no HTTP call.

On completion it prints items embedded, items skipped for missing `item_text`
(naming `feedspool reindex` when that count is non-zero), and **elapsed wall
time** — the run takes tens of seconds to minutes, so the duration is part of
the result, not decoration.

**Verification — automated:**
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `go test ./internal/database -run TestEmbedStaleness -v` — all four cases select work: no row for this model; `source_hash` differs from `item_text`; `generator_version` differs; and (negative) an up-to-date row selects nothing
- [ ] `go test ./internal/database -run TestEmbedWindow -v` — window bounds are inclusive at both ends; an item with NULL `published_date` is selected on its `first_seen`; an item outside the window is not selected
- [ ] `go test ./internal/database -run TestEmbedModelIsolation -v` — embedding model A leaves model B's rows untouched and still reports model B's items as outstanding
- [ ] `go test ./internal/database -run TestEmbedForce -v` — `force` selects an up-to-date row that the normal predicate skips
- [ ] `go test ./internal/database -run TestEmbedSkipsItemsWithoutText -v` — an item with no `item_text` row is not embedded and is counted by `CountItemsMissingText`
- [ ] `go test ./internal/database -run TestEmbedUsesEffectiveDateIndex -v` — `EXPLAIN QUERY PLAN` for the generator's query mentions `idx_items_effective_date`
- [ ] `go test ./internal/config -run TestEmbedConfig -v` — defaults load; `FEEDSPOOL_EMBED_API_KEY` is picked up
- [ ] `./feedspool embed --last 2d --since 2026-01-01T00:00:00Z` exits non-zero with a message naming the conflict

**Verification — manual:**

The spool is a fixed snapshot but `--last 2d` is relative to *now*, so the
window count drifts as the snapshot ages. Check these against each other
rather than against the 1,898 measured on 2026-09-15.

- [ ] `embed --last 2d --dry-run` on the spool copy reports a count that
      **matches `items --since <same instant> --format json | jq length`** —
      the two must agree, whatever the absolute number is
- [ ] `--dry-run` makes no HTTP call: it still succeeds with Ollama stopped
- [ ] `embed --last 2d` completes, printing progress, and embeds the number
      `--dry-run` predicted
- [ ] Re-running immediately reports **0** outstanding (staleness works)
- [ ] `--force` then `--dry-run` reports the **full window** again, equal to
      the first dry-run count
- [ ] Record wall time and observed items/sec; compare to the 36/s (nomic) and
      80/s (qwen3) in `research.md` §2, expecting to **beat** both, since real
      items (~100-token median) are far shorter than the 307-token benchmark

---

## Phase 5: `feedspool related`

Makes the vectors inspectable — the read surface that distinguishes a correct
embedding from a scrambled one, and the scan-and-rank path clustering reuses.

**Files:**
- Create: `internal/database/similarity.go` — `NearestItems`
- Create: `cmd/related.go`
- Create: `internal/database/similarity_test.go`

**Key changes:**

```go
type Neighbor struct {
	Item       *Item
	Similarity float64
}

// NearestItems ranks every item carrying a vector for modelID against the
// subject item, by dot product. Both measured models return L2-normalised
// vectors (research.md §2), so the dot product IS cosine similarity -- no
// normalisation here.
//
// Reads all candidate vectors into memory before resolving item rows, for the
// reason readItemTextSources documents at item_text.go:130: the pool is capped
// at one connection, so a query issued while another result set is open would
// contend with it.
//
// A full scan is deliberate. A 2-day window is ~1,898 vectors (research.md
// §5), so this is ~1.5M multiply-adds -- microseconds. ANN indexing would not
// pay for itself until millions of vectors, and sqlite-vec is unavailable
// under CGO_ENABLED=0 anyway.
func (db *DB) NearestItems(modelID string, itemID int64, limit int) ([]Neighbor, error)
```

The subject item is excluded from its own results. Items whose stored `dims`
disagree with the subject's are skipped rather than compared — that can only
happen across models, and the `model_id` filter already prevents it, so the
check is a guard against a corrupt row, not an expected path.

`cmd/related.go` resolves its subject **by link**, mirroring `cmd/item.go`:
positional link, `--feed` + `--guid` for a link matching more than one item,
and a non-zero exit naming each matching feed URL and GUID on ambiguity. Flags:
`--limit` (default 10), `--model` (default `embed.model`), `--format`
(table|json) plus the global `--json`, matching `cmd/item.go:38`.

Exits non-zero with an actionable message when the subject has no embedding
for the requested model, naming the `feedspool embed` command to run.

**Verification — automated:**
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `go test ./internal/database -run TestNearestItemsRanking -v` — hand-built unit vectors rank in the expected order, with an orthogonal vector scoring ~0 and an identical one ~1
- [ ] `go test ./internal/database -run TestNearestItemsExcludesSelf -v` — the subject is absent from its own neighbours
- [ ] `go test ./internal/database -run TestNearestItemsRespectsModel -v` — neighbours come only from the requested `model_id`
- [ ] `go test ./internal/database -run TestNearestItemsLimit -v` — `limit` truncates after ranking, not before
- [ ] `go test ./internal/database -run TestNearestItemsNoEmbedding -v` — a subject with no vector for the model returns a distinguishable error, not an empty list
- [ ] `./feedspool related https://example.com/nonexistent` exits non-zero

**Verification — manual:**
- [ ] `./feedspool --database /tmp/feedspool-issue30/spool.db related <link from the embedded window>` returns neighbours whose titles are **recognisably on the same topic**. This is the judgement the whole slice exists to enable — a timing number cannot substitute for reading the titles.
- [ ] Similarity scores are in a sane range (top neighbour well under 1.0 but clearly above the tail) rather than all clustered at one value, which would indicate a broken codec
- [ ] `--json` output parses and carries the same ordering as the table

---

## Phase 6: Docs, smoke test, and the model bake-off

Doc and verification phase. **No new behavior**, so TDD is explicitly opted
out; the smoke script is itself the test.

**Files:**
- Create: `scripts/smoke-embed.sh` — matching `scripts/smoke-reindex-force.sh`'s style
- Modify: `MANUAL.md` — `embed` and `related` subcommand sections, `item_embeddings` under Data Model, an `embed:` config block
- Modify: `docs/dev-sessions/2026-09-15-1732-issue-30-embeddings/notes.md` — results and the bake-off verdict

**Key changes:**

`scripts/smoke-embed.sh <path-to-spool-copy> [model]`, asserting properties
rather than printing numbers — the teeth `smoke-reindex-force.sh` has:

1. Every windowed item with an `item_text` row ends up with a vector: after
   `embed --last 2d`, `embed --last 2d --dry-run` reports 0 outstanding.
2. **Idempotence:** a second `embed --last 2d` embeds 0 items. Fails if it
   re-embeds, which is how a broken staleness predicate would present.
3. `related` on a sampled item returns `--limit 10` neighbours with
   similarities in `(0, 1]` and strictly descending. Fails on 0 results.
4. `--force` re-selects the whole window, proving force is not a no-op.
5. Fails if the window held fewer than 50 items — too small to prove anything,
   mirroring `smoke-reindex-force.sh`'s "finished too fast" guard.

The script takes a **copy** path and says so in its usage line, because it
writes to the database.

**Verification — automated:**
- [ ] `make format` clean
- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] `bash -n scripts/smoke-embed.sh` parses
- [ ] `scripts/smoke-embed.sh /tmp/feedspool-issue30/spool.db nomic-embed-text` passes all five assertions
- [ ] `scripts/smoke-embed.sh` on a fresh copy with `qwen3-embedding:0.6b` passes all five assertions

**Verification — manual:**
- [ ] `MANUAL.md` describes `embed` and `related` accurately enough to use them without reading the source; the `num_ctx` default and its reason are documented
- [ ] **Bake-off verdict recorded in `notes.md`:** for 5–10 sampled items, `related` output under nomic and under qwen3 side by side, with a judgement on which produces more topically coherent neighbours and whether the configured default should change
- [ ] Record in `notes.md`: migration 12 time, full-window embed wall time and items/sec per model, on-disk growth, and idle `embed` time (compare to #58's 5.07s idle `reindex`, which the index should beat)
- [ ] Confirm `data/feeds-backup.db` mtime is unchanged — all work went to copies
