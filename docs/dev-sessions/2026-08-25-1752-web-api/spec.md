# Dev Session Spec: HTTP API v1

Closes the design half of [#28](https://github.com/lmorchard/feedspool-go/issues/28).

## Revision 2 — rebased onto current `main`

The first draft was written against a `main` that was three commits stale.
[#53](https://github.com/lmorchard/feedspool-go/pull/53),
[#54](https://github.com/lmorchard/feedspool-go/pull/54), and
[#55](https://github.com/lmorchard/feedspool-go/pull/55) had already landed, and
two of them move things this design depends on. Every decision from the
brainstorm still stands; what follows are adaptations to code that now exists.

| Changed | Why |
| --- | --- |
| `q=` **is in v1** after all | `ItemFilter.Search` already ships as `instr(lower(title), lower(?))` behind `feedspool items --search`. The API exposing it is now a consistency requirement, not scope creep. #58 stays open for FTS5. |
| `first_seen_since` **dropped** | `--since`/`--until` already filter on *discovery time*, not `published_date`. The parameter I proposed is what `since` already does. Exposing both would be two names for one behavior. |
| Sort key is **effective date**, not `published_date` | Ordering is `COALESCE(published_date, first_seen)`. Scraped items (#55) deliberately leave `published_date` NULL, so a `published_date` cursor would mis-sort every scrape feed. |
| Item DTO gains `discovered_at`; `published_date` is **nullable** | Same reason. `--compact` already established `discovered_at` as the name. |
| Feed DTO gains `type`, `scrape_selector` | New columns from #55. |
| `busy_timeout` **removed from scope** | Already fixed by open PR [#59](https://github.com/lmorchard/feedspool-go/pull/59) against issue #57. Doing it here would conflict. |
| Migration `7` → **`10`** | `main` is at `maxMigrationVersion = 9`. |
| Added `GET /api/v1/status` | `GetSpoolStatus` and `FeedSummary` now exist with JSON tags; matching `feedspool status` is nearly free. |
| New caveat: **legacy timestamp formats** | See "Pagination and the timestamp problem" below. This is the one place the design had to bend. |

## Overview

`feedspool serve` today is a static file server with no database handle. This
spec adds a read/write JSON API for the feed database, mounted under
`/api/v1/` on that same server behind a `--api` flag.

The first consumer is external tools and scripts — `curl`, `jq`, agents, and
whatever reader UI comes later — so the contract is shaped for a shell, not for
one specific web page. It is REST, not GraphQL: the client is a pipe, and a
schema layer plus a resolver layer plus a dependency buys nothing there.

The API also lands same-origin with the rendered site, so the build's existing
web components can call it with no CORS work if that becomes interesting.

## Goals

1. Read access to feeds and items with honest pagination and filtering.
2. Annotation writes, matching the existing `annotate` / `mark-seen` semantics.
3. Stable, documented response shapes with a compatibility rule.
4. A published `openapi.yaml` that cannot silently drift from the routes.
5. Leave every existing `feedspool serve` invocation behaving exactly as it
   does today when `--api` is not passed.

## Non-Goals

- Full-text search. Tracked separately; see "Deferred" below.
- CORS configuration.
- Bulk annotation *removal*.
- GraphQL.
- Feed subscription management (`subscribe` / `unsubscribe`) over HTTP.
- Triggering fetches or renders over HTTP.

## Decisions Made

| Question | Decision |
| --- | --- |
| REST vs GraphQL | REST |
| First consumer | External tools and scripts |
| Auth | Optional bearer token, **off by default** |
| Mutations | Read + annotation writes |
| Wiring | `serve --api`, mounted at `/api/v1/` |
| Annotation duplicates | Unique index + dedupe migration |

## Identity

Two derived, stable identifiers, both computed from natural keys.

**Feed ID** — first 8 hex characters of `sha256(feed_url)`. This is the
existing `generateFeedID` in `internal/renderer/workflow.go`, unchanged. It is
already persisted as a live URL in every rendered site (`feeds/a3f1b2c4.html`),
so widening it would break bookmarked per-feed pages. `/api/v1/feeds/a3f1b2c4`
therefore lines up with `/feeds/a3f1b2c4.html` in the same build for free.

**Item ID** — first 16 hex characters of `sha256(feed_url + "\n" + guid)`.

Rejected alternatives for item identity:

- `items.id` is an `AUTOINCREMENT`. `purge` deletes archived rows, and a later
  refetch reinserts them with a new id. A script that stored an id gets a
  different article back.
- `items.link` is ambiguous. The CLI's own lookup is
  `SELECT feed_url, guid FROM items WHERE link = ? LIMIT 1` — a syndicated
  article present in two feeds already resolves arbitrarily.

Sixteen hex characters rather than eight because, unlike the feed ID, there is
no existing artifact to stay byte-compatible with.

Every payload also carries the full `feed_url` and `guid`, so clients can
reconstruct identity without reimplementing the hash.

**Lookup by natural key happens on the collection, not the path:**
`GET /api/v1/feeds?url=<exact>` and `GET /api/v1/items?link=<exact>`. This keeps
one canonical path form while letting scripts that only hold a URL — which is
every existing CLI user — get in the door. `?link=` may match multiple items and
returns a normal collection response.

## Resource Shapes

DTOs are hand-written in `internal/api/dto.go`, not `database.Item` and
`database.Feed` marshaled directly. The current `--format json` output emits
`sql.NullTime` as `{"Time":"...","Valid":true}`; the API emits `null` or an
RFC3339 string. Nullable columns become nullable JSON.

### Item (lean, as returned by list endpoints)

```json
{
  "id": "9f2c4a1be7d03518",
  "feed_id": "a3f1b2c4",
  "feed_url": "https://example.com/feed.xml",
  "guid": "https://example.com/posts/hello",
  "title": "Hello",
  "link": "https://example.com/posts/hello",
  "summary": "A short summary.",
  "published_date": "2026-08-20T14:02:00Z",
  "first_seen": "2026-08-21T09:00:00Z",
  "discovered_at": "2026-08-21T09:00:00Z",
  "archived": false
}
```

`published_date` and `first_seen` are both nullable. Scraped feeds (`type:
"scrape"`) deliberately leave `published_date` unset so that `first_seen`
controls ordering, so a client must not assume it is present.

`discovered_at` is the derived sort key — `published_date` falling back to
`first_seen`, matching `Item.EffectiveDate()` and the `discovered_at` field the
CLI's `--compact` output already emits. It is `null` only when both underlying
columns are unusable.

### Feed

```json
{
  "id": "a3f1b2c4",
  "url": "https://example.com/feed.xml",
  "title": "Example",
  "description": "An example feed.",
  "last_updated": "2026-08-20T14:02:00Z",
  "last_fetch_time": "2026-08-25T06:00:00Z",
  "last_successful_fetch": "2026-08-25T06:00:00Z",
  "latest_item_date": "2026-08-20T14:02:00Z",
  "error_count": 0,
  "last_error": "",
  "user_agent": "",
  "type": "rss",
  "scrape_selector": ""
}
```

`type` is `rss` or `scrape`; `scrape_selector` is populated only for the latter.

### Annotation

```json
{
  "kind": "seen",
  "value": null,
  "actor": null,
  "created_at": "2026-08-25T17:14:03Z"
}
```

`database.ItemAnnotation.CreatedAt` is a `string`; the DTO layer parses it to a
`time.Time` and emits RFC3339. If a stored value fails to parse, emit it
verbatim rather than failing the request.

### The `include` parameter

Heavy fields are opt-in through one comma-separated `include` parameter. A
50-item page carrying full article HTML plus raw gofeed blobs is megabytes,
which makes the default response useless from a shell.

| Value | Adds |
| --- | --- |
| `content` | `content` — the full item body |
| `raw` | `item_json` / `feed_json` — the raw gofeed blob |
| `metadata` | `metadata` — the unfurl record for the item's `link` |
| `annotations` | `annotations` — array of annotation objects |
| `counts` | Feeds only: `item_count`, `unseen_count` |

Defaults:

- List endpoints: nothing included.
- `GET /api/v1/items/{id}`: `content,annotations`.
- `GET /api/v1/feeds/{id}`: nothing; `raw` and `counts` always explicit.

`raw` is never implied anywhere — it is large and duplicates fields already
present in the lean shape.

Allowed values differ per endpoint: `counts` is valid only on feed endpoints,
`content` only on item endpoints. An unrecognized value, or one not valid for
the endpoint it was sent to, is a `400 invalid_parameter`.

`include=annotations` on an item with none yields `[]`, never `null`.

## Endpoints

```
GET    /api/v1/                                    service root
GET    /api/v1/openapi.yaml                        the spec, as application/yaml
GET    /api/v1/status                              spool health, mirrors `feedspool status`
GET    /api/v1/feeds                               list feeds
GET    /api/v1/feeds/{feed_id}                     one feed
GET    /api/v1/items                               list items
GET    /api/v1/items/{item_id}                     one item
GET    /api/v1/items/{item_id}/annotations         list annotations
POST   /api/v1/items/{item_id}/annotations         add one annotation
DELETE /api/v1/items/{item_id}/annotations/{kind}  remove annotations
POST   /api/v1/annotations                         bulk add
```

There is deliberately no `/feeds/{id}/items` — it is `/items?feed_id=` with
extra steps.

Service root:

```json
{"api_version": "v1", "feedspool_version": "1.0.2"}
```

`feedspool_version` is `cmd.Version`, stamped at build time.

`GET /api/v1/status` wraps the existing `GetSpoolStatus`:

```json
{
  "feed_count": 142,
  "item_count": 38217,
  "last_fetch_time": "2026-08-25T06:00:00Z",
  "failing_feed_count": 3,
  "consecutive_error_count": 1
}
```

`SpoolStatus` already carries JSON tags but marks `LastFetchTime` as `json:"-"`
because it is a `sql.NullTime`; the DTO layer emits it as a nullable RFC3339
string.

### Item list parameters

| Parameter | Values | Default |
| --- | --- | --- |
| `feed_id` | 8-hex feed ID | — |
| `feed_url` | exact feed URL | — |
| `feed_query` | feed URL substring, case-insensitive | — |
| `link` | exact item link | — |
| `q` | item title substring, case-insensitive | — |
| `since` | RFC3339, on discovery time | — |
| `until` | RFC3339, on discovery time | — |
| `seen` | `true` \| `false` | both |
| `archived` | `true` \| `false` \| `any` | `false` |
| `sort` | `newest` \| `oldest` | `newest` |
| `limit` | 1–200 | 50 |
| `cursor` | opaque | — |
| `include` | see above | none |

Any two of `feed_id`, `feed_url`, and `feed_query` together is a `400`.

**`since` and `until` filter on discovery time, not publication date.** This
matches `feedspool items --since` exactly: discovery time is `first_seen`,
falling back to `published_date` when `first_seen` is absent or holds the
`0001-01-01` zero-value sentinel. It is also the semantics a polling script
wants — feeds routinely backdate `published_date`, so "everything since my last
poll" is only correct against discovery time.

**`q` matches item titles only,** case-insensitively, by substring — identical
to `--search`. Not body content, not summaries. Deliberately the same
limitation as the CLI rather than a second, subtly different search; #58 will
upgrade both at once.

**`archived` defaults to `false`, which diverges from the CLI.** `feedspool
items` includes archived rows. The API default should mean "my current feed,"
and archived means the item is no longer present upstream. The divergence is
documented in MANUAL.md rather than quietly resolved in either direction.

**`seen` is tri-state on purpose.** `database.ItemFilter` has independent `Seen`
and `Unseen` booleans; the CLI guards the impossible combination with a runtime
check in `validateItemsOptions`. One parameter makes the state unrepresentable
at the API boundary instead.

### Feed list parameters

`url` (exact match), `limit` (1–500, default 200), `cursor`, `include`.

No `sort`. Default order is `url ASC` — the primary key, never null, always
unique, so the keyset cursor needs no tiebreak. There are tens to hundreds of
feeds and `latest_item_date` is in every payload, so a client wanting "most
recently active first" sorts locally. Server-side sort would mean NULL-ordering
inside a keyset cursor for no real gain.

## Pagination

Keyset, not offset.

Items are ordered by `(date_rank, effective_date DESC, id DESC)` for
`sort=newest` and ascending for `sort=oldest`. New items arrive at the head of
that ordering constantly, so offset paging silently skips and duplicates rows
mid-scan.

`effective_date` is `julianday(COALESCE(published_date, first_seen))`, the same
expression the CLI orders by. `date_rank` is `0` when that expression is
non-NULL and `1` when it is NULL, so unorderable rows form one deterministic
block at the tail instead of scattering.

The cursor is a base64url-encoded, opaque composite of the full sort key. For
items that is `(date_rank, effective_date, id)`; for feeds it is `(url)`. The
composite matters: feeds routinely stamp many items with an identical
timestamp, and a date-only cursor loses every row in the tie. The query uses
SQLite row-value comparison.

### Pagination and the timestamp problem

`GetItems` cannot back this. When `Since` or `Until` is set it loads every
matching row, filters and sorts in Go, then truncates — an in-memory full scan
with no stable cursor position. The API therefore needs its own repository
method rather than reusing it.

That Go-side pass exists for a reason worth understanding before replacing it.
`parseDatabaseTime` accepts five distinct layouts, including Go's default
`2006-01-02 15:04:05.999999999 -0700 MST` and a bare `2006-01-02`. SQLite's
`julianday()` understands none of those, returning NULL. The existing SQL is
written `(expr IS NULL OR expr >= julianday(?))` — keep anything SQLite can't
parse — and Go re-filters afterward to get the right answer.

So legacy rows written before `formatDatabaseTime` normalized writes to
RFC3339Nano may be unorderable in SQL. The `date_rank` column is the concession:
**pagination stays total and deterministic** — no row is skipped, none is
returned twice, and the cursor always advances — but rows SQLite cannot parse
sort as a block at the tail, ordered by `id` within it, rather than in true date
order.

The real fix is normalizing stored timestamps in a migration, which would also
let the CLI drop its Go-side scan. That is a separate data migration on the
whole `items` table and does not belong in the same PR as a new API surface;
filed as [#60](https://github.com/lmorchard/feedspool-go/issues/60). New writes are already normalized, so the affected set is
bounded and shrinking.

Add a new `ListItems(*ItemPage)` to `internal/database` for this. `GetItems`
keeps its current behavior — the CLI depends on it and changing its semantics
as a side effect of shipping an API is the wrong trade.

Clients must treat the cursor as opaque. Its internal encoding is not part of
the v1 contract and may change.

Collection response:

```json
{
  "data": [ ... ],
  "next_cursor": "eyJwZCI6IjIwMjYtMDgtMjBUMTQ6MDI6MDBaIiwiaWQiOjQyfQ",
  "limit": 50
}
```

`next_cursor` is `null` when the result set is exhausted. Single-resource
endpoints return the bare object, with no envelope.

There is no `total`. It costs a second full scan and it is wrong by the time
the client reads it.

`limit` above the maximum is clamped, not rejected; the effective value comes
back in the response so a client can tell.

A malformed or undecodable cursor is a `400 invalid_cursor`.

## Writes

### Add one annotation

```
POST /api/v1/items/9f2c4a1be7d03518/annotations
Content-Type: application/json

{"kind": "seen", "value": null, "actor": null}
```

`201` with the annotation object on create, `200` with the existing object if it
was already present.

### Remove annotations

```
DELETE /api/v1/items/9f2c4a1be7d03518/annotations/seen
DELETE /api/v1/items/9f2c4a1be7d03518/annotations/starred?value=later
```

`204`, idempotent — `204` even when nothing matched.

Semantics mirror `database.RemoveAnnotation` exactly: with no `?value=`, delete
rows where `value IS NULL`; with `?value=x`, delete rows matching `x`. It does
**not** delete every row of a kind regardless of value. That is the current
behavior and changing it here would put the API and CLI in disagreement.

### Bulk add

```
POST /api/v1/annotations
Content-Type: application/json

{"item_ids": ["9f2c...", "1a4b..."], "kind": "seen", "value": null, "actor": null}
```

`200` with `{"added": 12, "already_present": 3, "not_found": []}`.

Bulk add only. Marking a screenful seen is the archetypal reader operation;
unmarking a screenful is not, and supporting it means either `DELETE` with a
request body or an `op` field that turns the endpoint into RPC. Single-item
`DELETE` covers the real case.

Cap `item_ids` at 500 per request; over that is a `400`.

### Write validation

- `Content-Type: application/json` required on any request with a body; `415`
  otherwise.
- Bodies read through `http.MaxBytesReader`.
- `kind` must match `^[A-Za-z0-9_.:-]{1,64}$`. Arbitrary kinds are the feature,
  but `{kind}` is a path segment in the `DELETE` route, and a kind containing a
  slash or a percent-escape is not reliably addressable through Go's
  `ServeMux` path matching. Restricting the charset removes that whole class of
  problem rather than papering over it. Reads return whatever is stored, so
  kinds written by the CLI outside this set remain visible via
  `include=annotations`; removing one requires `feedspool unannotate`. Document
  that.
- `actor` is `null` unless the client supplies one.
- An unknown `item_id` in the single-item path is a `404`; in bulk it lands in
  the `not_found` array without failing the request.

## Errors

```json
{"error": {"code": "invalid_cursor", "message": "cursor is not valid base64"}}
```

| Code | Status |
| --- | --- |
| `invalid_parameter` | 400 |
| `invalid_cursor` | 400 |
| `unauthorized` | 401 |
| `not_found` | 404 |
| `method_not_allowed` | 405 |
| `unsupported_media_type` | 415 |
| `payload_too_large` | 413 |
| `internal_error` | 500 |

`internal_error` messages are generic; details go to the log, not the response.

**Unknown query parameters are rejected** with `400 invalid_parameter`. This
costs forward compatibility, but `?limitt=10` silently returning the default 50
is a genuinely bad afternoon for a script author, and that matters more at this
scale.

## Compatibility

`/api/v1/` in the path. Within v1, changes are additive only:

- Allowed: new fields on existing shapes, new optional parameters, new
  endpoints, new error codes, new `include` values.
- Requires v2: removing or renaming a field, changing a field's type, changing
  a parameter's default, changing pagination semantics.

The cursor encoding is explicitly excluded from the contract.

## Architecture

### New package: `internal/ids`

```go
func FeedID(feedURL string) string          // 8 hex, sha256(url)
func ItemID(feedURL, guid string) string    // 16 hex, sha256(url + "\n" + guid)
```

`generateFeedID` moves out of `internal/renderer/workflow.go` and the renderer
imports this instead, so the API and the rendered site cannot drift apart.

### New package: `internal/api`

| File | Responsibility |
| --- | --- |
| `api.go` | Route table and `Handler(deps) http.Handler` |
| `dto.go` | Response types and converters from `database` models |
| `feeds.go` | Feed handlers |
| `items.go` | Item handlers |
| `annotations.go` | Annotation handlers |
| `pagination.go` | Cursor encode/decode, limit clamping |
| `params.go` | Query parameter parsing, validation, unknown-param rejection |
| `errors.go` | Error envelope writer |
| `middleware.go` | Auth, content-type checks, panic recovery |
| `openapi.yaml` | Embedded via `go:embed` |

Routes are declared as a table:

```go
var routes = []route{
    {http.MethodGet, "/api/v1/feeds", handleListFeeds},
    // ...
}
```

Both the mux builder and the OpenAPI parity test read this table. Go's
`http.ServeMux` will not enumerate its registered patterns, so a table is the
cheap way to make drift detection possible — and it keeps the whole surface
reviewable in one place.

### Changes to `internal/server`

- `NewServer(config *Config, db *database.DB)`. `cmd/serve.go` owns the DB
  lifecycle: it opens the database only when `--api` is set, checks
  `IsInitialized()`, and closes it on shutdown. The server does not open files.
- `createHandler` becomes an `http.ServeMux`: `/api/v1/` to the API handler,
  `/` to the existing static handler. The current security-header and
  verbose-logging wrapper stays outside the mux, unchanged.
- `Config` gains `Bind string`, `APIEnabled bool`, `APIToken string`.
- `validateConfig` only requires `Dir` when static serving is on, so
  `serve --api` works with no build directory. With no `Dir`, `/` returns a
  JSON `404`.
- The listen address becomes `net.JoinHostPort(bind, port)`; an empty `Bind`
  yields `:port`, which is exactly today's behavior.

### Configuration

```yaml
serve:
  port: 8889
  bind: ""          # "" = all interfaces (current behavior)
  dir: ./build
  api:
    enabled: false
    token: ""       # or FEEDSPOOL_API_TOKEN
```

Flags: `--api`, `--bind`. There is deliberately **no `--api-token` flag** — a
token passed on the command line lands in `ps` output. Config file or
environment only, bound via `viper.BindEnv("serve.api.token",
"FEEDSPOOL_API_TOKEN")`.

`bind` exists to make the startup warning honest. Without it, the warning would
fire for every localhost user and get tuned out.

### Auth

Empty token: the middleware passes everything through. Non-empty token: every
`/api/v1/` request requires `Authorization: Bearer <token>`, compared with
`subtle.ConstantTimeCompare`; failures get `401` and a `WWW-Authenticate: Bearer`
header. Uniform across reads and writes.

At startup, if the API is enabled, `bind` is not a loopback address, and no
token is configured, log a `logrus.Warn`. It is a warning, not a gate — open by
default is the chosen posture. The warning just makes the choice visible.

## Database Changes

### Migration 10 — annotation uniqueness

`AddAnnotation` is a bare `INSERT` and `item_annotations` has no unique
constraint, so `feedspool mark-seen <link>` twice already writes two `seen`
rows. That is survivable from a CLI. Over an API it is not: a UI marking seen on
scroll, or an agent re-running its triage, accumulates rows that
`GET .../annotations` then faithfully returns as duplicates.

The migration, in one transaction:

1. Delete duplicate rows, keeping the one with `MIN(created_at)` per
   `(feed_url, item_guid, kind, COALESCE(value, ''))` group — so "first seen"
   stays honest — breaking ties on `MIN(rowid)` so the result is deterministic
   when duplicates share a timestamp, which they often will since
   `CURRENT_TIMESTAMP` has one-second resolution.
2. `CREATE UNIQUE INDEX idx_item_annotations_unique ON item_annotations(feed_url, item_guid, kind, COALESCE(value, ''))`.

`COALESCE` is required: SQLite treats `NULL`s as distinct in a unique index, so
`(feed, guid, 'seen', NULL)` would otherwise never conflict with itself.

`AddAnnotation` then becomes `INSERT ... ON CONFLICT DO NOTHING`, which fixes
the CLI and API together. A new `AnnotationExists` helper backs the
`201`-vs-`200` distinction.

`main` is at `maxMigrationVersion = 9`, so this is migration 10.
`applyMigration4WithBackfill` is the existing precedent for a transactional,
data-touching migration; follow its shape.

### Not in scope: `busy_timeout` and `SetMaxOpenConns`

The first draft added `PRAGMA busy_timeout = 5000` here. Issue #57 covers that
exact problem and PR #59 is already open against it, so it is removed from this
spec to avoid a conflicting change.

`SetMaxOpenConns(1)` also stays as-is. It is a real throughput ceiling for a
long-running server, but the fetcher runs 32 concurrent goroutines against this
setting and changing its contention behavior as a side effect of shipping an API
is the wrong trade. Noted as a follow-up.

## OpenAPI

`internal/api/openapi.yaml`, hand-written, embedded with `go:embed`, served at
`/api/v1/openapi.yaml` as `application/yaml`.

Hand-written rather than generated: code generation means a new dependency and a
build step, and the surface here is a dozen endpoints.

Drift is caught by a test rather than by discipline. The test parses the
embedded YAML's `paths`, enumerates the `routes` table, and asserts both
directions match — every route documented, every documented path routed. There
are no exemptions: the service root and `openapi.yaml` itself appear in the
spec's `paths` like everything else, so the test needs no allowlist to
maintain.

## Testing

- **Handlers** — table-driven, `httptest` against a temp SQLite database built
  with `database.New(filepath.Join(t.TempDir(), "test.db"))` plus `InitSchema`.
  Status, body shape, every filter, every `include` value.
- **Pagination continuity** — seed a set, page through it, insert a row at the
  head mid-scan, assert no duplicated and no skipped rows. This is the test that
  justifies keyset over offset, so it should exist.
- **Cursors** — round-trip encode/decode; malformed, truncated, and
  foreign-encoding cursors all produce `400`.
- **Auth** — no token configured means open; token configured means `401`
  without and `200` with.
- **Route/OpenAPI parity** — both directions.
- **Migration 10** — seed duplicate annotations with distinct `created_at`,
  migrate, assert one row survives carrying the earliest timestamp, assert a
  repeat insert is a no-op.
- **`internal/ids`** — golden values pinning known URLs to their current 8-hex
  `FeedID` outputs. Those values are live static-site URLs; a silent change
  breaks bookmarks.
- **`internal/server`** — `--api` with no `--dir` starts; `--dir` without
  `--api` behaves exactly as before.

## Documentation

- MANUAL.md gains an "HTTP API" section: endpoint reference, `curl` examples,
  the `archived` divergence from `feedspool items`, and the v1 compatibility
  rule.
- README.md gets a short mention pointing at the manual.
- `feedspool.yaml.example` gains the `serve.bind` and `serve.api` blocks.

## Deferred

| Item | Why |
| --- | --- |
| Ranked / full-text search | `?q=` ships in v1 as a title substring match, mirroring `--search` exactly. What is deferred is doing it *properly*: an FTS5 virtual table, a migration, index upkeep on every upsert, `bm25()` ranking, and body-content matching. Tracked in #58, which also has to solve ranking-vs-keyset-cursor. |
| Normalizing stored timestamps | See "Pagination and the timestamp problem". A data migration over the whole `items` table; would also let the CLI drop its Go-side filter and sort. Tracked in #60. |
| CORS | Same-origin covers the rendered site. Nothing needs it yet. |
| Bulk annotation removal | No real use case; would force `DELETE`-with-body or an RPC-shaped endpoint. |
| Raising `SetMaxOpenConns` | Would change fetcher contention behavior as a side effect. |
| Subscription management over HTTP | Writes to feed list files, not the database — a different problem. |
