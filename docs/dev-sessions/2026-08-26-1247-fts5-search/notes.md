# Session Notes: FTS5 Full-Text Search

**Issue:** [#58](https://github.com/lmorchard/feedspool-go/issues/58)
**Branch:** `feat/58-fts5-search` (worktree at `.claude/worktrees/issue-58-fts5-search`)
**Status:** Complete. All seven phases committed; documentation is the last
phase and this note closes it out.

## What shipped

`feedspool items --search` and `GET /api/v1/items?q=` both do real full-text
search now — title, summary, and body, ranked by `bm25()` — replacing the old
case-insensitive title-substring match. Both surfaces accept the same small
query language (implicit AND, `"phrase"`, `-exclude`, `prefix*`); everything
else is literal text, so `C++`, `title:foo`, and `NEAR` are searched for
rather than parsed as syntax. A query of nothing but exclusions is rejected
(exit 1 / `400 invalid_parameter`) rather than silently returning nothing.

`--sort relevance` / `sort=relevance` join `newest`/`oldest` and require a
search; `newest` stays the default even with a search present. A new
`feedspool reindex [--force]` command fills in missing derived text or forces
a full rebuild.

Underneath: migration 11 adds `item_text` (HTML-stripped title/summary/body
plus `source_hash`/`generator`/`generator_version`/`computed_at` bookkeeping)
and an external-content FTS5 virtual table `items_fts`, kept in sync by
triggers on `item_text`. Go derives and writes `item_text` on every item
write; triggers handle deletes, which is what reaches the `ON DELETE CASCADE`
path `DeleteFeed` and purge use without any FTS-aware code there.

See `MANUAL.md` for the user-facing contract (the `items` and `reindex`
subcommand sections, the API's "Reading items" section, the `item_text` /
`items_fts` entries under Data Model, and a worked `bm25()` query under SQL
Recipes) and `spec.md` / `research.md` / `plan.md` in this directory for the
design rationale and phase-by-phase implementation record.

## Smoke test against a real database

Phase 7 called for running the full smoke test against a copy of a real
production spool rather than synthetic fixtures — the kind of test that
catches problems synthetic data can't: is the migration actually fast enough,
does ranking actually look better, are the size assumptions in the spec
right. This ran against a pristine copy of a 19,750-item spool, schema v4 (an
old database that had never seen migrations 5–11).

**Migration.** 23.5 seconds wall time for migrations 5 through 11 together
(not migration 11 alone — this database needed all of them). All 19,750 items
ended up indexed; SQLite's FTS5 integrity check (`INSERT INTO items_fts(items_fts)
VALUES('integrity-check')`) came back clean afterward.

**Disk.** 478,056,448 → 593,018,880 bytes, +24%. That's the cost of a second
copy of every item's stripped text plus the inverted index over it.

**`reindex` with nothing to do.** 5.07 seconds wall time but only 0.6 seconds
of CPU — it's I/O bound scanning for staleness, not doing any real work. Slow
for a no-op; worth knowing if something ever calls `reindex` on a schedule
expecting it to be instant when idle.

**`reindex --force`.** 34.4 seconds — a full rebuild from scratch, somewhat
slower than the original combined migration despite doing less schema work,
which tracks: it's pure backfill with no DDL to amortize the cost against.
Integrity check clean afterward.

**Ranking quality — the judgment that actually matters.** This is the part a
timing number can't tell you: relevance ordering is *visibly* better than date
order on this corpus, not just technically correct.
- `kubernetes` by date surfaces AI-digest aggregator posts that mention the
  word in passing, buried among unrelated content. By relevance, the top four
  results are all genuinely about Kubernetes, with the term in the title.
- `privacy` by date returns a measles outbreak story and an unrelated job
  posting (probably a body-text coincidence). By relevance: a TikTok privacy
  settlement, a privacy compliance role, and a privacy lawsuit — all on
  point.

**Body reach is real, not just theoretical.** Date-ordered results for these
same queries include body-only matches — items where the search term never
appears in the title — that a title-only search could never have surfaced.
This is the actual point of #58, and it's confirmed on real data rather than
just in a unit test with a hand-built fixture.

**Grammar on real data.**
- Phrase narrows: a bare-terms query at 730 hits dropped to 164 once quoted
  as a phrase.
- Exclusion narrows: 449 hits dropped to 228 once a `-term` exclusion was
  added.
- Prefix widens only slightly: `secur` alone was 1327 hits, `secur*` only
  1332. See stemming, below — this is why.

**Truncation cap: fine as-is.** The largest derived body on this corpus is
258,150 bytes against the 262,144-byte (256 KiB) cap — nothing is actually
truncated, but the largest real item sits at 98.5% of the limit. 76 items
(out of 19,750) exceed 64 KiB; the mean is roughly 2.6 KiB. **Answered open
question: leave the cap where it is.** It's comfortably ahead of the largest
real item today, and 98.5% is close enough that raising it "for headroom"
would be premature — revisit only if a future corpus actually hits it.

**Porter stemming: doing real work, kept as-is.** Prefix search (`secur*`
vs `secur`) adds almost nothing on this corpus, and the reason is that porter
stemming already conflates `secure`/`security`/`securing` into the same stem
— so a search for `security` already recall-matches nearly everything a
prefix search would have added by hand. **Answered open question:** this
isn't a wash, it's stemming visibly pulling its weight. Recorded as answered
and kept, not left open.

**CLI and API agree on real data.** With `archived=any`, the API returns
exactly the same 57 items for `kubernetes` as the CLI does, in identical rank
order — the shared query-building code (`search_sql.go`) is doing its job.
Without `archived=any`, the API's `archived=false` default cuts that to 34 —
see below.

**Relevance pagination holds together.** Paging through `kubernetes` results
in pages of 7 (9 pages) collected all 57 ids, all distinct, in the same order
as a single page with `limit=200`. The offset-in-cursor design for
`sort=relevance` (as opposed to the keyset cursor date sorts use) isn't
losing or duplicating rows under real pagination.

**Pre-existing log noise, not introduced here.** Migrating this database
printed four `Failed to rollback transaction` warnings at the default log
level, and nothing else. This is an existing codebase idiom — a transaction
that already committed still runs its deferred rollback, which errors
harmlessly and gets logged as a warning (see `internal/database/db.go:184`
and `internal/database/migrations.go:308`) — not something phase 7's smoke
test introduced. It is, however, what a real upgrade actually looks like on
the console, so it's worth knowing before you go looking for a bug that isn't
there.

## Two things worth knowing that weren't in the original spec

**The API's `archived=false` default bites harder in a search context than a
browse context.** It's already documented as a general divergence between the
CLI (which includes archived items by default) and the API (which doesn't),
but a user typing a search into the API and getting 34 results instead of the
57 the CLI would give them for the same query is a much easier default to
trip over than "my item list is 40% smaller than I expected." `MANUAL.md`'s
search sections now point at `archived=any` explicitly rather than relying on
the reader to have already read the general pagination caveat.

**Upgrading is quiet, not broken.** Migration 11's backfill has no progress
output at the default log level (Warn) — only at `-v` (Info). On a 20k-item
spool that's ~24 seconds of apparent hang during an otherwise-instant
`fetch` or `status` invocation. `MANUAL.md` now says this plainly under
`items_fts` in the Data Model section so a future upgrade isn't mistaken for
a stuck migration.

## Loose end noticed, not fixed

`MANUAL.md`'s `schema_migrations` entry says "Current version: 4," which
predates this work by a long way — the schema is now at version 11. Out of
scope for this phase (nothing here asked for it, and it's not related to
FTS5), but worth a follow-up pass since it's directly under the tables this
phase just documented.

## For whoever picks this up next

- The feature is done and reviewed through phase 6; this phase is
  documentation only, no code changed.
- If `porter` or the 256 KiB cap ever need reconsidering, this note has the
  measurements that justified leaving them alone — don't just eyeball the
  spec's original guesses.
- The smoke test was against one real spool. If a much larger or very
  differently-shaped corpus (e.g., mostly very long body text, or many more
  feeds) becomes available, it would be worth re-running the same checks —
  particularly the truncation-cap headroom and the no-op `reindex` I/O cost,
  since both are workload-shape-dependent.
