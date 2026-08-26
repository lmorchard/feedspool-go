# Handoff: implement full-text search (#58)

Paste this into a fresh context. It is written for someone with no memory of the
API work that produced it.

---

## The task

Implement [#58](https://github.com/lmorchard/feedspool-go/issues/58) — proper
full-text search over feed items — in the `feedspool-go` repository.

**Standalone.** It does not depend on #30 (embeddings/clustering) and must not
wait for it. Read the comment thread on #58 for why the two are complementary
rather than sequential.

Work in a git worktree. Read `CLAUDE.md` at the repo root first — it has
non-negotiable build and lint rules.

## Read before designing

1. **[#58](https://github.com/lmorchard/feedspool-go/issues/58) and both its
   comments.** The issue body was written against a stale understanding; the
   first comment corrects it (substring search already ships) and the second
   adds the shared-substrate framing. The corrections matter more than the body.
2. **[#30](https://github.com/lmorchard/feedspool-go/issues/30)'s latest
   comment** — for the shared groundwork and the `CGO_ENABLED=0` constraint.
   You are not implementing #30, but you are building a foundation it inherits.
3. **`docs/dev-sessions/2026-08-25-1752-web-api/spec.md`**, specifically
   "Pagination and the `GetItems` path" and the compatibility section. The API
   contract you are extending is described there.
4. **`MANUAL.md`**, the "HTTP API" section.

## Do this first, before anything else

**Verify FTS5 is compiled into `modernc.org/sqlite`.**

Everything in this issue depends on it, the project is `CGO_ENABLED=0`, and
there is no fallback — `mattn/go-sqlite3` is explicitly forbidden by CLAUDE.md
because it would break static binaries and cross-compilation.

A ten-line program that opens an in-memory database and runs
`CREATE VIRTUAL TABLE t USING fts5(x)` settles it.

If FTS5 is **not** available, stop and report. Do not work around it. The
alternatives — a hand-rolled inverted index, or staying on `instr()` — are
different enough in cost and risk that they need a fresh decision, not an
improvised one.

## Coordination hazard: another agent is on #60

[#60](https://github.com/lmorchard/feedspool-go/issues/60) is being worked
right now, and it changes `buildItemsQuery`, `GetItems`, and
`itemInDiscoveryWindow` in `internal/database/item_repository.go`.

**That is the same function you need to touch.** `ItemFilter.Search` lives
inside `buildItemsQuery`.

Before you start editing, check whether #60 has landed. If it has not, either
coordinate or sequence behind it — do not both rewrite that function in
parallel. Rebasing a large search change onto a rewritten query builder is
much worse than waiting.

## Current state, so you do not have to rediscover it

Search already exists as a substring match, in two places that must stay
consistent:

| Location | Code |
| --- | --- |
| `internal/database/item_repository.go:459` (`buildItemsQuery`, backs the CLI) | `instr(lower(i.title), lower(?)) > 0` |
| `internal/database/pagination.go:175` (`ListItems`, backs the API) | same expression |

Surfaces:

- CLI: `feedspool items --search <text>` (`cmd/items.go`)
- API: `GET /api/v1/items?q=<text>` (`internal/api/items.go`, `params.go`)

Both match **titles only** — not summary, not content. That limitation is
deliberate and documented, so that the two surfaces agree.

`maxMigrationVersion` is currently **10**. Your migration is 11 unless
something lands first — check, do not assume.

Two indexes already exist and are relevant background:
`idx_items_effective_date` and `idx_items_feed_effective_date`, both over
`julianday(COALESCE(published_date, first_seen))`.

## Contract questions to settle before writing code

These change the endpoint shape, not just the implementation, so decide them
up front rather than discovering them mid-build.

1. **`?q=` compatibility.** It currently ships as a title substring match, and
   the v1 API contract is *additive only* — renaming a field, changing a type,
   or changing what a parameter matches all require v2. Silently upgrading
   `?q=` to FTS5 changes both the result set and the ordering for an existing
   endpoint. Either FTS5 arrives as an opt-in within v1 (`match=fts`, say), or
   it waits for v2. Pick one deliberately.

2. **`sort=relevance` versus the keyset cursor.** Adding the sort value is
   additive and fine. The problem is that the item cursor is
   `(date_rank, effective_date, id)` and a relevance ordering has no equivalent
   stable natural key. Options: relevance results paginate by offset and the
   API documents why that one ordering differs, or the cursor carries the
   `bm25()` score. Neither is obviously right.

3. **What text is searchable.** Title only, as today? Or title + summary +
   content? Broadening it changes what existing `--search` and `?q=` calls
   return, which is the same compatibility question as (1) wearing a hat.

## Shared substrate — build these as reusable pieces

#30 will need all three. Building them generically costs very little more than
inlining them, and it stops the two features from disagreeing later.

1. **One canonical "what text represents this item?" function.** Title +
   summary + content, HTML stripped, truncated somewhere sensible. FTS5 indexes
   its output; #30's embedder would embed the same output. If each feature
   answers this question separately they will drift, and you get search hits
   over text the clustering never saw.

2. **Recompute-when-stale bookkeeping** — `(item, generator,
   generator_version, computed_at)`. You need it for reindex-on-item-change and
   rebuild-on-tokenizer-change; #30 needs the identical thing for
   re-embed-on-model-change.

3. **A resumable batched backfill** over existing items, so a large database
   can be indexed without one enormous transaction.

## Implementation notes

- **Index maintenance.** Triggers on `items` versus explicit writes in
  `UpsertItem` / `MarkItemsArchived` / the purge paths. Triggers are less code
  to forget; explicit writes are easier to reason about given
  `SetMaxOpenConns(1)`. Decide deliberately and record why.
- **Ranking** is `bm25()`. Confirm it is available in whatever FTS5 build you
  find in step one.
- Compose with the existing filters (`feed_id`, `since`, `seen`, `archived`) —
  search is another `WHERE` condition, not a separate query path.
- Keep the CLI and API in step. They have diverged once already (annotation
  `kind` charset, see #62) and it is a recurring failure mode here.

## Repo-specific rules that will bite you

- **`make build`, never `go build`.** The Makefile injects version metadata and
  names the binary `feedspool`.
- **`CGO_ENABLED=0`** is exported in the Makefile and load-bearing. No
  `mattn/go-sqlite3`, no C extensions, no `-linkmode external`.
- **`make format && make lint && make test`** after every meaningful change.
  `make lint` uses a pinned golangci-lint version; do **not** install a
  different one to dodge a finding. It is strict about `goconst` (hoist
  repeated string literals into constants, including in tests) and
  `gochecknoglobals` (package-level `var` slices and maps need to become
  functions). Budget a cleanup pass after writing any new package.
- **`internal/` gets tests; `cmd/` does not need them.**
- `go test -race` needs cgo and runs outside the Makefile, as CI does it.

## Two lessons from the API work

**`main` moves fast.** The API spec was written against a `main` three commits
stale, and two landed PRs contradicted design decisions in it. `git fetch`
and read recent commits *before* designing, not just before pushing. Check
sibling worktrees under `.claude/worktrees/` too — they reveal what else is
in flight.

**Smoke-test against real data before claiming done.** Point the built binary
at a copy of an actual feed database and run the documented usage pattern end
to end. On the API work this caught two bugs the unit tests structurally could
not, because the tests had seeded tidy fixture values that happened to agree
with the bug. For this issue the equivalent is: index a real database, run
real queries, and check that the results are actually *good* — a search feature
can be entirely correct and still useless, and only inspection tells you which
you have.

## Process

Use the brainstorming skill before implementing — this has real open contract
questions and should not start as code. Then `dev-session` for the artifacts,
in a new `docs/dev-sessions/<timestamp>-fts5-search/`.

Do not touch #30. Do not touch #60.
