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
type ModelDefaults struct {
	Prefix    string
	NumCtx    int
	BatchSize int
}

// Keyed by model name with any ":tag" stripped, so "qwen3-embedding:0.6b" and
// "qwen3-embedding" resolve the same. A function, not a package var, following
// the getMigrations() precedent -- gochecknoglobals flags the var form.
func modelDefaults() map[string]ModelDefaults {
	return map[string]ModelDefaults{
		ModelNomicEmbedText: {Prefix: nomicPrefix, NumCtx: nomicNumCtx, BatchSize: nomicBatchSize},
		ModelQwen3Embedding: {Prefix: "", NumCtx: qwen3NumCtx, BatchSize: qwen3BatchSize},
		ModelEmbeddingGemma: {Prefix: gemmaPrefix, NumCtx: gemmaNumCtx, BatchSize: gemmaBatchSize},
	}
}

// DefaultsFor falls back to a conservative unknown-model profile rather than
// erroring, so a model we have not characterized still works.
func DefaultsFor(model string) ModelDefaults
```

**Deviation from the original plan, applied during phase 1.** This was going to
be `Template string`, a `fmt` pattern applied with `fmt.Sprintf(p.template, t)`.
Two reasons it became a plain `Prefix` instead:

1. `go vet`'s printf analyzer (which golangci-lint runs) flags a
   non-constant format string, so the `Sprintf` form would have needed a
   `//nolint` to ship.
2. All three real templates are pure prefixes, so `p.prefix + text` is both
   simpler and cannot misbehave on text containing `%`.

The named constants (`nomicNumCtx`, `nomicPrefix`, …) exist because `mnd` flags
bare 8192/2048 and `goconst` flags repeated literals. Values unchanged.

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
- [x] `make format` clean — gofumpt reflowed two long `fmt.Errorf` calls, no other changes
- [x] `make lint` passes — **0 issues** (first run found 19: 12 `goconst`, 4 `misspell`, 2 `mnd`, 1 `gochecknoglobals`; all fixed, see the deviation note above)
- [x] `make test` passes — **all 19 packages ok**
- [x] `TestEmbedBatchesRequestsAndPreservesOrder` — 20 texts at batch 8 issues exactly 3 requests sized **8/8/4**, and each returned vector is one-hot at its input index, so ordering across batch seams is asserted too
- [x] `TestEmbedAppliesModelPrefix` — captured request body shows `clustering: ` for nomic, bare text for qwen3, `title: none | text: ` for embeddinggemma
- [x] `TestEmbedSendsNumCtx` — captured body carries `options.num_ctx` = **8192** for nomic; `TestEmbedConfigOverridesModelDefaults` confirms an explicit 4096 wins
- [x] `TestEmbedRejectsBadResponses` — **7 subtests**: too few embeddings, too many, ragged dims, HTTP 500, malformed JSON, absent `embeddings` field, unnormalized vectors
- [x] `TestCheckUnitNorm` — 7 subtests; plus `TestCheckUnitNormToleranceIsTight` fails a norm of ~1.049, so the tolerance cannot be widened to uselessness without a test failing
- [x] `TestEmbedEmptyInputMakesNoRequest` — zero HTTP requests, empty slice
- [x] `TestEmbedReadsResponsesLargerThanTheHTTPClientCap` — a 64-vector response (~800KB, past httpclient's 100KB limited-read cap) decodes, confirming `LimitResponseSize` is left unset

**Verification — manual:**
- [x] `FEEDSPOOL_EMBED_LIVE=1 go test ./internal/embed -run TestLiveOllama -v` against real Ollama 0.32.0: **`nomic-embed-text` → 768 dims, `qwen3-embedding:0.6b` → 1024 dims, all vectors unit length.** Skipped without the env var, so CI stays hermetic and `make test` needs no model downloads.

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
- [x] `make format` clean — gofumpt reflowed three multi-line calls
- [x] `make lint` passes — **0 issues** (one `funlen`: migration 12's entry pushed `getMigrations` to 106 lines against a 100 limit; fixed by lifting the DDL to a `migration12DDL` package constant, which keeps the explanation and leaves #58's migration 11 entry untouched)
- [x] `make test` passes — **all 20 packages ok**
- [x] `TestVectorRoundTrip` — compares `math.Float32bits`, so it is bit-for-bit rather than approximate and would catch a silent float64 detour; `TestVectorNaNAndInfRoundTrip` covers NaN/±Inf; `TestEncodeVectorLength` checks `dims*4` at 1/8/768/1024
- [x] `TestEncodeVectorIsLittleEndian` — pins 1.0 as `00 00 80 3F`. A round-trip test alone would pass on big-endian too, and byte order is a storage contract
- [x] `TestDecodeVectorRejectsWrongLength` — **7 subtests**: one byte short, one long, empty, dims larger than blob, dims smaller, zero dims, negative dims
- [x] `TestItemEmbeddingTwoModelsCoexist` — one item holds a 768-dim nomic row and a 1024-dim qwen3 row at once, each read back independently, exactly 2 rows
- [x] `TestItemEmbeddingUpsertReplacesRatherThanDuplicating` — one row survives, carrying the updated vector and hash
- [x] `TestItemEmbeddingCascadesOnItemDelete` — deleting the item removes its embedding rows
- [x] `TestGetItemEmbeddingMissingIsDistinguishable` / `...RejectsCorruptBlob` — absent embedding reports via `IsNoEmbedding`; a blob shorter than its `dims` column errors instead of decoding
- [x] `TestMigration12*` — **6 tests**: present on a fresh schema, registered with a description and `maxMigrationVersion` 12, idempotent over three applications, adds no date index, does not embed, and **`TestMigration12MatchesSchemaFile` diffs the live table definition between schema.sql and the migration** so the two cannot drift
- [x] `TestDotProduct` — 4 subtests plus a width-mismatch rejection

**Verification — manual:**
- [x] Migration 11→12 on the real spool copy (680 MB, 460 feeds, **34,613 items**): **0.62s wall / 0.05s CPU**, announced as `Migrating database to schema version 12: add the item_embeddings table...`. Well under the 17.6s that included the `item_text` backfill, as expected for DDL only.
- [x] Re-running is a no-op — **0.034s**, no migration line, file size unchanged at 680 MB
- [x] `data/feeds-backup.db` mtime unchanged (17:42) — all work went to the copy

**Unplanned fix this phase required.** `rewindPastMigration11` (`item_text_test.go:633`, from #58) deleted only `version = 11` from `schema_migrations`. `GetMigrationVersion` reads `MAX(version)` (`db.go:158`), so once 12 existed the rewind left the database reporting itself as migrated, `RunMigrations` did nothing, and six of #58's tests failed on a missing `item_text` rather than on what they meant to assert. Changed to `version >= 11` so the rewind is real. The helper was only accidentally correct while 11 was the head; a comment now says so, for migration 13.

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
- [x] `make format` clean
- [x] `make lint` passes — **0 issues**
- [x] `make test` passes — **all 20 packages ok**
- [x] `TestRunStagedBackfillHoldsNoTransactionDuringCompute` — passes, and `Compute` ran 3 times so batch boundaries are actually exercised
- [x] **Proved the test has teeth.** Temporarily added a `db.conn.Begin()` held across `Compute` and re-ran: the test **failed** in 0.26s with `could not reach the database during Compute, so a transaction was open across it … context deadline exceeded` — exactly the intended diagnosis, at exactly the probe deadline. Regression reverted; `grep` confirms no trace left. An invariant test for a failure that has never occurred is worth only what its demonstrated failure mode proves.
- [x] `TestRunStagedBackfillResumesAfterFailure` — an injected mid-batch failure leaves earlier batches committed (neither 0 nor all 10 rows), and the second run computes **only** the remainder, asserted as a count rather than just "it finished"
- [x] `TestRunStagedBackfillTerminatesWhenWorkIsNeverCompleted` — a generator whose `Write` is a no-op (rows stay selectable forever) still terminates, and touches each of the 7 items exactly once. Guarded by a 10s watchdog so a regression fails rather than hanging CI
- [x] `TestRunStagedBackfillReportsProgress` — exactly 3 samples for 5 items at batch 2, `total` constant at 5, `done` strictly increasing, final `done` 5
- [x] `TestRunStagedBackfillToleratesNilProgress` — no panic, same contract `RunBackfill` has
- [x] `TestRunStagedBackfillStopsOnCanceledContext` — returns `context.Canceled` and does not process every item
- [x] `TestRunStagedBackfillEmptyWorkSetIsANoOp` — `Compute` never called

**Verification — manual:**
- [x] None needed — no user-visible surface. (The driver gets its real-corpus exercise in phase 4, where `embed` drives it.)

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
- [x] `make format` clean
- [x] `make lint` passes — **0 issues** (first run: 5 `forbidigo`, 3 `nolintlint`, 1 `prealloc` — see the note below)
- [x] `make test` passes — **all 20 packages ok**
- [x] `TestEmbedStaleness` — **4 subtests**: no row for this model is work; `source_hash` differing from `item_text` is work; a rolled-back `generator_version` is work; and the negative that matters most, an up-to-date row selects **nothing** (otherwise every run re-embeds the window)
- [x] `TestEmbedWindowBounds` / `...IsInclusiveAtBothEnds` / `...FallsBackToFirstSeenWhenPublishedIsNull` — only the in-window item is embedded; a window closed exactly on two items selects both; a NULL `published_date` is selected on `first_seen`
- [x] `TestEmbedModelsAreIndependent` — embedding nomic leaves qwen3's items outstanding and vice versa, with neither model's rows disturbed
- [x] `TestEmbedForceReselectsUpToDateRows` — normal predicate 0, forced 1
- [x] `TestEmbedSkipsAndCountsItemsWithoutText` — the textless item is skipped and counted by `CountItemsMissingText`
- [x] **`TestEmbedQueryUsesTheEffectiveDateIndex`** — `EXPLAIN QUERY PLAN` on the real generator query names `idx_items_effective_date`, so migration 9's index is genuinely in use rather than assumed
- [x] `TestEmbedSendsDerivedText` — title, summary and body all reach the provider
- [x] `TestEmbedPropagatesProviderErrors` — a provider failure aborts with the error wrapped and **0 rows written**
- [x] `internal/config`: `TestGetDefaultEmbed`, `TestLoadConfigReadsEmbedSettings`, `TestLoadConfigLeavesEmbedOverridesUnsetAtZero`
- [x] `./feedspool embed --last 2d --since ...` exits **1** with `cannot specify both --last and an explicit range (--since/--until)`

**Verification — manual** (all against the 34,613-item spool copy):
- [x] **`embed --last 2d --dry-run` reported 1263 items; `items --since/--until` over the identical instants returned 1263. Exact match** — the embed predicate and `items` agree, which is what reusing `aliasedEffectiveDateExpression` was for
- [x] `--dry-run` makes no network call — succeeds with `embed.base_url` pointed at a dead port (`127.0.0.1:9`). Confirmed non-spurious: a **real** run against that same config fails with `dial tcp 127.0.0.1:9: connect: connection refused`
- [x] Live run: **1,257 items in 43.0s = 29.2 items/s**, progress printed per batch
- [x] Re-running immediately reports **0 items to embed / Nothing to do**
- [x] `--force --dry-run` re-selects the full window (1,256; the trailing window drifts between invocations, which is why these are checked against each other and not a fixed number)
- [x] **Idle `embed --dry-run`: 0.100s**, against idle `reindex` at 1.544s on the same spool — 15× faster, confirming `research.md` §5's prediction that a window-scoped index-served predicate beats reindex's full scan

**Throughput reading, corrected.** The plan expected to beat 36 items/s. 29.2/s looks like a miss but is not: 35.8/s was nomic at **batch 64**, and nomic's own default batch is **8**, which benchmarked at 32.3/s. So 29.2/s is ~90% of the right baseline, and the missing 10% is the staged driver's two transactions plus the `item_text` read per batch. Comparing against the batch-64 figure was the error.

**Lint notes.** `forbidigo` guards `fmt.Print*` and is managed by an explicit per-file allowlist in `.golangci.yml`; `cmd/embed.go` is added to it, which is the intended extension point rather than a suppression. Three `//nolint:gosec` directives turned out unused (gosec only flags the `fmt.Sprintf` placeholder case) — converted to plain comments so the reasoning survives without a dead directive.

**Unplanned fix.** The spec, plan and config comment all promised `FEEDSPOOL_EMBED_API_KEY`, which would have silently not worked: `root.go` calls `viper.AutomaticEnv()` with no prefix or key replacer, so a dotted key like `embed.api_key` has no usable environment spelling. Added `viper.BindEnv("embed.api_key", "FEEDSPOOL_EMBED_API_KEY")`, matching the `serve.api.token` → `FEEDSPOOL_API_TOKEN` precedent at `cmd/serve.go:72`.

**Test-fixture fix.** `TestEmbedFallsBackToFirstSeenWhenPublishedIsNull` failed at first, and the code was right: `UpsertItem` only writes `first_seen` when the caller supplies it, and `seedItem` did not — so nulling `published_date` left both NULL and `COALESCE` correctly selected nothing. `seedItem` now sets `FirstSeen`, which is the real shape of any item the fetcher has seen, and the test asserts the fixture has one before relying on the fallback.

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
- [x] `make format` clean
- [x] `make lint` passes — **0 issues** (one `nolintlint`: an unused `//nolint:gosec`, converted to a plain comment)
- [x] `make test` passes — **all 20 packages ok**
- [x] `TestNearestItemsRanksByDotProduct` — identical / 45° / orthogonal / opposed vectors rank in that order **and** score 1.0 / 0.7071 / 0 / −1.0. Checking the values as well as the order is what catches a sign error, which can still produce a plausible ordering
- [x] `TestNearestItemsExcludesSelf` — the subject, which would always rank first, is absent
- [x] `TestNearestItemsRespectsModel` — a 1024-dim qwen3 row is not a candidate for a 768-dim nomic subject
- [x] `TestNearestItemsLimitTruncatesAfterRanking` — fixtures seeded worst-first so an early-truncating scan would keep the wrong ones; `limit 1` returns the genuine best match
- [x] `TestNearestItemsWithoutASubjectEmbedding` — distinguishable via `IsNoEmbedding`, so the command can say "run `feedspool embed`"
- [x] `TestNearestItemsSkipsMismatchedWidths` — a corrupt same-model row is skipped with a warning rather than failing the whole query
- [x] `TestNearestItemsWithNoCandidates`, `...RejectsNonPositiveLimit`, `...ReturnsPopulatedItems`

**Verification — manual** (against the 34,613-item spool, 1,257 items embedded):
- [x] **Topical coherence confirmed.** Subject *"OpenAI boss says world 'right to be afraid' but 'should trust' AI firms"* → all 8 neighbours specifically about AI safety/slowdown, from **eight different feeds** (Verge, Ars Technica, Techmeme ×2, pivot-to-ai, The Register, Bloomberg Law via Techmeme). Cross-feed topical clustering is the thing #30 wants, and it is visibly working.
- [x] Similarity scores span a real range rather than clustering at one value: 0.8691 down to 0.4036 across 1,256 candidates, strictly descending throughout (verified programmatically over all 1,256)
- [x] `--json` parses and preserves the table's ordering
- [x] A subject with no embedding exits non-zero naming `feedspool embed`; a nonexistent link exits non-zero

**Finding that matters for slice B.** The similarity distribution for one subject over 1,256 candidates: **median 0.586**, p75 0.623, p95 0.748, p99 0.815, max 0.869, min 0.404. So nomic's baseline for *unrelated* feed items is ~0.55–0.62, **not 0** — the known anisotropy of embedding models, where all vectors occupy a narrow cone. Two consequences for clustering:

1. **A naive absolute threshold will not work.** "Similarity > 0.5" matches essentially everything on this corpus.
2. **The signal lives in the top 1–5%.** `>= 0.85` gave 4 items, `>= 0.80` gave 18, `>= 0.75` gave 60.

Corroborating: a niche subject (orchids/grassland) topped out at 0.7424 with genuinely ecological top-2 and drift by rank 3, while the AI subject sat at 0.87 inside a dense cluster. **Cluster density shows up in the similarity magnitude**, so slice B wants a relative or distribution-aware threshold rather than a constant.

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
- [x] `make format` clean
- [x] `make lint` passes — **0 issues** (one `gocritic emptyStringTest`, fixed)
- [x] `make test` passes — **all 20 packages ok**
- [x] `go test -race` on `internal/embed` and `internal/database` passes (run outside the Makefile with `CGO_ENABLED=1`, as CI does)
- [x] `bash -n scripts/smoke-embed.sh` parses
- [x] `scripts/smoke-embed.sh <fresh copy> nomic-embed-text 2d` — **PASS**: 1,248 embedded in 42.4s, 0 outstanding after, `Nothing to do` on re-run, `--force` re-selected 1,248, `related` ranked 10 neighbours
- [x] `scripts/smoke-embed.sh <fresh copy> qwen3-embedding:0.6b 2d` — **PASS**: 1,236 embedded in 2m21s, same four assertions

**Verification — manual:**
- [x] `MANUAL.md` gains `embed` and `related` subcommand sections, the `embed:` config block, `item_embeddings` under Data Model, and `schema_migrations` bumped to 12. The `num_ctx` behaviour and the input cap are documented with their reasons.
- [x] **Bake-off verdict recorded in `notes.md`: nomic stays the default.** Both models embedded into one database (which is what the composite key is for) and compared through `related`. On a dense topic both are excellent and near-indistinguishable; on a **sparse** topic nomic finds genuinely related items (0.74, 0.73) while qwen3 returns noise from rank one. nomic is also **3.4× faster on real items** (29.2 vs 8.7 items/s) and reliable at a 4× larger context.
- [x] Timings recorded in `notes.md`: migration 12 at 0.62s, embed at 43.0s/2m21s, idle dry-run at 0.100s vs `reindex` 1.544s, storage ~3 KB/item/model
- [x] `data/feeds-backup.db` mtime unchanged — every run used a copy

**Three unplanned findings, all recorded in `notes.md`.**

1. **`qwen3-embedding:0.6b` is unreliable above `num_ctx: 2048`** on Ollama 0.32.0 — its runner dies with HTTP 400 `do embedding request: EOF` at an *unstable* threshold. Its default is now 2048, and the provider **truncates every input** to `num_ctx * 3` chars, because relying on a provider's over-length behaviour is not safe and the spool's largest item is ~57,000 chars. Five new tests cover it. This was not in the plan and the command simply does not work on a real corpus without it.
2. **Similarity thresholds must be relative.** Unrelated items score ~0.59 under nomic, not 0; the signal is the top 1–5%, and the scale differs per model. This is the main input to slice B and the thing most likely to be got wrong from intuition.
3. **The benchmark that predicted qwen3 would be faster was wrong**, because it used uniform synthetic input. Ollama processes a batch at its longest member, so a mixed-length real batch behaves nothing like a uniform one.

**Smoke-script bugs found by running it**, all fixed: an empty-array expansion that trips `set -u` on macOS bash 3.2; an unbounded `--since` that picked the corpus's one future-dated item as the subject; and a subject link that was ambiguous because the BBC feed carries some articles under two GUIDs. It also now distinguishes "already embedded, pass a fresh copy" from "window too small", since that is the likely mistake on a re-run.
