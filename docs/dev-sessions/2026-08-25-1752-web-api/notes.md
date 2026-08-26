# Session Notes: HTTP API v1

**Issue:** [#28](https://github.com/lmorchard/feedspool-go/issues/28)
**Branch:** `feat/28-web-api` (worktree at `.claude/worktrees/issue-28-web-api`)
**Status:** Complete, CI green, [PR #61](https://github.com/lmorchard/feedspool-go/pull/61) open for review.

## What shipped

A read/write JSON API at `/api/v1/`, mounted on `serve` behind `--api`. Eleven
endpoints, keyset pagination, optional bearer auth, an embedded OpenAPI
document with a drift test, and manual coverage.

Twelve commits: one per plan task, plus a rebase revision, a review-fix
round, and a test reorganization. Every one is lint-clean and green.

## Decisions Les made

| Question | Answer |
| --- | --- |
| REST vs GraphQL | REST — the consumer is `curl \| jq` |
| First consumer | External tools and scripts |
| Auth | Optional bearer token, **off by default** |
| Mutations | Read + annotation writes |
| Wiring | `serve --api` at `/api/v1/` |
| Annotation duplicates | Unique index + dedupe migration |

I pushed back once, on the open-by-default posture. Les chose it deliberately.
The mitigation that survived is a startup warning plus a new `serve.bind`
option, so the warning only fires when the port is genuinely reachable off-box.

## The thing that cost the most time

**The spec was written against a `main` that was three commits stale.** I only
caught it because I noticed an `issue-57-database-lock` worktree while setting
up my own. Always `git fetch` before designing against the current tree.

Four consequences, all recorded in "Revision 2" of `spec.md`:

- `--search` already shipped as a title substring match, so `?q=` moved from
  deferred into v1 to match.
- `since`/`until` already filter on *discovery time*, which made my proposed
  `first_seen_since` parameter redundant. Dropped.
- Scraped feeds (#55) deliberately leave `published_date` NULL, so the cursor
  had to key on `COALESCE(published_date, first_seen)`, not `published_date`.
- `busy_timeout` was already fixed by open PR #59. Removed from scope.

**I also got a correction wrong in the other direction.** I filed #60 claiming
stored timestamps were un-normalized and that this forced a compromise in the
cursor design. Migration 9 already normalizes them and indexes the
effective-date expression — I had not read it. I corrected the spec and
re-scoped #60 to what is actually left (dropping `GetItems`' now-vestigial
in-memory filter and sort). The `date_rank` cursor component stayed, because it
still covers genuinely-NULL dates cheaply, but the justification is much
narrower than I first wrote.

## Design points worth remembering

**Item IDs are derived, not stored.** `items.id` is an AUTOINCREMENT, and
`purge`-then-refetch reassigns it, so a stored ID would silently start
resolving to a different article. `link` is ambiguous — the CLI's own lookup is
`WHERE link = ? LIMIT 1`. So: `sha256(feed_url + "\n" + guid)`, first 16 hex.

**Feed IDs had to stay 8 hex.** They are already published as static-site URLs
(`feeds/<id>.html`), so widening would break bookmarks. `internal/ids` now
holds both, with a golden test pinning the existing values.

**`internal/database` duplicates the hash rather than importing `internal/ids`,**
to keep the dependency graph one-directional.
`internal/database/hashid_agreement_test.go` is the only thing keeping the two
copies honest. Do not delete it without deleting the duplication.

**Pagination could not reuse `GetItems`.** With a time window set it loads every
matching row, filters and sorts in Go, then truncates — no stable cursor
position. `ListItems` is a separate path. `GetItems` was left alone because the
CLI depends on its behavior.

**The keyset predicate is written out longhand,** not as a SQLite row-value
comparison, because `date_rank` ascends while the date component may descend
and one row-value comparison cannot express mixed directions.

## Two bugs found by actually running it

Both would have shipped on unit tests alone.

1. **`--dir` defaults to `./build`**, so `Dir` is effectively never empty and
   API-only mode failed validation on a directory the user never asked for.
   `dropMissingStaticDir` handles it, and only when the API is on.
2. **A bare `os.Exit(1)`** in `runServe` swallowed the startup error. Replaced
   with a logged error — which is how the above got diagnosed at all.

Also: registering a JSON catch-all at `/api/v1/` silently costs ServeMux's
automatic 405. A second method-less mux (`pathProbe`) distinguishes an unknown
path from a method mismatch. A unit test caught that one.

The OpenAPI drift test earned its keep immediately: it caught
`method_not_allowed` missing from the documented error enum.

## Code review round

A `/code-review high` pass plus a rerun against a real database turned up eight
things. All fixed; details in the "Address code review findings" commit.

The two that matter most, both invisible to unit tests:

- **`since` was inclusive; the CLI's is exclusive.** `itemInDiscoveryWindow`
  requires `discoveredAt.After(since)`. My SQL used `>=`. Polling with
  `max(discovered_at)` re-delivered the whole boundary batch every time.
- **Timestamps were emitted at whole-second precision.** Real `first_seen`
  values carry microseconds, so the `discovered_at` a client read could not
  round-trip back into `since`. Everything now uses RFC3339Nano.

Both were found by pointing the server at a copy of the real database and
actually running the documented polling loop. The unit tests had seeded
whole-second timestamps, so they agreed with the bug.

Also worth remembering: `discovered_at` and the sort key use **opposite**
precedence, deliberately. `DiscoveredAt()` is `first_seen ?? published_date`
(what `since` filters on); `EffectiveDate()` is `published_date ?? first_seen`
(what ordering uses). I had them backwards at first, and the CLI's `--compact`
output was already right — worth checking existing definitions before writing a
new one.

## Left undone

| Item | Where |
| --- | --- |
| Annotation filtering by kind, `actor` uniqueness, CLI/API charset asymmetry | [#62](https://github.com/lmorchard/feedspool-go/issues/62) |
| FTS5 search — ranking, body matching | [#58](https://github.com/lmorchard/feedspool-go/issues/58) |
| Drop `GetItems`' in-memory filter and sort | [#60](https://github.com/lmorchard/feedspool-go/issues/60) |
| CORS | Nothing needs it; same-origin covers the site |
| Bulk annotation *removal* | No use case; would force DELETE-with-body |
| Raising `SetMaxOpenConns(1)` | Would change fetcher contention as a side effect |

## Annotations: what I learned building on them

Worth reading #41 before touching them again — it states intent the code does not.

- They exist so **agents can keep tool-side state** ("I processed this"), because a
  cursor alone forces the agent to remember its position and that breaks across
  sessions and machines.
- The table is generic on purpose: `kind` + optional `value` + optional `actor`.
  Flag-style kinds (`seen`) leave `value` NULL and mean true by presence;
  value-bearing kinds (`tag`) allow several rows per item.
- **The renderer and `serve` ignore them, deliberately.** #41: no badges, no
  counters, "the newspaper invariant from the README stays intact." Do not drift
  into changing that as a side effect of something else.
- Everything queryable is hardcoded to `kind = 'seen'` in three places. Any other
  kind is writable but not findable at scale. `idx_item_annotations_kind` exists
  and no query uses it — the schema anticipated the gap before the code did.
- Migration 10 here made uniqueness `(feed_url, item_guid, kind, COALESCE(value,''))`
  with **no `actor`**, which diverges from #41. Unreachable today because every
  writer passes `actor = NULL`, but the API is the first surface that can set it.
  All captured in #62.

## If you pick this up cold

- `GetItemByHashID` scans every `(feed_url, guid)` pair and hashes each one,
  because a derived ID cannot be indexed. Milliseconds at personal scale. If it
  ever matters, the fix is a stored generated column with an index, not a
  different ID scheme.
- `make lint` is strict about `goconst` and `gochecknoglobals`. Package-level
  `var` slices need to become functions; repeated string literals need
  constants. Budget for a lint pass after writing any new package.
- Task boundaries in `plan.md` assumed each task would be independently
  lint-clean. That is not true in Go — `unused` and `unparam` are package-wide,
  so the API foundation only goes clean once its handlers exist. Tasks 4–6
  landed as one commit for that reason.
